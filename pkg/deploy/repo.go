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
