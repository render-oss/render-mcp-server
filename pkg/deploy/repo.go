package deploy

import (
	"context"

	"github.com/render-oss/cli/pkg/client"
)

type Repo struct {
	client *client.ClientWithResponses
}

func NewRepo(c *client.ClientWithResponses) *Repo {
	return &Repo{client: c}
}

func (d *Repo) ListDeploysForService(ctx context.Context, serviceID string, params *client.ListDeploysParams) ([]*client.Deploy, error) {
	resp, err := d.client.ListDeploysWithResponse(ctx, serviceID, params)
	if err != nil {
		return nil, err
	}
	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, err
	}

	result := make([]*client.Deploy, 0, len(*resp.JSON200))
	for _, deploy := range *resp.JSON200 {
		result = append(result, deploy.Deploy)
	}

	return result, nil
}

type TriggerDeployInput struct {
	ClearCache *bool
	CommitId   *string
	ImageUrl   *string
}

func (d *Repo) TriggerDeploy(ctx context.Context, serviceID string, input TriggerDeployInput) (*client.Deploy, error) {
	clearCache := client.DoNotClear
	if input.ClearCache != nil && *input.ClearCache {
		clearCache = client.Clear
	}

	resp, err := d.client.CreateDeployWithResponse(ctx, serviceID, client.CreateDeployJSONRequestBody{
		ClearCache: &clearCache,
		CommitId:   input.CommitId,
		ImageUrl:   input.ImageUrl,
	})
	if err != nil {
		return nil, err
	}

	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, err
	}

	return resp.JSON201, nil
}

func (d *Repo) GetDeploy(ctx context.Context, serviceID, deployID string) (*client.Deploy, error) {
	resp, err := d.client.RetrieveDeployWithResponse(ctx, serviceID, deployID)
	if err != nil {
		return nil, err
	}

	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, err
	}

	return resp.JSON200, nil
}

func (d *Repo) CancelDeploy(ctx context.Context, serviceID, deployID string) (*client.Deploy, error) {
	resp, err := d.client.CancelDeployWithResponse(ctx, serviceID, deployID)
	if err != nil {
		return nil, err
	}

	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, err
	}

	return resp.JSON200, nil
}
