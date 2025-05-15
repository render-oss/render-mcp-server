package keyvalue

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

func (r *Repo) ListKeyValue(ctx context.Context, params *client.ListKeyValueParams) ([]*client.KeyValue, error) {
	workspace, err := config.WorkspaceID()
	if err != nil {
		return nil, err
	}

	params.OwnerId = &client.OwnerIdParam{workspace}

	resp, err := r.client.ListKeyValueWithResponse(ctx, params)
	if err != nil {
		return nil, err
	}

	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, err
	}

	kvs := make([]*client.KeyValue, 0, len(*resp.JSON200))
	for _, kv := range *resp.JSON200 {
		kvs = append(kvs, &kv.KeyValue)
	}

	return kvs, nil
}

func (r *Repo) GetKeyValue(ctx context.Context, id string) (*client.KeyValueDetail, error) {
	resp, err := r.client.RetrieveKeyValueWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, err
	}

	return resp.JSON200, nil
}

func (r *Repo) GetKeyValueConnectionInfo(ctx context.Context, id string) (*client.KeyValueConnectionInfo, error) {
	resp, err := r.client.RetrieveKeyValueConnectionInfoWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, err
	}

	return resp.JSON200, nil
}
