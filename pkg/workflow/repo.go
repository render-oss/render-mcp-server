package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/render-oss/render-mcp-server/pkg/client"
	workflowclient "github.com/render-oss/render-mcp-server/pkg/client/workflows"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

const maxResponseBytes = 16 << 20

// Use the generated raw methods so response bodies are bounded before decoding.
// In particular, TaskRunResult's generated []interface{} would round large integers.
//
//go:generate go tool counterfeiter -o ../fakes/fakeworkflowrepoclient_gen.go . workflowRepoClient
type workflowRepoClient interface {
	CreateWorkflow(context.Context, client.CreateWorkflowJSONRequestBody, ...client.RequestEditorFn) (*http.Response, error)
	CreateWorkflowVersion(context.Context, client.CreateWorkflowVersionJSONRequestBody, ...client.RequestEditorFn) (*http.Response, error)
	ListWorkflows(context.Context, *client.ListWorkflowsParams, ...client.RequestEditorFn) (*http.Response, error)
	GetWorkflow(context.Context, string, ...client.RequestEditorFn) (*http.Response, error)
	ListWorkflowVersions(context.Context, *client.ListWorkflowVersionsParams, ...client.RequestEditorFn) (*http.Response, error)
	GetWorkflowVersion(context.Context, string, ...client.RequestEditorFn) (*http.Response, error)
	ListTasks(context.Context, *client.ListTasksParams, ...client.RequestEditorFn) (*http.Response, error)
	GetTask(context.Context, string, ...client.RequestEditorFn) (*http.Response, error)
	ListTaskRuns(context.Context, *client.ListTaskRunsParams, ...client.RequestEditorFn) (*http.Response, error)
	GetTaskRun(context.Context, string, ...client.RequestEditorFn) (*http.Response, error)
}

type Repo struct{ client workflowRepoClient }

func NewRepo(c workflowRepoClient) *Repo { return &Repo{client: c} }

// Run and Attempt retain typed metadata while leaving arbitrary results as JSON.
// Inputs are deliberately not returned by these debugging tools.
type Run struct {
	workflowclient.TaskRun
	Attempts       []Attempt `json:"attempts"`
	Error          *string   `json:"error,omitempty"`
	ErrorTruncated bool      `json:"errorTruncated,omitempty"`
}
type Attempt struct {
	workflowclient.TaskAttempt
	Error          *string         `json:"error,omitempty"`
	ErrorTruncated bool            `json:"errorTruncated,omitempty"`
	Results        json.RawMessage `json:"-"`
}

func (a *Attempt) UnmarshalJSON(data []byte) error {
	type plain Attempt
	var v struct {
		*plain
		Results json.RawMessage `json:"results"`
	}
	v.plain = (*plain)(a)
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	a.Results = v.Results
	return nil
}

type runDetails struct {
	Run
	Results json.RawMessage `json:"results"`
}

func decodeResponse[T any](resp *http.Response, err error) (T, error) {
	var result T
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return result, context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return result, context.DeadlineExceeded
		}
		return result, errors.New("Workflows API request failed")
	}
	if resp == nil || resp.Body == nil {
		return result, errors.New("Workflows API returned an empty response")
	}
	// Never include or log upstream error bodies: they may contain task inputs.
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("Workflows API returned HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return result, errors.New("could not read Workflows API response")
	}
	if len(b) > maxResponseBytes {
		return result, errors.New("Workflows API response exceeds 16 MiB; narrow list filters or retrieve this run directly through the API")
	}
	if len(b) == 0 {
		return result, errors.New("Workflows API returned an empty response")
	}
	if err = json.Unmarshal(b, &result); err != nil {
		return result, errors.New("Workflows API returned invalid JSON")
	}
	return result, nil
}

func workspaceOwner(ctx context.Context) (*[]string, error) {
	id, err := session.FromContext(ctx).GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if id == "" {
		return nil, errors.New("select a workspace or supply workspaceId")
	}
	return &[]string{id}, nil
}

func (r *Repo) GetWorkflow(ctx context.Context, id string) (*workflowclient.Workflow, error) {
	v, err := decodeResponse[workflowclient.Workflow](r.client.GetWorkflow(ctx, id))
	if err != nil {
		return nil, err
	}
	if v.Id != id || v.OwnerId == "" {
		return nil, errors.New("Workflows API returned invalid workflow identity")
	}
	if _, err = workspaceOwner(ctx); err != nil {
		return nil, err
	}
	if err = validate.WorkspaceMatches(ctx, v.OwnerId); err != nil {
		return nil, err
	}
	return &v, nil
}
func (r *Repo) GetVersion(ctx context.Context, id string) (*workflowclient.WorkflowVersion, error) {
	v, err := decodeResponse[workflowclient.WorkflowVersion](r.client.GetWorkflowVersion(ctx, id))
	if err != nil {
		return nil, err
	}
	if v.Id != id || v.WorkflowId == "" {
		return nil, errors.New("Workflows API returned invalid version identity")
	}
	if _, err = r.GetWorkflow(ctx, v.WorkflowId); err != nil {
		return nil, err
	}
	return &v, nil
}
func (r *Repo) GetTask(ctx context.Context, id string) (*workflowclient.Task, error) {
	v, err := decodeResponse[workflowclient.Task](r.client.GetTask(ctx, id))
	if err != nil {
		return nil, err
	}
	if v.Id != id || v.WorkflowId == nil || *v.WorkflowId == "" {
		return nil, errors.New("Workflows API returned invalid task identity")
	}
	if _, err = r.GetWorkflow(ctx, *v.WorkflowId); err != nil {
		return nil, err
	}
	return &v, nil
}
func (r *Repo) getRun(ctx context.Context, id string) (*runDetails, error) {
	v, err := decodeResponse[runDetails](r.client.GetTaskRun(ctx, id))
	if err != nil {
		return nil, err
	}
	if v.Id != id || v.TaskId == "" {
		return nil, errors.New("Workflows API returned invalid run identity")
	}
	if _, err = r.GetTask(ctx, v.TaskId); err != nil {
		return nil, err
	}
	return &v, nil
}

// Lists always include the selected workspace. Filters and cursor are passed
// through to the API; a call fetches one page, never an unbounded history.
func (r *Repo) ListWorkflows(ctx context.Context, p *client.ListWorkflowsParams) ([]workflowclient.Workflow, *string, error) {
	owner, err := workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	p.OwnerId = owner
	rows, err := decodeResponse[[]client.WorkflowWithCursor](r.client.ListWorkflows(ctx, p))
	if err != nil {
		return nil, nil, err
	}
	out := make([]workflowclient.Workflow, 0, len(rows))
	var cursor *string
	for _, row := range rows {
		if row.Workflow.Id == "" || row.Workflow.OwnerId == "" {
			return nil, nil, errors.New("Workflows API returned invalid workflow identity")
		}
		if err = validate.WorkspaceMatches(ctx, row.Workflow.OwnerId); err != nil {
			return nil, nil, err
		}
		out = append(out, row.Workflow)
		c := row.Cursor
		cursor = &c
	}
	return out, cursor, nil
}
func (r *Repo) ListVersions(ctx context.Context, p *client.ListWorkflowVersionsParams) ([]workflowclient.WorkflowVersion, *string, error) {
	owner, err := workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	p.OwnerId = owner
	rows, err := decodeResponse[[]client.WorkflowVersionWithCursor](r.client.ListWorkflowVersions(ctx, p))
	if err != nil {
		return nil, nil, err
	}
	out := make([]workflowclient.WorkflowVersion, 0, len(rows))
	var cursor *string
	for _, row := range rows {
		out = append(out, row.WorkflowVersion)
		c := row.Cursor
		cursor = &c
	}
	return out, cursor, nil
}
func (r *Repo) ListTasks(ctx context.Context, p *client.ListTasksParams) ([]workflowclient.Task, *string, error) {
	owner, err := workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	p.OwnerId = owner
	rows, err := decodeResponse[[]client.TaskWithCursor](r.client.ListTasks(ctx, p))
	if err != nil {
		return nil, nil, err
	}
	out := make([]workflowclient.Task, 0, len(rows))
	var cursor *string
	for _, row := range rows {
		out = append(out, row.Task)
		c := row.Cursor
		cursor = &c
	}
	return out, cursor, nil
}
func (r *Repo) ListRuns(ctx context.Context, p *client.ListTaskRunsParams) ([]workflowclient.TaskRun, *string, error) {
	owner, err := workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	p.OwnerId = owner
	rows, err := decodeResponse[[]client.TaskRunWithCursor](r.client.ListTaskRuns(ctx, p))
	if err != nil {
		return nil, nil, err
	}
	out := make([]workflowclient.TaskRun, 0, len(rows))
	var cursor *string
	for _, row := range rows {
		out = append(out, row.TaskRun)
		c := row.Cursor
		cursor = &c
	}
	return out, cursor, nil
}
