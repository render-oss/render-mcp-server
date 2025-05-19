package keyvalue

import (
	"context"

	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/config"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

//go:generate go tool counterfeiter -o ../fakes/fakekeyvaluerepoclient_gen.go . keyValueRepoClient
type keyValueRepoClient interface {
	ListKeyValueWithResponse(ctx context.Context, params *client.ListKeyValueParams, reqEditors ...client.RequestEditorFn) (*client.ListKeyValueResponse, error)
	RetrieveKeyValueWithResponse(ctx context.Context, id string, reqEditors ...client.RequestEditorFn) (*client.RetrieveKeyValueResponse, error)
	RetrieveKeyValueConnectionInfoWithResponse(ctx context.Context, id string, reqEditors ...client.RequestEditorFn) (*client.RetrieveKeyValueConnectionInfoResponse, error)
	CreateKeyValueWithResponse(ctx context.Context, body client.KeyValuePOSTInput, reqEditors ...client.RequestEditorFn) (*client.CreateKeyValueResponse, error)
}

type Repo struct {
	client keyValueRepoClient
}

func NewRepo(c keyValueRepoClient) *Repo {
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

func (r *Repo) CreateKeyValue(ctx context.Context, input client.KeyValuePOSTInput) (*client.KeyValueDetail, error) {
	if err := validate.WorkspaceMatches(input.OwnerId); err != nil {
		return nil, err
	}

	resp, err := r.client.CreateKeyValueWithResponse(ctx, input)
	if err != nil {
		return nil, err
	}

	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, err
	}

	return resp.JSON201, nil
}
