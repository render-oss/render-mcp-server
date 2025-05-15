package environment

import (
	"context"
	"fmt"

	"github.com/render-oss/cli/pkg/client"
)

type Repo struct {
	client *client.ClientWithResponses
}

func NewRepo(client *client.ClientWithResponses) *Repo {
	return &Repo{client: client}
}

// GetEnvironment retrieves an environment by ID.
// Note: We are not checking the workspace here because we currently only call this is from contexts
// where we've pulled the environment ID from a resource that was already checked. If this changes, we should
// fetch the project and check its workspace. For now, we will avoid the extra network call.
func (e *Repo) GetEnvironment(ctx context.Context, id string) (*client.Environment, error) {
	resp, err := e.client.RetrieveEnvironmentWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}

	if resp.JSON200 == nil {
		return nil, fmt.Errorf("unexpected response: %v", resp.Status())
	}

	return resp.JSON200, nil
}

func (e *Repo) ListEnvironments(ctx context.Context, params *client.ListEnvironmentsParams) ([]*client.Environment, error) {
	return client.ListAll(ctx, params, e.listPage)
}

func (e *Repo) listPage(ctx context.Context, params *client.ListEnvironmentsParams) ([]*client.Environment, *client.Cursor, error) {
	resp, err := e.client.ListEnvironmentsWithResponse(ctx, params)
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
	envs := make([]*client.Environment, 0, len(*resp.JSON200))
	for _, projectWithCursor := range *resp.JSON200 {
		envs = append(envs, &projectWithCursor.Environment)
	}

	return envs, &res[len(res)-1].Cursor, nil
}
