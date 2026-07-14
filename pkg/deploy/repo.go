package deploy

import (
	"context"
	"net/http"

	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

//go:generate go tool counterfeiter -o ../fakes/fakedeployrepoclient_gen.go . deployRepoClient
type deployRepoClient interface {
	ListDeploysWithResponse(ctx context.Context, serviceId client.ServiceIdParam, params *client.ListDeploysParams, reqEditors ...client.RequestEditorFn) (*client.ListDeploysResponse, error)
	RetrieveDeployWithResponse(ctx context.Context, serviceId client.ServiceIdParam, deployId client.DeployIdParam, reqEditors ...client.RequestEditorFn) (*client.RetrieveDeployResponse, error)
	CreateDeployWithResponse(ctx context.Context, serviceId client.ServiceIdParam, body client.CreateDeployJSONRequestBody, reqEditors ...client.RequestEditorFn) (*client.CreateDeployResponse, error)
	RetrieveServiceWithResponse(ctx context.Context, id client.ServiceIdParam, reqEditors ...client.RequestEditorFn) (*client.RetrieveServiceResponse, error)
}

type Repo struct {
	client deployRepoClient
}

func NewRepo(c deployRepoClient) *Repo {
	return &Repo{
		client: c,
	}
}

func (r *Repo) ListDeploys(ctx context.Context, serviceId string, params *client.ListDeploysParams) ([]*client.Deploy, *client.Cursor, error) {
	resp, err := r.client.ListDeploysWithResponse(ctx, serviceId, params)
	if err != nil {
		return nil, nil, err
	}

	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, nil, err
	}
	if resp.JSON200 == nil || len(*resp.JSON200) == 0 {
		return nil, nil, nil
	}

	res := *resp.JSON200
	deploys := make([]*client.Deploy, 0, len(res))
	for _, deployWithCursor := range res {
		deploys = append(deploys, deployWithCursor.Deploy)
	}

	return deploys, res[len(res)-1].Cursor, nil
}

func (r *Repo) GetDeploy(ctx context.Context, serviceId string, deployId string) (*client.Deploy, error) {
	resp, err := r.client.RetrieveDeployWithResponse(ctx, client.ServiceIdParam(serviceId), client.DeployIdParam(deployId))
	if err != nil {
		return nil, err
	}

	return client.BodyFromResponse(resp.JSON200, resp)
}

// TriggerDeploy triggers a new deploy for a service. It returns (nil, nil)
// when the API accepts the deploy request without synchronously creating a
// deploy (a 202 response).
func (r *Repo) TriggerDeploy(ctx context.Context, serviceId string, clearCache bool) (*client.Deploy, error) {
	// Validate that the service belongs to the workspace in the current session
	// before deploying it.
	serviceResp, err := r.client.RetrieveServiceWithResponse(ctx, serviceId)
	if err != nil {
		return nil, err
	}
	service, err := client.BodyFromResponse(serviceResp.JSON200, serviceResp)
	if err != nil {
		return nil, err
	}
	if err := validate.WorkspaceMatches(ctx, service.OwnerId); err != nil {
		return nil, err
	}

	clearCacheVal := client.DoNotClear
	if clearCache {
		clearCacheVal = client.Clear
	}

	body := client.CreateDeployJSONRequestBody{
		ClearCache: &clearCacheVal,
	}

	resp, err := r.client.CreateDeployWithResponse(ctx, client.ServiceIdParam(serviceId), body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() == http.StatusAccepted {
		return nil, nil
	}

	return client.BodyFromResponse(resp.JSON201, resp)
}
