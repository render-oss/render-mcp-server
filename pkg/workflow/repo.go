package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/render-oss/render-mcp-server/pkg/client"
	workflowclient "github.com/render-oss/render-mcp-server/pkg/client/workflows"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

const maxResponseBytes = 16 << 20

// Run results are decoded from Body rather than JSON200: TaskRunResult's
// generated []interface{} would round large integers.
//
//go:generate go tool counterfeiter -o ../fakes/fakeworkflowrepoclient_gen.go . workflowRepoClient
type workflowRepoClient interface {
	CreateWorkflowWithResponse(context.Context, client.CreateWorkflowJSONRequestBody, ...client.RequestEditorFn) (*client.CreateWorkflowResponse, error)
	CreateWorkflowVersionWithResponse(context.Context, client.CreateWorkflowVersionJSONRequestBody, ...client.RequestEditorFn) (*client.CreateWorkflowVersionResponse, error)
	ListWorkflowsWithResponse(context.Context, *client.ListWorkflowsParams, ...client.RequestEditorFn) (*client.ListWorkflowsResponse, error)
	GetWorkflowWithResponse(context.Context, string, ...client.RequestEditorFn) (*client.GetWorkflowResponse, error)
	ListWorkflowVersionsWithResponse(context.Context, *client.ListWorkflowVersionsParams, ...client.RequestEditorFn) (*client.ListWorkflowVersionsResponse, error)
	GetWorkflowVersionWithResponse(context.Context, string, ...client.RequestEditorFn) (*client.GetWorkflowVersionResponse, error)
	ListTasksWithResponse(context.Context, *client.ListTasksParams, ...client.RequestEditorFn) (*client.ListTasksResponse, error)
	GetTaskWithResponse(context.Context, string, ...client.RequestEditorFn) (*client.GetTaskResponse, error)
	ListTaskRunsWithResponse(context.Context, *client.ListTaskRunsParams, ...client.RequestEditorFn) (*client.ListTaskRunsResponse, error)
	GetTaskRunWithResponse(context.Context, string, ...client.RequestEditorFn) (*client.GetTaskRunResponse, error)
	ListFlowsWithResponse(context.Context, *client.ListFlowsParams, ...client.RequestEditorFn) (*client.ListFlowsResponse, error)
	GetFlowWithResponse(context.Context, string, ...client.RequestEditorFn) (*client.GetFlowResponse, error)
	ListFlowRunsWithResponse(context.Context, *client.ListFlowRunsParams, ...client.RequestEditorFn) (*client.ListFlowRunsResponse, error)
	GetFlowRunWithResponse(context.Context, string, ...client.RequestEditorFn) (*client.GetFlowRunResponse, error)
	StartFlowRunWithResponse(context.Context, client.StartFlowRunJSONRequestBody, ...client.RequestEditorFn) (*client.StartFlowRunResponse, error)
	CancelFlowRunWithResponse(context.Context, string, ...client.RequestEditorFn) (*client.CancelFlowRunResponse, error)
	ListFlowRunStepsWithResponse(context.Context, string, *client.ListFlowRunStepsParams, ...client.RequestEditorFn) (*client.ListFlowRunStepsResponse, error)
	GetFlowRunStepWithResponse(context.Context, string, int, *client.GetFlowRunStepParams, ...client.RequestEditorFn) (*client.GetFlowRunStepResponse, error)
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

var errResponseTooLarge = errors.New("Workflows API response exceeds 16 MiB; narrow list filters or retrieve this run directly through the API")

// limitResponses bounds response bodies before the generated client reads them.
func limitResponses(c *client.ClientWithResponses) *client.ClientWithResponses {
	if c == nil {
		return nil
	}
	raw, ok := c.ClientInterface.(*client.Client)
	if !ok {
		panic("workflow: cannot bound responses for this client type")
	}
	limited := *raw
	limited.Client = limitedDoer{raw.Client}
	return &client.ClientWithResponses{ClientInterface: &limited}
}

type limitedDoer struct{ client.HttpRequestDoer }

func (d limitedDoer) Do(req *http.Request) (*http.Response, error) {
	resp, err := d.HttpRequestDoer.Do(req)
	if resp == nil || resp.Body == nil {
		return resp, err
	}
	// Error bodies are never used, and a body or header that fails to parse
	// would hide the status, so the generated parser only sees the status.
	if resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.CopyN(io.Discard, resp.Body, 4<<10)
		resp.Body.Close()
		resp.Body = http.NoBody
		resp.Header = http.Header{}
		return resp, err
	}
	resp.Body = &limitedBody{ReadCloser: resp.Body, remaining: maxResponseBytes}
	return resp, err
}

type limitedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if int64(len(p)) > b.remaining+1 {
		p = p[:b.remaining+1]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	if b.remaining < 0 {
		return 0, errResponseTooLarge
	}
	return n, err
}

type apiResponse interface{ StatusCode() int }

// checkResponse never includes or logs upstream error bodies: they may contain
// task inputs.
func checkResponse(resp apiResponse, err error) error {
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			return context.Canceled
		case errors.Is(err, context.DeadlineExceeded):
			return context.DeadlineExceeded
		case errors.Is(err, errResponseTooLarge):
			return errResponseTooLarge
		case !isTransportError(err):
			return errors.New("Workflows API returned invalid JSON")
		}
		return errors.New("Workflows API request failed")
	}
	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("Workflows API returned HTTP %d", resp.StatusCode())
	}
	return nil
}

// isTransportError reports whether err came from sending the request rather
// than from parsing a response that arrived.
func isTransportError(err error) bool {
	var urlErr *url.Error
	return errors.As(err, &urlErr)
}

// json200 falls back to Body when the response had no JSON content type.
func json200[T any](v *T, body []byte) (T, error) {
	if v != nil {
		return *v, nil
	}
	return decodeBody[T](body)
}

func decodeBody[T any](body []byte) (T, error) {
	var result T
	if len(body) == 0 {
		return result, errors.New("Workflows API returned an empty response")
	}
	if err := json.Unmarshal(body, &result); err != nil {
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
	resp, err := r.client.GetWorkflowWithResponse(ctx, id)
	if err = checkResponse(resp, err); err != nil {
		return nil, err
	}
	v, err := json200(resp.JSON200, resp.Body)
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
	resp, err := r.client.GetWorkflowVersionWithResponse(ctx, id)
	if err = checkResponse(resp, err); err != nil {
		return nil, err
	}
	v, err := json200(resp.JSON200, resp.Body)
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
	resp, err := r.client.GetTaskWithResponse(ctx, id)
	if err = checkResponse(resp, err); err != nil {
		return nil, err
	}
	v, err := json200(resp.JSON200, resp.Body)
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
	resp, err := r.client.GetTaskRunWithResponse(ctx, id)
	if err = checkResponse(resp, err); err != nil {
		return nil, err
	}
	v, err := decodeBody[runDetails](resp.Body)
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
	resp, err := r.client.ListWorkflowsWithResponse(ctx, p)
	if err = checkResponse(resp, err); err != nil {
		return nil, nil, err
	}
	rows, err := json200(resp.JSON200, resp.Body)
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
	resp, err := r.client.ListWorkflowVersionsWithResponse(ctx, p)
	if err = checkResponse(resp, err); err != nil {
		return nil, nil, err
	}
	rows, err := json200(resp.JSON200, resp.Body)
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
	resp, err := r.client.ListTasksWithResponse(ctx, p)
	if err = checkResponse(resp, err); err != nil {
		return nil, nil, err
	}
	rows, err := json200(resp.JSON200, resp.Body)
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
	resp, err := r.client.ListTaskRunsWithResponse(ctx, p)
	if err = checkResponse(resp, err); err != nil {
		return nil, nil, err
	}
	rows, err := json200(resp.JSON200, resp.Body)
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
