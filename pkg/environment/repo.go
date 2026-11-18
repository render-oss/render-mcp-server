package environment

import (
	"context"

	"github.com/render-oss/render-mcp-server/pkg/client"
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

	return client.BodyFromResponse(ctx, resp.JSON200, resp)
}

func (e *Repo) ListEnvironments(ctx context.Context, params *client.ListEnvironmentsParams) ([]*client.Environment, error) {
	return client.ListAll(ctx, params, e.listPage)
}

func (e *Repo) listPage(ctx context.Context, params *client.ListEnvironmentsParams) ([]*client.Environment, *client.Cursor, error) {
	resp, err := e.client.ListEnvironmentsWithResponse(ctx, params)
	if err != nil {
		return nil, nil, err
	}

	body, err := client.BodyFromResponse(ctx, resp.JSON200, resp)
	if err != nil {
		return nil, nil, err
	}

	res := *body
	envs := make([]*client.Environment, 0, len(res))
	for _, projectWithCursor := range res {
		envs = append(envs, &projectWithCursor.Environment)
	}

	var cursor *client.Cursor
	if len(res) > 0 {
		cursor = &res[len(res)-1].Cursor
	}

	return envs, cursor, nil
}
