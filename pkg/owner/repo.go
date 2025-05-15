package owner

import (
	"context"
	"fmt"

	"github.com/render-oss/cli/pkg/client"
	"github.com/render-oss/cli/pkg/pointers"
)

type ListInput struct {
	Name string
}

type Repo struct {
	client *client.ClientWithResponses
}

func NewRepo(client *client.ClientWithResponses) *Repo {
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

	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, err
	}

	var owners []*client.Owner
	for _, ownerWithCursor := range *resp.JSON200 {
		owners = append(owners, ownerWithCursor.Owner)
	}

	return owners, nil
}

func (r *Repo) RetrieveOwner(ctx context.Context, id string) (*client.Owner, error) {
	resp, err := r.client.RetrieveOwnerWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}

	if resp.JSON200 == nil {
		return nil, fmt.Errorf("unexpected response: %v", resp.Status())
	}

	return resp.JSON200, nil
}
