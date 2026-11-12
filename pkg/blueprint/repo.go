package blueprint

import (
	"context"
	"net/http"

	"github.com/render-oss/render-mcp-server/pkg/client"
	blueprintclient "github.com/render-oss/render-mcp-server/pkg/client/blueprints"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

//go:generate go tool counterfeiter -o ../fakes/fakeblueprintrepoclient_gen.go . blueprintRepoClient
type blueprintRepoClient interface {
	RetrieveBlueprintWithResponse(ctx context.Context, blueprintID blueprintclient.BlueprintId, reqEditors ...client.RequestEditorFn) (*client.RetrieveBlueprintResponse, error)
	CreateBlueprintWithResponse(ctx context.Context, body client.CreateBlueprintJSONRequestBody, reqEditors ...client.RequestEditorFn) (*client.CreateBlueprintResponse, error)
	ApproveBlueprintWithResponse(ctx context.Context, blueprintID blueprintclient.BlueprintId, body client.ApproveBlueprintJSONRequestBody, reqEditors ...client.RequestEditorFn) (*client.ApproveBlueprintResponse, error)
	CreateBlueprintSyncWithResponse(ctx context.Context, blueprintID blueprintclient.BlueprintId, reqEditors ...client.RequestEditorFn) (*client.CreateBlueprintSyncResponse, error)
	UpdateBlueprintWithResponse(ctx context.Context, blueprintID blueprintclient.BlueprintId, body client.UpdateBlueprintJSONRequestBody, reqEditors ...client.RequestEditorFn) (*client.UpdateBlueprintResponse, error)
}

type Repo struct {
	client blueprintRepoClient
}

func NewRepo(c blueprintRepoClient) *Repo {
	return &Repo{client: c}
}

func (r *Repo) CreateBlueprint(ctx context.Context, input client.CreateBlueprintJSONRequestBody) (*blueprintclient.CreateBlueprintResponse, error) {
	if err := validate.WorkspaceMatches(ctx, input.OwnerId); err != nil {
		return nil, err
	}

	response, err := r.client.CreateBlueprintWithResponse(ctx, input)
	if err != nil {
		return nil, err
	}
	return client.BodyFromResponse(ctx, response.JSON201, response)
}

func (r *Repo) ApproveBlueprint(ctx context.Context, blueprintID string, input client.ApproveBlueprintJSONRequestBody) (*blueprintclient.ApprovedSync, error) {
	response, err := r.client.ApproveBlueprintWithResponse(ctx, blueprintID, input)
	if err != nil {
		return nil, err
	}
	return client.BodyFromResponse(ctx, response.JSON200, response)
}

func (r *Repo) PlanBlueprint(ctx context.Context, blueprintID string) (*blueprintclient.CreateBlueprintSyncResponse, error) {
	response, err := r.client.CreateBlueprintSyncWithResponse(ctx, blueprintID)
	if err != nil {
		return nil, err
	}
	if response.StatusCode() == http.StatusCreated {
		return client.BodyFromResponse(ctx, response.JSON201, response)
	}
	return client.BodyFromResponse(ctx, response.JSON200, response)
}

func (r *Repo) UpdateBlueprint(ctx context.Context, blueprintID string, input client.UpdateBlueprintJSONRequestBody) (*blueprintclient.Blueprint, error) {
	response, err := r.client.UpdateBlueprintWithResponse(ctx, blueprintID, input)
	if err != nil {
		return nil, err
	}
	return client.BodyFromResponse(ctx, response.JSON200, response)
}

func (r *Repo) CheckBlueprintWorkspace(ctx context.Context, blueprintID string) error {
	_, err := r.RetrieveBlueprintAndCheckWorkspace(ctx, blueprintID)
	return err
}

func (r *Repo) RetrieveBlueprintAndCheckWorkspace(ctx context.Context, blueprintID string) (*blueprintclient.BlueprintDetail, error) {
	response, err := r.client.RetrieveBlueprintWithResponse(ctx, blueprintID)
	if err != nil {
		return nil, err
	}
	blueprint, err := client.BodyFromResponse(ctx, response.JSON200, response)
	if err != nil {
		return nil, err
	}
	if err := validate.WorkspaceMatches(ctx, blueprint.OwnerId); err != nil {
		return nil, err
	}
	return blueprint, nil
}
