package workflow

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	workflowclient "github.com/render-oss/render-mcp-server/pkg/client/workflows"
	"github.com/render-oss/render-mcp-server/pkg/mcpserver"
)

// Flow runs pause only for Durability 2.1 sleep and detached steps.
func flowRunStatuses(opts Options) []string {
	statuses := []workflowclient.FlowRunStatus{workflowclient.FlowRunStatusPending, workflowclient.FlowRunStatusRunning, workflowclient.FlowRunStatusSucceeded, workflowclient.FlowRunStatusFailed, workflowclient.FlowRunStatusCanceled}
	if opts.Durability21 {
		statuses = append(statuses, workflowclient.FlowRunStatusPaused)
	}
	return mcpserver.EnumValuesFromClientType(statuses...)
}

// FlowSummary adds the slug list_flow_runs takes, so the model does not have
// to assemble it from get_workflow.
type FlowSummary struct {
	workflowclient.Flow
	Slug *string `json:"slug,omitempty"`
}

func (r *Repo) listFlowSummaries(ctx context.Context, p *client.ListFlowsParams) ([]FlowSummary, *string, error) {
	flows, cursor, err := r.ListFlows(ctx, p)
	if err != nil {
		return nil, nil, err
	}
	workflows := map[string]*workflowclient.Workflow{}
	out := make([]FlowSummary, 0, len(flows))
	for _, f := range flows {
		s := FlowSummary{Flow: f}
		if f.WorkflowId != nil {
			w, ok := workflows[*f.WorkflowId]
			if !ok {
				w, err = r.GetWorkflow(ctx, *f.WorkflowId)
				if err != nil {
					if ctx.Err() != nil {
						return nil, nil, ctx.Err()
					}
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return nil, nil, err
					}
					w = nil
				}
				workflows[*f.WorkflowId] = w
			}
			if w == nil {
				continue
			}
			if w.Slug != nil && *w.Slug != "" {
				s.Slug = new(*w.Slug + "/" + f.Name)
			}
		}
		out = append(out, s)
	}
	return out, cursor, nil
}

func listFlows(r *Repo) server.ServerTool {
	return readTool(toolSpec{
		Name:        "list_flows",
		Description: "Discover registered flows, their IDs and slugs in the selected workspace. Flows whose workflow cannot be retrieved are omitted. Each workflow version registers its own flows, so the same name can appear once per version. Filter by workflowVersionId to choose a version, then pass the flow's id (flw-...) to start_flow_run. Use nextCursor for another page, even if this page is empty.",
		Handler: func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
			p := &client.ListFlowsParams{}
			var e error
			if p.Limit, p.Cursor, e = pagination(q); e != nil {
				return nil, e
			}
			if p.WorkflowVersionId, e = one(q, "workflowVersionId"); e != nil {
				return nil, e
			}
			if p.FlowSlug, e = one(q, "flowSlug"); e != nil {
				return nil, e
			}
			v, c, e := r.listFlowSummaries(ctx, p)
			return page(v, c), e
		},
		Options: pageOptions(filterOption("workflowVersionId", "Filter by workflow version ID, for example a version from create_workflow_version"), filterOption("flowSlug", "Filter by workflow-slug/flow-name, optionally :version")),
	})
}
func getFlow(r *Repo) server.ServerTool {
	return readTool(toolSpec{
		Name:        "get_flow",
		Description: "Get a registered flow's name, workflow ID and version ID in the selected workspace.",
		Handler: func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
			id, e := requiredID(q, "flowId")
			if e != nil {
				return nil, e
			}
			return r.GetFlow(ctx, id)
		},
		Options: []mcp.ToolOption{
			idOption("flowId", "Flow ID"),
		},
	})
}
func listFlowRuns(r *Repo, opts Options) server.ServerTool {
	statuses := flowRunStatuses(opts)
	return readTool(toolSpec{
		Name:        "list_flow_runs",
		Description: "List flow runs with status and timestamps. A page is not the entire history. Use get_flow_run for errors and attempts, get_flow_run_results for results, and list_flow_run_steps for steps.",
		Handler: func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
			p := &client.ListFlowRunsParams{}
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
			if p.FlowId, e = one(q, "flowId"); e != nil {
				return nil, e
			}
			if p.FlowSlug, e = one(q, "flowSlug"); e != nil {
				return nil, e
			}
			status, e := optionalString(q, "status")
			if e != nil {
				return nil, e
			}
			if status != nil {
				s := workflowclient.FlowRunStatus(*status)
				if !s.Valid() || !slices.Contains(statuses, *status) {
					return nil, fmt.Errorf("status must be one of %s", strings.Join(statuses, ", "))
				}
				p.Status = &[]workflowclient.FlowRunStatus{s}
			}
			v, c, e := r.ListFlowRuns(ctx, p)
			return page(v, c), e
		},
		Options: pageOptions(filterOption("workflowId", "Filter by workflow ID"), filterOption("workflowVersionId", "Filter by workflow version ID"), filterOption("flowId", "Filter by flow ID"), filterOption("flowSlug", "Filter by workflow-slug/flow-name, optionally :version"), mcp.WithString("status", mcp.Enum(statuses...), mcp.Description("Filter by flow run status"))),
	})
}
func getFlowRun(r *Repo) server.ServerTool {
	return readTool(toolSpec{
		Name:        "get_flow_run",
		Description: "Inspect a flow run's status, error, error details and attempts (zero-indexed). A flow run gets a new attempt when it resumes after its process is interrupted; it replays completed steps rather than rerunning them. Errors are capped at 4 KiB each with errorTruncated flags. Inputs and results are omitted: use get_flow_run_results for results, list_flow_run_steps for steps, and list_logs with the workflow ID as resource and this ID as taskRun for logs. Poll this tool to wait for a run started with start_flow_run. Flow failure is data, not a tool failure.",
		Handler: func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
			id, e := requiredID(q, "flowRunId")
			if e != nil {
				return nil, e
			}
			run, e := r.getFlowRun(ctx, id)
			if e != nil {
				return nil, e
			}
			return flowRunMetadata(run), nil
		},
		Options: []mcp.ToolOption{
			idOption("flowRunId", "Flow run ID"),
		},
	})
}
func getFlowRunResults(r *Repo) server.ServerTool {
	return readTool(toolSpec{
		Name:        "get_flow_run_results",
		Description: "Read a terminal flow run's results as paged JSON text, preserving arrays and integers exactly. Nonterminal runs return ready=false, not a final result. Concatenate content pages, passing nextOffset and sha256 on each follow-up; nextOffset=null marks the end. Do not infer success from ready=true: inspect status. Failed and canceled runs may have null or empty results.",
		Handler: func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
			id, e := requiredID(q, "flowRunId")
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
			hash, e := optionalString(q, "sha256")
			if e != nil {
				return nil, e
			}
			expected := ""
			if hash != nil {
				expected = *hash
			}
			run, e := r.getFlowRun(ctx, id)
			if e != nil {
				return nil, e
			}
			return pageFlowResults(run, offset, limit, expected)
		},
		Options: []mcp.ToolOption{
			idOption("flowRunId", "Flow run ID"),
			mcp.WithNumber("offset", mcp.Min(0), mcp.DefaultNumber(0), mcp.Description("UTF-8 byte offset, use nextOffset from the preceding page")),
			mcp.WithNumber("limit", mcp.Min(4), mcp.Max(maxResultPageBytes), mcp.DefaultNumber(maxResultPageBytes), mcp.Description("Maximum result bytes in this page")),
			filterOption("sha256", "Result hash from the first page; required for offsets greater than zero"),
		},
	})
}
func listFlowRunSteps(r *Repo, opts Options) server.ServerTool {
	description := "List a flow run's steps in the order the flow started them, with type, status, and timestamps. Use get_flow_run_step for a step's result and error."
	if opts.Durability21 {
		description += " A detached step's childTaskRunId works with get_workflow_run, get_workflow_run_results, and list_logs. A sleep step's wakeAt says when it resumes."
	}
	return readTool(toolSpec{
		Name:        "list_flow_run_steps",
		Description: description,
		Handler: func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
			id, e := requiredID(q, "flowRunId")
			if e != nil {
				return nil, e
			}
			p := &client.ListFlowRunStepsParams{}
			if p.Limit, p.Cursor, e = pagination(q); e != nil {
				return nil, e
			}
			v, c, e := r.ListFlowRunSteps(ctx, id, p)
			return page(v, c), e
		},
		Options: pageOptions(idOption("flowRunId", "Flow run ID")),
	})
}
func getFlowRunStep(r *Repo, opts Options) server.ServerTool {
	description := "Get one flow run step's status, result, and error. The step's input is omitted. Errors are capped at 4 KiB each with errorTruncated flags. result is present only when the step succeeded; null means the step returned no value."
	if opts.Durability21 {
		description += " For a detached step, inspect the child run with get_workflow_run."
	}
	return readTool(toolSpec{
		Name:        "get_flow_run_step",
		Description: description,
		Handler: func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
			id, e := requiredID(q, "flowRunId")
			if e != nil {
				return nil, e
			}
			if _, ok := q.GetArguments()["seq"]; !ok {
				return nil, errors.New("seq is required; use list_flow_run_steps to find step numbers")
			}
			seq, e := integer(q, "seq", 0, 0, math.MaxInt32)
			if e != nil {
				return nil, e
			}
			stepKey, e := optionalString(q, "stepKey")
			if e != nil {
				return nil, e
			}
			return r.GetFlowRunStep(ctx, id, seq, &client.GetFlowRunStepParams{StepKey: stepKey})
		},
		Options: []mcp.ToolOption{
			idOption("flowRunId", "Flow run ID"),
			mcp.WithNumber("seq", mcp.Required(), mcp.Min(0), mcp.Description("The step's seq from list_flow_run_steps")),
			filterOption("stepKey", "The stepKey from list_flow_run_steps, used to distinguish steps with the same seq. Omit for a step recorded without a key"),
		},
	})
}

func startFlowRun(r *Repo) server.ServerTool {
	return writeTool(toolSpec{
		Name:        "start_flow_run",
		Description: "Start a flow run in the selected workspace using an exact flow ID from list_flows. This runs the user's code, so start a run only when asked to. The ID selects a registered workflow version; it does not automatically select the latest version. After create_workflow_version, wait until get_workflow_version reports the new version ready, then use list_flows with that workflowVersionId to find its flow ID. Returns the new run, which starts pending; poll get_flow_run until it is terminal, then read get_flow_run_results, list_flow_run_steps, and list_logs. Always pass an idempotencyKey. If the call times out or disconnects, retry with the same key to get the same run, never without one.",
		Handler: func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
			id, err := requiredID(q, "flow")
			if err != nil {
				return nil, err
			}
			body := client.StartFlowRunJSONRequestBody{Flow: id}
			if body.IdempotencyKey, err = optionalString(q, "idempotencyKey"); err != nil {
				return nil, err
			}
			if err = flowInput(q, &body.Input); err != nil {
				return nil, err
			}
			return r.StartFlowRun(ctx, body)
		},
		Options: []mcp.ToolOption{
			idOption("flow", "Exact flow ID (flw-...) returned as id by list_flows. Select the desired workflowVersionId when listing flows. Slugs are not accepted"),
			mcp.WithAny("input", mcp.Description("The flow's arguments: an array for positional arguments or an object for named ones. Omit for a flow that takes none. The request is limited to 256 KiB")),
			filterOption("idempotencyKey", "A fresh UUID for this run. Repeating it within 24 hours returns the first run instead of starting another, and ignores the repeated input. Keys are scoped to the workflow"),
		},
	})
}

func flowInput(q mcp.CallToolRequest, input *workflowclient.FlowInput) error {
	raw, ok := q.GetArguments()["input"]
	if !ok || raw == nil {
		return input.FromFlowInput0(workflowclient.FlowInput0{})
	}
	switch v := raw.(type) {
	case []any:
		return input.FromFlowInput0(v)
	case map[string]any:
		return input.FromFlowInput1(v)
	}
	return errors.New("input must be an array of positional arguments or an object of named arguments")
}

func cancelFlowRun(r *Repo, opts Options) server.ServerTool {
	states := "pending or running"
	if opts.Durability21 {
		states = "pending, running, or paused"
	}
	return writeTool(toolSpec{
		Name:        "cancel_flow_run",
		Description: "Request cancellation of a " + states + " flow run. Cancellation stops the flow and cannot be undone. Returns once the request is accepted; poll get_flow_run until the status is canceled.",
		Destructive: true,
		Handler: func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
			id, err := requiredID(q, "flowRunId")
			if err != nil {
				return nil, err
			}
			return r.CancelFlowRun(ctx, id)
		},
		Options: []mcp.ToolOption{
			idOption("flowRunId", "Exact ID of the flow run to cancel in the selected workspace"),
		},
	})
}
