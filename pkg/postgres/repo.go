package postgres

import (
	"context"

	"github.com/render-oss/cli/pkg/client"
	"github.com/render-oss/cli/pkg/config"
)

type Repo struct {
	client *client.ClientWithResponses
}

func NewRepo(c *client.ClientWithResponses) *Repo {
	return &Repo{
		client: c,
	}
}

func (r *Repo) ListPostgres(ctx context.Context, params *client.ListPostgresParams) ([]*client.Postgres, error) {
	workspace, err := config.WorkspaceID()
	if err != nil {
		return nil, err
	}

	params.OwnerId = &client.OwnerIdParam{workspace}

	return client.ListAll(ctx, params, r.listPage)
}

func (r *Repo) listPage(ctx context.Context, params *client.ListPostgresParams) ([]*client.Postgres, *client.Cursor, error) {
	resp, err := r.client.ListPostgresWithResponse(ctx, params)
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
	pgs := make([]*client.Postgres, 0, len(res))
	for _, pg := range res {
		pgs = append(pgs, &pg.Postgres)
	}

	return pgs, &res[len(res)-1].Cursor, nil
}

func (r *Repo) GetPostgres(ctx context.Context, id string) (*client.PostgresDetail, error) {
	resp, err := r.client.RetrievePostgresWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, err
	}

	return resp.JSON200, nil
}

func (r *Repo) GetPostgresConnectionInfo(ctx context.Context, id string) (*client.PostgresConnectionInfo, error) {
	resp, err := r.client.RetrievePostgresConnectionInfoWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, err
	}

	return resp.JSON200, nil
}

func (r *Repo) RestartPostgresDatabase(ctx context.Context, id string) error {
	resp, err := r.client.RestartPostgres(ctx, id)
	if err != nil {
		return err
	}

	return client.ErrorFromResponse(resp)
}
