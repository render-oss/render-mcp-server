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
