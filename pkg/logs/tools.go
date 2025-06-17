package logs

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	logsclient "github.com/render-oss/render-mcp-server/pkg/client/logs"
	"github.com/render-oss/render-mcp-server/pkg/mcpserver"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

func Tools(c *client.ClientWithResponses) []server.ServerTool {
	logRepo := NewLogRepo(c)

	return []server.ServerTool{
		listLogs(logRepo),
		listLogLabelValues(logRepo),
	}
}

func listLogs(logRepo *LogRepo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("list_logs",
			mcp.WithDescription("List logs matching the provided filters. Logs are paginated by start and end timestamps. "+
				"There are more logs to fetch if hasMore is true in the response. "+
				"Provide the nextStartTime and nextEndTime timestamps as the startTime and endTime query parameters to fetch the next page of logs. "+
				"You can query for logs across multiple resources, but all resources must be in the same region and belong to the same owner."),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:          "List logs",
				ReadOnlyHint:   pointers.From(true),
				IdempotentHint: pointers.From(true),
				OpenWorldHint:  pointers.From(true),
			}),
			mcp.WithArray("resource",
				mcp.Required(),
				mcp.Description("Filter logs by their resource. A resource is the id of a server, cronjob, job, postgres, or redis."),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithArray("level",
				mcp.Description("Filter logs by their severity level. Wildcards and regex are supported."),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithArray("type",
				mcp.Description("Filter logs by their type. Types include app for application logs, request for request logs, and build for build logs. You can find the full set of types available for a query by using the list_log_label_values tool."),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithArray("instance",
				mcp.Description("Filter logs by the instance they were emitted from. An instance is the id of a specific running server."),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithArray("host",
				mcp.Description("Filter request logs by their host. Wildcards and regex are supported."),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithArray("statusCode",
				mcp.Description("Filter request logs by their status code. Wildcards and regex are supported."),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithArray("method",
				mcp.Description("Filter request logs by their requests method. Wildcards and regex are supported."),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithArray("path",
				mcp.Description("Filter request logs by their path. Wildcards and regex are supported."),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithArray("text",
				mcp.Description("Filter by the text of the logs. Wildcards and regex are supported."),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithString("startTime",
				mcp.Description("Start time for log query (RFC3339 format). "+
					"Defaults to 1 hour ago. "+
					"The start time must be within the last 30 days."),
			),
			mcp.WithString("endTime",
				mcp.Description("End time for log query (RFC3339 format). "+
					"Defaults to the current time. "+
					"The end time must be within the last 30 days."),
			),
			mcp.WithString("direction",
				mcp.Description("The direction to query logs for. Backward will return most recent logs first. Forward will start with the oldest logs in the time range."),
				mcp.Enum(string(logsclient.Backward), string(logsclient.Forward)),
				mcp.DefaultString(string(logsclient.Backward)),
			),
			mcp.WithNumber("limit",
				mcp.Description("Maximum number of logs to return"),
				mcp.Min(1),
				mcp.Max(100),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ownerId, err := session.FromContext(ctx).GetWorkspace(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			resource, err := validate.RequiredToolArrayParam[string](request, "resource")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			llParams := &client.ListLogsParams{
				OwnerId:  ownerId,
				Resource: resource,
			}

			// Optional filters
			if levelFilters, ok, err := validate.OptionalToolArrayParam[string](request, "level"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				llParams.Level = &levelFilters
			}

			if typeFilters, ok, err := validate.OptionalToolArrayParam[string](request, "type"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				llParams.Type = &typeFilters
			}

			if instanceFilters, ok, err := validate.OptionalToolArrayParam[string](request, "instance"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				llParams.Instance = &instanceFilters
			}

			if hostFilters, ok, err := validate.OptionalToolArrayParam[string](request, "host"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				llParams.Host = &hostFilters
			}

			if statusCodeFilters, ok, err := validate.OptionalToolArrayParam[string](request, "statusCode"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				llParams.StatusCode = &statusCodeFilters
			}

			if methodFilters, ok, err := validate.OptionalToolArrayParam[string](request, "method"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				llParams.Method = &methodFilters
			}

			if pathFilters, ok, err := validate.OptionalToolArrayParam[string](request, "path"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				llParams.Path = &pathFilters
			}

			if textFilters, ok, err := validate.OptionalToolArrayParam[string](request, "text"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				llParams.Text = &textFilters
			}

			if startTimeStr, ok, err := validate.OptionalToolParam[string](request, "startTime"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				parsedTime, err := time.Parse(time.RFC3339, startTimeStr)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				startTimeParam := client.StartTimeParam(parsedTime)
				llParams.StartTime = &startTimeParam
			}

			if endTimeStr, ok, err := validate.OptionalToolParam[string](request, "endTime"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				parsedTime, err := time.Parse(time.RFC3339, endTimeStr)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				endTimeParam := client.EndTimeParam(parsedTime)
				llParams.EndTime = &endTimeParam
			}

			if direction, ok, err := validate.OptionalToolParam[string](request, "direction"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				directionParam := logsclient.LogDirectionParam(direction)
				llParams.Direction = &directionParam
			}

			if limit, ok, err := validate.OptionalToolParam[float64](request, "limit"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				limitInt := int(limit)
				llParams.Limit = &limitInt
			}

			response, err := logRepo.ListLogs(ctx, llParams)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			respJSON, err := json.Marshal(response)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(string(respJSON)), nil
		},
	}
}

func listLogLabelValues(logRepo *LogRepo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("list_log_label_values",
			mcp.WithDescription("List all values for a given log label in the logs matching the provided filters. "+
				"This can be used to discover what values are available for filtering logs using the list_logs tool. "+
				"You can query for logs across multiple resources, but all resources must be in the same region and belong to the same owner.",
			),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:          "List log label values",
				ReadOnlyHint:   pointers.From(true),
				IdempotentHint: pointers.From(true),
				OpenWorldHint:  pointers.From(true),
			}),
			mcp.WithString("label",
				mcp.Required(),
				mcp.Description("The label to list values for."),
				mcp.Enum(mcpserver.EnumValuesFromClientType(client.Host, client.Instance, client.Level, client.Method, client.StatusCode, client.Type)...),
			),
			mcp.WithArray("resource",
				mcp.Required(),
				mcp.Description("Filter by resource. A resource is the id of a server, cronjob, job, postgres, or redis."),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithArray("level",
				mcp.Description("Filter logs by their severity level. Wildcards and regex are supported."),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithArray("type",
				mcp.Description("Filter logs by their type."),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithArray("instance",
				mcp.Description("Filter logs by the instance they were emitted from."),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithArray("host",
				mcp.Description("Filter request logs by their host. Wildcards and regex are supported."),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithArray("statusCode",
				mcp.Description("Filter request logs by their status code. Wildcards and regex are supported."),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithArray("method",
				mcp.Description("Filter request logs by their requests method. Wildcards and regex are supported."),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithArray("path",
				mcp.Description("Filter request logs by their path. Wildcards and regex are supported."),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithArray("text",
				mcp.Description("Filter by the text of the logs. Wildcards and regex are supported."),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithString("startTime",
				mcp.Description("Start time for log query (RFC3339 format). "+
					"Defaults to 1 hour ago. "+
					"The start time must be within the last 30 days."),
			),
			mcp.WithString("endTime",
				mcp.Description("End time for log query (RFC3339 format). "+
					"Defaults to the current time. "+
					"The end time must be within the last 30 days."),
			),
			mcp.WithString("direction",
				mcp.Description("The direction to query logs for. Backward will return most recent logs first. Forward will start with the oldest logs in the time range."),
				mcp.Enum(string(logsclient.Backward), string(logsclient.Forward)),
				mcp.DefaultString(string(logsclient.Backward)),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			label, err := validate.RequiredToolParam[string](request, "label")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ownerId, err := session.FromContext(ctx).GetWorkspace(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			resource, err := validate.RequiredToolArrayParam[string](request, "resource")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			params := &client.ListLogsValuesParams{
				OwnerId:  ownerId,
				Label:    client.ListLogsValuesParamsLabel(label),
				Resource: resource,
			}

			// Optional filters
			if levelFilters, ok, err := validate.OptionalToolArrayParam[string](request, "level"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				params.Level = &levelFilters
			}

			if typeFilters, ok, err := validate.OptionalToolArrayParam[string](request, "type"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				params.Type = &typeFilters
			}

			if instanceFilters, ok, err := validate.OptionalToolArrayParam[string](request, "instance"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				params.Instance = &instanceFilters
			}

			if hostFilters, ok, err := validate.OptionalToolArrayParam[string](request, "host"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				params.Host = &hostFilters
			}

			if statusCodeFilters, ok, err := validate.OptionalToolArrayParam[string](request, "statusCode"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				params.StatusCode = &statusCodeFilters
			}

			if methodFilters, ok, err := validate.OptionalToolArrayParam[string](request, "method"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				params.Method = &methodFilters
			}

			if pathFilters, ok, err := validate.OptionalToolArrayParam[string](request, "path"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				params.Path = &pathFilters
			}

			if textFilters, ok, err := validate.OptionalToolArrayParam[string](request, "text"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				params.Text = &textFilters
			}

			if startTimeStr, ok, err := validate.OptionalToolParam[string](request, "startTime"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				parsedTime, err := time.Parse(time.RFC3339, startTimeStr)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				startTimeParam := client.StartTimeParam(parsedTime)
				params.StartTime = &startTimeParam
			}

			if endTimeStr, ok, err := validate.OptionalToolParam[string](request, "endTime"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				parsedTime, err := time.Parse(time.RFC3339, endTimeStr)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				endTimeParam := client.EndTimeParam(parsedTime)
				params.EndTime = &endTimeParam
			}

			if direction, ok, err := validate.OptionalToolParam[string](request, "direction"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				directionParam := logsclient.LogDirectionParam(direction)
				params.Direction = &directionParam
			}

			values, err := logRepo.ListLogLabelValues(ctx, params)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			respJSON, err := json.Marshal(values)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(string(respJSON)), nil
		},
	}
}
