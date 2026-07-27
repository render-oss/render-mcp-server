package owner

import (
	"context"

	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
)

type ListInput struct {
	Name string
}

type ownerRepoClient interface {
	ListOwnersWithResponse(
		ctx context.Context,
		params *client.ListOwnersParams,
		reqEditors ...client.RequestEditorFn,
	) (*client.ListOwnersResponse, error)
	RetrieveOwnerWithResponse(
		ctx context.Context,
		id string,
		reqEditors ...client.RequestEditorFn,
	) (*client.RetrieveOwnerResponse, error)
}

type Repo struct {
	client ownerRepoClient
}

func NewRepo(client ownerRepoClient) *Repo {
	return &Repo{client: client}
}

func (r *Repo) ListOwners(ctx context.Context, input ListInput) ([]*client.Owner, error) {
	listParams := &client.ListOwnersParams{Limit: pointers.From(100)}
	if input.Name != "" {
		listParams.Name = pointers.From([]string{input.Name})
	}

	resp, err := r.client.ListOwnersWithResponse(ctx, listParams)
	if err != nil {
		return nil, err
	}

	res, err := client.BodyFromResponse(resp.JSON200, resp)
	if err != nil {
		return nil, err
	}

	var owners []*client.Owner
	for _, ownerWithCursor := range *res {
		owners = append(owners, ownerWithCursor.Owner)
	}

	return owners, nil
}

func (r *Repo) RetrieveOwner(ctx context.Context, id string) (*client.Owner, error) {
	resp, err := r.client.RetrieveOwnerWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}

	return client.BodyFromResponse(resp.JSON200, resp)
}
