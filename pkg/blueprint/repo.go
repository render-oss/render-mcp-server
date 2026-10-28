package blueprint

import (
	"context"

	"github.com/render-oss/render-mcp-server/pkg/client"
	blueprintclient "github.com/render-oss/render-mcp-server/pkg/client/blueprints"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

//go:generate go tool counterfeiter -o ../fakes/fakeblueprintrepoclient_gen.go . blueprintRepoClient
type blueprintRepoClient interface {
	CreateBlueprintWithResponse(ctx context.Context, body client.CreateBlueprintJSONRequestBody, reqEditors ...client.RequestEditorFn) (*client.CreateBlueprintResponse, error)
	ApproveBlueprintWithResponse(ctx context.Context, blueprintID blueprintclient.BlueprintId, body client.ApproveBlueprintJSONRequestBody, reqEditors ...client.RequestEditorFn) (*client.ApproveBlueprintResponse, error)
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
	return client.BodyFromResponse(response.JSON201, response)
}

func (r *Repo) ApproveBlueprint(ctx context.Context, blueprintID string, input client.ApproveBlueprintJSONRequestBody) (*blueprintclient.ApprovedSync, error) {
	// The Blueprint ID identifies the target; REST authorizes access independently
	// of the selected MCP workspace.
	response, err := r.client.ApproveBlueprintWithResponse(ctx, blueprintID, input)
	if err != nil {
		return nil, err
	}
	return client.BodyFromResponse(response.JSON200, response)
}
