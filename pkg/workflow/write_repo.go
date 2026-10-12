package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	resp, err := r.client.CreateWorkflow(ctx, body)
	data, err := readWriteResponse(resp, err, false)
	if err != nil {
		return nil, createOutcomeError(err)
	}
	var value workflowclient.Workflow
	if err := json.Unmarshal(data, &value); err != nil || value.Id == "" || value.OwnerId != body.OwnerId {
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
	resp, err := r.client.CreateWorkflowVersion(ctx, body)
	data, err := readWriteResponse(resp, err, true)
	if err != nil {
		return nil, versionOutcomeError(err)
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

// Keep the existing Workflows raw-client boundary: generated WithResponse
// methods buffer bodies before decoding, and shared API error helpers can log
// response bodies containing secrets. Reads retain their strict 200 decoder.
// Creation returns 201. Version deployment currently returns 201 JSON in the
// backend, while the published contract permits an empty 202 acceptance.
func readWriteResponse(resp *http.Response, requestErr error, allowAccepted bool) ([]byte, error) {
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if requestErr != nil {
		if errors.Is(requestErr, context.Canceled) {
			return nil, errors.New("Workflows write canceled; outcome unknown")
		}
		if errors.Is(requestErr, context.DeadlineExceeded) {
			return nil, errors.New("Workflows write timed out; outcome unknown")
		}
		return nil, errors.New("Workflows write response unavailable; outcome unknown")
	}
	if resp == nil {
		return nil, errors.New("Workflows write response unavailable; outcome unknown")
	}
	if resp.StatusCode != http.StatusCreated && !(allowAccepted && resp.StatusCode == http.StatusAccepted) {
		return nil, fmt.Errorf("Workflows API returned HTTP %d", resp.StatusCode)
	}
	var data []byte
	if resp.Body != nil {
		var err error
		data, err = io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		if err != nil {
			return nil, errors.New("could not read Workflows write response; outcome unknown")
		}
	}
	if len(data) > maxResponseBytes {
		return nil, errors.New("Workflows write response exceeds 16 MiB")
	}
	if len(bytes.TrimSpace(data)) == 0 && resp.StatusCode != http.StatusAccepted {
		return nil, errors.New("Workflows API returned an unexpected empty creation response")
	}
	return data, nil
}
