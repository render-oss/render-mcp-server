package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

const defaultLimit = 20
const requestTimeout = 30 * time.Second
const maxToolOutputBytes = 256 << 10

func Tools(c *client.ClientWithResponses) []server.ServerTool {
	r := NewRepo(c)
	return []server.ServerTool{
		listWorkflows(r), getWorkflow(r), listVersions(r), getVersion(r),
		listTasks(r), getTask(r), listRuns(r), getRun(r), getResults(r),
		createWorkflow(r), createWorkflowVersion(r),
	}
}

// WithLimits wraps already workspace-scoped tools so workspace resolution and
// all Workflows API calls share the same deadline.
func WithLimits(tools []server.ServerTool) []server.ServerTool {
	bounded := make([]server.ServerTool, len(tools))
	for i, tool := range tools {
		handler := tool.Handler
		tool.Handler = func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ctx, cancel := context.WithTimeout(ctx, requestTimeout)
			defer cancel()
			return handler(ctx, request)
		}
		bounded[i] = tool
	}
	return bounded
}

// readTool keeps the same annotations and error convention as other services.
func readTool(name, description string, handler func(context.Context, mcp.CallToolRequest) (any, error), options ...mcp.ToolOption) server.ServerTool {
	options = append([]mcp.ToolOption{mcp.WithDescription(description), mcp.WithToolAnnotation(mcp.ToolAnnotation{ReadOnlyHint: pointers.From(true), DestructiveHint: pointers.From(false), IdempotentHint: pointers.From(true), OpenWorldHint: pointers.From(false)})}, options...)
	tool := mcp.NewTool(name, options...)
	return server.ServerTool{Tool: tool, Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Reject typos rather than silently ignoring a filter and widening a query.
		for key := range request.GetArguments() {
			if key != "workspaceId" {
				if _, ok := tool.InputSchema.Properties[key]; !ok {
					return mcp.NewToolResultError("unknown parameter: " + key), nil
				}
			}
		}
		value, err := handler(ctx, request)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		b, err := json.Marshal(value)
		if err != nil {
			return mcp.NewToolResultError("could not encode Workflows response"), nil
		}
		if len(b) > maxToolOutputBytes {
			return mcp.NewToolResultError("Workflows tool output exceeds 256 KiB; reduce limit or use the REST API for this resource"), nil
		}
		return mcp.NewToolResultText(string(b)), nil
	}}
}

func idOption(name, description string) mcp.ToolOption {
	return mcp.WithString(name, mcp.Required(), mcp.Description(description))
}
func filterOption(name, description string) mcp.ToolOption {
	return mcp.WithString(name, mcp.Description(description))
}
func pageOptions(filters ...mcp.ToolOption) []mcp.ToolOption {
	return append(filters,
		mcp.WithNumber("limit", mcp.DefaultNumber(defaultLimit), mcp.Min(1), mcp.Max(100), mcp.Description("Maximum records on this page, 1 to 100")),
		mcp.WithString("cursor", mcp.Description("Cursor returned by the preceding page; omit for the first page")))
}
func requiredID(q mcp.CallToolRequest, name string) (string, error) {
	s, e := validate.RequiredToolParam[string](q, name)
	if e == nil && strings.TrimSpace(s) == "" {
		e = fmt.Errorf("%s must not be empty", name)
	}
	return s, e
}
func optionalString(q mcp.CallToolRequest, name string) (*string, error) {
	s, ok, e := validate.OptionalToolParam[string](q, name)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, nil
	}
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("%s must not be empty", name)
	}
	return &s, nil
}
func one(q mcp.CallToolRequest, name string) (*[]string, error) {
	s, e := optionalString(q, name)
	if e != nil || s == nil {
		return nil, e
	}
	return &[]string{*s}, nil
}
func integer(q mcp.CallToolRequest, name string, def, minValue, maxValue int) (int, error) {
	v, ok, e := validate.OptionalToolParam[float64](q, name)
	if e != nil {
		return 0, e
	}
	if !ok {
		return def, nil
	}
	if math.IsNaN(v) || math.IsInf(v, 0) || math.Trunc(v) != v || v < float64(minValue) || v > float64(maxValue) {
		return 0, fmt.Errorf("%s must be an integer from %d to %d", name, minValue, maxValue)
	}
	return int(v), nil
}
func pagination(q mcp.CallToolRequest) (*int, *string, error) {
	limit, e := integer(q, "limit", defaultLimit, 1, 100)
	if e != nil {
		return nil, nil, e
	}
	s, ok, e := validate.OptionalToolParam[string](q, "cursor")
	if e != nil {
		return nil, nil, e
	}
	if !ok || s == "" {
		return &limit, nil, nil
	}
	return &limit, &s, nil
}
func page[T any](items []T, cursor *string) any {
	return struct {
		Items      []T     `json:"items"`
		NextCursor *string `json:"nextCursor"`
	}{items, cursor}
}

func listWorkflows(r *Repo) server.ServerTool {
	return readTool("list_workflows", "Discover Workflows in the selected workspace. Returns one page and nextCursor; follow until null or an empty page.", func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
		p := &client.ListWorkflowsParams{}
		var e error
		if p.Limit, p.Cursor, e = pagination(q); e != nil {
			return nil, e
		}
		if p.Name, e = one(q, "name"); e != nil {
			return nil, e
		}
		if p.EnvironmentId, e = one(q, "environmentId"); e != nil {
			return nil, e
		}
		v, c, e := r.ListWorkflows(ctx, p)
		return page(v, c), e
	}, pageOptions(filterOption("name", "Filter by workflow name"), filterOption("environmentId", "Filter by environment ID"))...)
}
func getWorkflow(r *Repo) server.ServerTool {
	return readTool("get_workflow", "Get a Workflow's configuration and verify it belongs to the selected workspace.", func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
		id, e := requiredID(q, "workflowId")
		if e != nil {
			return nil, e
		}
		return r.GetWorkflow(ctx, id)
	}, idOption("workflowId", "Workflow ID"))
}
func listVersions(r *Repo) server.ServerTool {
	return readTool("list_workflow_versions", "List Workflow versions and their build/registration status. Use nextCursor for another page.", func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
		p := &client.ListWorkflowVersionsParams{}
		var e error
		if p.Limit, p.Cursor, e = pagination(q); e != nil {
			return nil, e
		}
		if p.WorkflowID, e = one(q, "workflowId"); e != nil {
			return nil, e
		}
		v, c, e := r.ListVersions(ctx, p)
		return page(v, c), e
	}, pageOptions(filterOption("workflowId", "Filter by workflow ID"))...)
}
func getVersion(r *Repo) server.ServerTool {
	return readTool("get_workflow_version", "Get a Workflow version's build/registration status in the selected workspace.", func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
		id, e := requiredID(q, "workflowVersionId")
		if e != nil {
			return nil, e
		}
		return r.GetVersion(ctx, id)
	}, idOption("workflowVersionId", "Workflow version ID"))
}
func taskFilters() []mcp.ToolOption {
	return []mcp.ToolOption{filterOption("workflowId", "Filter by workflow ID"), filterOption("workflowVersionId", "Filter by workflow version ID"), filterOption("taskSlug", "Filter by workflow-slug/task-name, optionally :version")}
}
func listTasks(r *Repo) server.ServerTool {
	return readTool("list_workflow_tasks", "Discover registered tasks, their IDs and versions in the selected workspace. Use nextCursor for another page.", func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
		p := &client.ListTasksParams{}
		var e error
		if p.Limit, p.Cursor, e = pagination(q); e != nil {
			return nil, e
		}
		if p.WorkflowId, e = one(q, "workflowId"); e != nil {
			return nil, e
		}
		if p.WorkflowVersionId, e = one(q, "workflowVersionId"); e != nil {
			return nil, e
		}
		if p.TaskSlug, e = one(q, "taskSlug"); e != nil {
			return nil, e
		}
		v, c, e := r.ListTasks(ctx, p)
		return page(v, c), e
	}, pageOptions(taskFilters()...)...)
}
func getTask(r *Repo) server.ServerTool {
	return readTool("get_workflow_task", "Get a registered task's name, workflow ID and version ID in the selected workspace.", func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
		id, e := requiredID(q, "taskId")
		if e != nil {
			return nil, e
		}
		return r.GetTask(ctx, id)
	}, idOption("taskId", "Task ID"))
}
func listRuns(r *Repo) server.ServerTool {
	return readTool("list_workflow_runs", "List task runs with status, attempts and parent/root relationships. A page is not the entire history. The API does not support status/time filters here. Use get_workflow_run for errors and get_workflow_run_results for results.", func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
		p := &client.ListTaskRunsParams{}
		var e error
		if p.Limit, p.Cursor, e = pagination(q); e != nil {
			return nil, e
		}
		if p.WorkflowId, e = one(q, "workflowId"); e != nil {
			return nil, e
		}
		if p.WorkflowVersionId, e = one(q, "workflowVersionId"); e != nil {
			return nil, e
		}
		if p.TaskSlug, e = one(q, "taskSlug"); e != nil {
			return nil, e
		}
		if p.RootTaskRunId, e = one(q, "rootTaskRunId"); e != nil {
			return nil, e
		}
		v, c, e := r.ListRuns(ctx, p)
		return page(v, c), e
	}, pageOptions(append(taskFilters(), filterOption("rootTaskRunId", "Filter to the run tree with this root run ID"))...)...)
}
func getRun(r *Repo) server.ServerTool {
	return readTool("get_workflow_run", "Inspect a task run's status, errors and all attempts (zero-indexed), plus parent/root relationships. Errors are capped at 4 KiB each with errorTruncated flags. Inputs/results are omitted. Use get_workflow_run_results for results. Report attempts as returned; retries and timestamps alone do not prove how many attempts ran or how long they queued. Task failure is data, not a tool failure.", func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
		id, e := requiredID(q, "taskRunId")
		if e != nil {
			return nil, e
		}
		run, e := r.getRun(ctx, id)
		if e != nil {
			return nil, e
		}
		return runMetadata(run), nil
	}, idOption("taskRunId", "Task run ID"))
}
func getResults(r *Repo) server.ServerTool {
	return readTool("get_workflow_run_results", "Read a terminal run's or attempt's results as paged JSON text, preserving arrays and integers exactly. Nonterminal states return ready=false, not a final result. Concatenate content pages, passing nextOffset and sha256 on each follow-up; nextOffset=null marks the end. Do not unwrap the API's results array or infer success from ready=true: inspect status. Failed/canceled runs may have null/empty results.", func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
		id, e := requiredID(q, "taskRunId")
		if e != nil {
			return nil, e
		}
		offset, e := integer(q, "offset", 0, 0, maxResponseBytes)
		if e != nil {
			return nil, e
		}
		limit, e := integer(q, "limit", maxResultPageBytes, 4, maxResultPageBytes)
		if e != nil {
			return nil, e
		}
		var attempt *int
		if _, ok := q.GetArguments()["attempt"]; ok {
			v, e := integer(q, "attempt", 0, 0, math.MaxInt32)
			if e != nil {
				return nil, e
			}
			attempt = &v
		}
		hash, e := optionalString(q, "sha256")
		if e != nil {
			return nil, e
		}
		expected := ""
		if hash != nil {
			expected = *hash
		}
		run, e := r.getRun(ctx, id)
		if e != nil {
			return nil, e
		}
		return pageResults(run, attempt, offset, limit, expected)
	}, idOption("taskRunId", "Task run ID"), mcp.WithNumber("attempt", mcp.Min(0), mcp.Description("Optional zero-indexed attempt number; omit for the run's results")), mcp.WithNumber("offset", mcp.Min(0), mcp.DefaultNumber(0), mcp.Description("UTF-8 byte offset, use nextOffset from the preceding page")), mcp.WithNumber("limit", mcp.Min(4), mcp.Max(maxResultPageBytes), mcp.DefaultNumber(maxResultPageBytes), mcp.Description("Maximum result bytes in this page")), filterOption("sha256", "Result hash from the first page; required for offsets greater than zero"))
}
