package deploy

import (
	"context"

	"github.com/render-oss/render-mcp-server/pkg/client"
)

type Repo struct {
	client *client.ClientWithResponses
}

func NewRepo(c *client.ClientWithResponses) *Repo {
	return &Repo{
		client: c,
	}
}

type ListDeploysParams struct {
	*client.ListDeploysParams
	serviceId string
}

func (r *Repo) ListDeploys(ctx context.Context, serviceId string, params *client.ListDeploysParams) ([]*client.Deploy, error) {
	listDeploysParams := &ListDeploysParams{
		ListDeploysParams: params,
		serviceId:         serviceId,
	}
	return client.ListAll(ctx, listDeploysParams, r.listDeploysPage)
}

func (r *Repo) listDeploysPage(ctx context.Context, params *ListDeploysParams) ([]*client.Deploy, *client.Cursor, error) {
	resp, err := r.client.ListDeploysWithResponse(ctx, params.serviceId, params.ListDeploysParams)
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
