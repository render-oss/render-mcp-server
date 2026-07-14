package deploy

import (
	"context"

	"github.com/render-oss/render-mcp-server/pkg/client"
)

//go:generate go tool counterfeiter -o ../fakes/fakedeployrepoclient_gen.go . deployRepoClient
type deployRepoClient interface {
	ListDeploysWithResponse(ctx context.Context, serviceId client.ServiceIdParam, params *client.ListDeploysParams, reqEditors ...client.RequestEditorFn) (*client.ListDeploysResponse, error)
	RetrieveDeployWithResponse(ctx context.Context, serviceId client.ServiceIdParam, deployId client.DeployIdParam, reqEditors ...client.RequestEditorFn) (*client.RetrieveDeployResponse, error)
	CreateDeployWithResponse(ctx context.Context, serviceId client.ServiceIdParam, body client.CreateDeployJSONRequestBody, reqEditors ...client.RequestEditorFn) (*client.CreateDeployResponse, error)
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

	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, err
	}

	return resp.JSON200, nil
}

func (r *Repo) TriggerDeploy(ctx context.Context, serviceId string, clearCache bool) (*client.Deploy, error) {
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
	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, err
	}

	return resp.JSON201, nil
}
