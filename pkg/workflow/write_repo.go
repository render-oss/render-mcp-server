package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/render-oss/render-mcp-server/pkg/client"
	workflowclient "github.com/render-oss/render-mcp-server/pkg/client/workflows"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

const maxWriteRequestBytes = 256 << 10

// VersionCreation reports acceptance, not a completed build or registration.
// A documented 202 may have no body, so Version is optional.
type VersionCreation struct {
	WorkflowID string                          `json:"workflowId"`
	Accepted   bool                            `json:"accepted"`
	Version    *workflowclient.WorkflowVersion `json:"version,omitempty"`
	NextStep   string                          `json:"nextStep"`
}

func checkWriteRequest(ctx context.Context, body any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := json.Marshal(body)
	if err != nil {
		return errors.New("cannot encode Workflows request")
	}
	if len(b) > maxWriteRequestBytes {
		return errors.New("Workflows request exceeds 256 KiB; reduce configuration size before retrying")
	}
	return nil
}

func (r *Repo) Create(ctx context.Context, body client.CreateWorkflowJSONRequestBody) (*workflowclient.Workflow, error) {
	if _, err := workspaceOwner(ctx); err != nil {
		return nil, err
	}
	if err := validate.WorkspaceMatches(ctx, body.OwnerId); err != nil {
		return nil, err
	}
	if err := checkWriteRequest(ctx, body); err != nil {
		return nil, err
	}
	resp, err := r.client.CreateWorkflowWithResponse(ctx, body)
	if err = checkWriteResponse(resp, err, false); err != nil {
		return nil, createOutcomeError(err)
	}
	value, err := json200(resp.JSON201, resp.Body)
	if err != nil || value.Id == "" || value.OwnerId != body.OwnerId {
		return nil, createOutcomeError(errors.New("Workflows API returned invalid workflow identity or JSON"))
	}
	return &value, nil
}

func (r *Repo) CreateVersion(ctx context.Context, body client.CreateWorkflowVersionJSONRequestBody) (*VersionCreation, error) {
	if err := checkWriteRequest(ctx, body); err != nil {
		return nil, err
	}
	// GetWorkflow verifies the exact ID and owner before the deployment POST.
	if _, err := r.GetWorkflow(ctx, body.WorkflowId); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resp, err := r.client.CreateWorkflowVersionWithResponse(ctx, body)
	if err = checkWriteResponse(resp, err, true); err != nil {
		return nil, versionOutcomeError(err)
	}
	data := resp.Body
	if len(bytes.TrimSpace(data)) == 0 && resp.StatusCode() != http.StatusAccepted {
		return nil, versionOutcomeError(errors.New("Workflows API returned an unexpected empty creation response"))
	}
	result := &VersionCreation{WorkflowID: body.WorkflowId, Accepted: true,
		NextStep: "Use list_workflow_versions with workflowId, then get_workflow_version to check build and registration status. Do not request another deployment just because this one is not ready yet."}
	if len(bytes.TrimSpace(data)) == 0 {
		return result, nil
	}
	var version workflowclient.WorkflowVersion
	if err := json.Unmarshal(data, &version); err != nil || version.Id == "" || version.WorkflowId != body.WorkflowId {
		return nil, versionOutcomeError(errors.New("Workflows API returned invalid version identity or JSON"))
	}
	result.Version = &version
	result.NextStep = "Use get_workflow_version with the returned version ID to check build and registration status. Acceptance alone does not mean the version is ready."
	return result, nil
}

func createOutcomeError(err error) error {
	return fmt.Errorf("%w; a workflow may already exist. Use list_workflows in this workspace to reconcile before retrying; do not automatically repeat creation", err)
}
func versionOutcomeError(err error) error {
	return fmt.Errorf("%w; a deployment may already have started. Use list_workflow_versions for this workflow to reconcile before retrying; do not automatically repeat deployment", err)
}

// checkWriteResponse maps write failures without returning upstream bodies,
// unlike the shared API error helpers. Creation returns 201. Version
// deployment currently returns 201 JSON in the backend, while the published
// contract permits an empty 202 acceptance.
func checkWriteResponse(resp apiResponse, requestErr error, allowAccepted bool) error {
	if requestErr != nil {
		switch {
		case errors.Is(requestErr, context.Canceled):
			return errors.New("Workflows write canceled; outcome unknown")
		case errors.Is(requestErr, context.DeadlineExceeded):
			return errors.New("Workflows write timed out; outcome unknown")
		case errors.Is(requestErr, errResponseTooLarge):
			return errors.New("Workflows write response exceeds 16 MiB")
		case !isTransportError(requestErr):
			return errors.New("Workflows API returned invalid JSON; outcome unknown")
		}
		return errors.New("Workflows write response unavailable; outcome unknown")
	}
	if status := resp.StatusCode(); status != http.StatusCreated && !(allowAccepted && status == http.StatusAccepted) {
		return fmt.Errorf("Workflows API returned HTTP %d", status)
	}
	return nil
}
