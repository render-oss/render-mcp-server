package event

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	eventtypes "github.com/render-oss/render-mcp-server/pkg/client/eventtypes"
	"github.com/render-oss/render-mcp-server/pkg/mcpserver"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

const (
	// defaultLookback is how far back list_events looks when no startTime is
	// given. A week is wide enough to answer "why did this restart last night".
	defaultLookback = 7 * 24 * time.Hour

	defaultLimit = 20
)

func Tools(c *client.ClientWithResponses) []server.ServerTool {
	eventRepo := NewRepo(c)

	return []server.ServerTool{
		listEvents(eventRepo),
	}
}

// serviceEventTypes returns the event types the API accepts as a filter.
func serviceEventTypes() []string {
	return mcpserver.EnumValuesFromClientType(eventtypes.ServiceEventTypeValues()...)
}

func listEvents(eventRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("list_events",
			mcp.WithDescription("List a service's event history: deploys, builds, restarts, "+
				"failures, scaling, suspensions, and disk changes. "+
				"Use it to find out what happened to a service and why. Failure events carry the "+
				"reason a service went down, such as out-of-memory kills, non-zero exits, failed "+
				"health checks, and evictions, which may not show up in logs. "+
				"The first page covers the last 7 days unless startTime says otherwise. For older "+
				"events, page with the cursor from the previous call. If the first page comes back "+
				"empty there is no cursor to follow, so set an earlier startTime instead. "+
				"Events cover services only, not Postgres or Key Value instances."),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "List service events",
				ReadOnlyHint:    pointers.From(true),
				DestructiveHint: pointers.From(false),
				IdempotentHint:  pointers.From(true),
				OpenWorldHint:   pointers.From(false),
			}),
			mcp.WithString("serviceId",
				mcp.Required(),
				mcp.Description("The ID of the service to list events for"),
			),
			mcp.WithArray("eventTypes",
				mcp.Description("Filter to specific event types. Returns all types when omitted. "+
					"For debugging a failure, server_failed, deploy_ended, build_ended, and "+
					"image_pull_failed carry the most detail."),
				mcp.Items(map[string]interface{}{
					"type": "string",
					"enum": serviceEventTypes(),
				}),
			),
			mcp.WithString("startTime",
				mcp.Description("Start of the time range in RFC3339 format "+
					"(e.g. '2024-01-01T12:00:00Z'). Defaults to 7 days ago on the first page. "+
					"Set it to reach events older than that."),
			),
			mcp.WithString("endTime",
				mcp.Description("End of the time range in RFC3339 format "+
					"(e.g. '2024-01-01T13:00:00Z'). Defaults to the current time."),
			),
			mcp.WithNumber("limit",
				mcp.Description("The maximum number of events to return, newest first."),
				mcp.DefaultNumber(defaultLimit),
				mcp.Min(1),
				mcp.Max(100),
			),
			mcp.WithString("cursor",
				mcp.Description("A unique string that corresponds to a position in the result list. "+
					"If provided, the endpoint returns results that appear after the corresponding position. "+
					"To fetch the first page of results, set to the empty string."),
				mcp.DefaultString(""),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			serviceId, err := validate.RequiredToolParam[string](request, "serviceId")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			params, err := parseListEventsParams(request)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			events, cursor, err := eventRepo.ListEvents(ctx, serviceId, params)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			respJSON, err := json.Marshal(events)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			respText := string(respJSON) + "\n\n cursor: "

			if cursor == nil {
				respText += `""`
			} else {
				respText += *cursor
			}

			return mcp.NewToolResultText(respText), nil
		},
	}
}

func parseListEventsParams(request mcp.CallToolRequest) (*client.ListEventsParams, error) {
	params := &client.ListEventsParams{
		Limit: pointers.From(defaultLimit),
	}

	if limit, ok, err := validate.OptionalToolParam[float64](request, "limit"); err != nil {
		return nil, err
	} else if ok {
		params.Limit = pointers.From(int(limit))
	}

	if cursor, ok, err := validate.OptionalToolParam[string](request, "cursor"); err != nil {
		return nil, err
	} else if ok && cursor != "" {
		params.Cursor = &cursor
	}

	startTime, err := optionalTimeParam(request, "startTime")
	if err != nil {
		return nil, err
	}
	if startTime == nil && params.Cursor == nil {
		// The default window only applies to a first page. A cursor already
		// pins the position, and the API applies both, so re-applying the
		// window would stop paging at the lookback.
		startTime = pointers.From(time.Now().Add(-defaultLookback))
	}
	params.StartTime = startTime

	params.EndTime, err = optionalTimeParam(request, "endTime")
	if err != nil {
		return nil, err
	}

	eventTypes, _, err := validate.OptionalToolArrayParam[string](request, "eventTypes")
	if err != nil {
		return nil, err
	}
	if len(eventTypes) > 0 {
		// The schema declares one `type` value, but the API accepts several
		// separated by commas, so send them as a single param.
		var typeParam client.EventTypeParam
		if err := typeParam.FromExternalRef8ServiceEventType(
			eventtypes.ServiceEventType(strings.Join(eventTypes, ",")),
		); err != nil {
			return nil, err
		}
		params.Type = &typeParam
	}

	return params, nil
}

// optionalTimeParam reads an RFC3339 timestamp argument. It returns nil when
// the argument was not supplied.
func optionalTimeParam(request mcp.CallToolRequest, param string) (*time.Time, error) {
	value, ok, err := validate.OptionalToolParam[string](request, param)
	if err != nil || !ok {
		return nil, err
	}

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid %s, expected RFC3339 (e.g. 2024-01-01T12:00:00Z): %w", param, err)
	}

	return &parsed, nil
}
