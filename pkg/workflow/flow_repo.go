package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/render-oss/render-mcp-server/pkg/client"
	workflowclient "github.com/render-oss/render-mcp-server/pkg/client/workflows"
)

// FlowRun and FlowAttempt mirror Run and Attempt: typed metadata, bounded
// errors, and no inputs. Results stay raw JSON for get_flow_run_results.
type FlowRun struct {
	workflowclient.FlowRun
	Attempts       []FlowAttempt                       `json:"attempts"`
	Error          *string                             `json:"error,omitempty"`
	ErrorTruncated bool                                `json:"errorTruncated,omitempty"`
	ErrorDetails   *workflowclient.FlowRunErrorDetails `json:"errorDetails,omitempty"`
}
type FlowAttempt struct {
	workflowclient.FlowRunAttempt
	Error          *string `json:"error,omitempty"`
	ErrorTruncated bool    `json:"errorTruncated,omitempty"`
}

type flowRunDetails struct {
	FlowRun
	Results json.RawMessage `json:"results"`
}

// Step omits the step's input like the other run tools, and keeps its result
// as raw JSON so large integers and explicit null values survive.
type Step struct {
	workflowclient.FlowRunStepDetails
	Error          *string         `json:"error,omitempty"`
	ErrorTruncated bool            `json:"errorTruncated,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
}

// FlowRunCancellation reports that cancellation was requested, not that the
// run has stopped.
type FlowRunCancellation struct {
	FlowRunID string `json:"flowRunId"`
	Requested bool   `json:"requested"`
	NextStep  string `json:"nextStep"`
}

func (r *Repo) GetFlow(ctx context.Context, id string) (*workflowclient.Flow, error) {
	resp, err := r.client.GetFlowWithResponse(ctx, id)
	if err = checkResponse(resp, err); err != nil {
		return nil, err
	}
	v, err := json200(resp.JSON200, resp.Body)
	if err != nil {
		return nil, err
	}
	if v.Id != id || v.WorkflowId == nil || *v.WorkflowId == "" {
		return nil, errors.New("Workflows API returned invalid flow identity")
	}
	if _, err = r.GetWorkflow(ctx, *v.WorkflowId); err != nil {
		return nil, err
	}
	return &v, nil
}
func (r *Repo) getFlowRun(ctx context.Context, id string) (*flowRunDetails, error) {
	resp, err := r.client.GetFlowRunWithResponse(ctx, id)
	if err = checkResponse(resp, err); err != nil {
		return nil, err
	}
	v, err := decodeBody[flowRunDetails](resp.Body)
	if err != nil {
		return nil, err
	}
	if v.Id != id || v.FlowId == "" {
		return nil, errors.New("Workflows API returned invalid flow run identity")
	}
	if _, err = r.GetFlow(ctx, v.FlowId); err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *Repo) GetFlowRunStep(ctx context.Context, flowRunID string, seq int, p *client.GetFlowRunStepParams) (*Step, error) {
	if _, err := r.getFlowRunSummary(ctx, flowRunID); err != nil {
		return nil, err
	}
	resp, err := r.client.GetFlowRunStepWithResponse(ctx, flowRunID, seq, p)
	if err = checkResponse(resp, err); err != nil {
		return nil, err
	}
	v, err := decodeBody[Step](resp.Body)
	if err != nil {
		return nil, err
	}
	if v.Seq != seq || (p != nil && p.StepKey != nil && v.StepKey != *p.StepKey) {
		return nil, errors.New("Workflows API returned invalid step identity")
	}
	v.Input = nil
	v.Error, v.ErrorTruncated = boundError(v.Error)
	return &v, nil
}

func (r *Repo) ListFlows(ctx context.Context, p *client.ListFlowsParams) ([]workflowclient.Flow, *string, error) {
	owner, err := workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	p.OwnerId = owner
	resp, err := r.client.ListFlowsWithResponse(ctx, p)
	if err = checkResponse(resp, err); err != nil {
		return nil, nil, err
	}
	rows, err := json200(resp.JSON200, resp.Body)
	if err != nil {
		return nil, nil, err
	}
	out := make([]workflowclient.Flow, 0, len(rows))
	var cursor *string
	for _, row := range rows {
		out = append(out, row.Flow)
		c := row.Cursor
		cursor = &c
	}
	return out, cursor, nil
}
func (r *Repo) ListFlowRuns(ctx context.Context, p *client.ListFlowRunsParams) ([]workflowclient.FlowRun, *string, error) {
	owner, err := workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	p.OwnerId = owner
	resp, err := r.client.ListFlowRunsWithResponse(ctx, p)
	if err = checkResponse(resp, err); err != nil {
		return nil, nil, err
	}
	rows, err := json200(resp.JSON200, resp.Body)
	if err != nil {
		return nil, nil, err
	}
	out := make([]workflowclient.FlowRun, 0, len(rows))
	var cursor *string
	for _, row := range rows {
		out = append(out, row.FlowRun)
		c := row.Cursor
		cursor = &c
	}
	return out, cursor, nil
}

// The run detail response includes results, which can exceed the response size
// limit. Only then fall back to scanning the workspace's run list, so actions
// that do not need results still work.
func (r *Repo) getFlowRunSummary(ctx context.Context, id string) (*workflowclient.FlowRun, error) {
	run, err := r.getFlowRun(ctx, id)
	if err == nil {
		return &run.FlowRun.FlowRun, nil
	}
	if !errors.Is(err, errResponseTooLarge) {
		return nil, err
	}
	return r.scanFlowRuns(ctx, id)
}

func (r *Repo) scanFlowRuns(ctx context.Context, id string) (*workflowclient.FlowRun, error) {
	p := &client.ListFlowRunsParams{Limit: new(100)}
	seen := map[string]bool{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		runs, cursor, err := r.ListFlowRuns(ctx, p)
		if err != nil {
			return nil, err
		}
		for _, run := range runs {
			if run.Id != id {
				continue
			}
			if run.FlowId == "" {
				return nil, errors.New("Workflows API returned invalid flow run identity")
			}
			if _, err := r.GetFlow(ctx, run.FlowId); err != nil {
				return nil, err
			}
			return &run, nil
		}
		if len(runs) == 0 || cursor == nil || *cursor == "" {
			return nil, errors.New("flow run not found in the selected workspace")
		}
		if seen[*cursor] {
			return nil, errors.New("Workflows API returned a repeated flow run cursor")
		}
		seen[*cursor] = true
		p.Cursor = cursor
	}
}

// The steps route carries no owner, so check the flow run's owner first.
func (r *Repo) ListFlowRunSteps(ctx context.Context, flowRunID string, p *client.ListFlowRunStepsParams) ([]workflowclient.FlowRunStep, *string, error) {
	if _, err := r.getFlowRunSummary(ctx, flowRunID); err != nil {
		return nil, nil, err
	}
	resp, err := r.client.ListFlowRunStepsWithResponse(ctx, flowRunID, p)
	if err = checkResponse(resp, err); err != nil {
		return nil, nil, err
	}
	rows, err := json200(resp.JSON200, resp.Body)
	if err != nil {
		return nil, nil, err
	}
	if rows.Steps == nil {
		rows.Steps = []workflowclient.FlowRunStep{}
	}
	if !rows.HasMore {
		rows.Cursor = nil
	}
	return rows.Steps, rows.Cursor, nil
}

var flowIDPattern = regexp.MustCompile(`^flw-[a-z0-9]+$`)

// StartFlowRun checks the exact flow's workspace before posting its ID, so the
// write cannot resolve a slug to a different workspace or workflow version.
func (r *Repo) StartFlowRun(ctx context.Context, body client.StartFlowRunJSONRequestBody) (*workflowclient.FlowRun, error) {
	if err := checkWriteRequest(ctx, body); err != nil {
		return nil, err
	}
	if !flowIDPattern.MatchString(body.Flow) {
		return nil, errors.New("flow must be a flow ID (flw-...) from list_flows; choose the desired workflow version before starting a run")
	}
	if _, err := r.GetFlow(ctx, body.Flow); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resp, err := r.client.StartFlowRunWithResponse(ctx, body)
	if err = checkWriteResponse(resp, err, true); err != nil {
		return nil, startOutcomeError(err)
	}
	run, err := json200(resp.JSON202, resp.Body)
	if err != nil || run.Id == "" {
		return nil, startOutcomeError(errors.New("Workflows API returned invalid flow run identity or JSON"))
	}
	return &run, nil
}

func (r *Repo) CancelFlowRun(ctx context.Context, id string) (*FlowRunCancellation, error) {
	run, err := r.getFlowRunSummary(ctx, id)
	if err != nil {
		return nil, err
	}
	if flowTerminal(run.Status) {
		return nil, fmt.Errorf("flow run is already %s; there is nothing to cancel", run.Status)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	resp, err := r.client.CancelFlowRunWithResponse(ctx, id)
	if err != nil {
		return nil, errors.New("Workflows cancel request failed; outcome unknown. Use get_flow_run to check the status")
	}
	if resp.StatusCode() != http.StatusNoContent {
		return nil, fmt.Errorf("Workflows API returned HTTP %d", resp.StatusCode())
	}
	return &FlowRunCancellation{FlowRunID: id, Requested: true,
		NextStep: "Use get_flow_run to confirm the status becomes canceled. Cancellation is not instant."}, nil
}

func startOutcomeError(err error) error {
	return fmt.Errorf("%w; the flow run may already have started. Retry with the same idempotencyKey to get that run instead of a second one, or use list_flow_runs to reconcile", err)
}
