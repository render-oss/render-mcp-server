package event

import (
	"context"
	"fmt"
	"strings"

	"github.com/render-oss/render-mcp-server/pkg/client"
	eventsclient "github.com/render-oss/render-mcp-server/pkg/client/events"
)

const (
	postgresIDPrefix = "dpg-"
	keyValueIDPrefix = "red-"
)

//go:generate go tool counterfeiter -o ../fakes/fakeeventrepoclient_gen.go . eventRepoClient
type eventRepoClient interface {
	ListEventsWithResponse(ctx context.Context, serviceId client.ServiceIdParam, params *client.ListEventsParams, reqEditors ...client.RequestEditorFn) (*client.ListEventsResponse, error)
}

type Repo struct {
	client eventRepoClient
}

func NewRepo(c eventRepoClient) *Repo {
	return &Repo{
		client: c,
	}
}

// ListEvents returns a service's events, newest first, plus the cursor of the
// last one for paging. It rejects Postgres and Key Value IDs up front, since
// the events route only resolves service IDs and would answer with a bare 404.
func (r *Repo) ListEvents(ctx context.Context, serviceId string, params *client.ListEventsParams) ([]eventsclient.ServiceEvent, *client.Cursor, error) {
	if err := rejectNonServiceID(serviceId); err != nil {
		return nil, nil, err
	}

	resp, err := r.client.ListEventsWithResponse(ctx, serviceId, params)
	if err != nil {
		return nil, nil, err
	}

	body, err := client.BodyFromResponse(resp.JSON200, resp)
	if err != nil {
		return nil, nil, err
	}

	res := *body
	// events stays non-nil so an empty page marshals as [] rather than null.
	events := make([]eventsclient.ServiceEvent, 0, len(res))
	for _, eventWithCursor := range res {
		events = append(events, eventWithCursor.Event)
	}

	// An empty page has no cursor to continue from.
	var cursor *client.Cursor
	if len(res) > 0 {
		cursor = &res[len(res)-1].Cursor
	}

	return events, cursor, nil
}

func rejectNonServiceID(serviceId string) error {
	switch {
	case strings.HasPrefix(serviceId, postgresIDPrefix):
		return fmt.Errorf("%s is a Postgres database; events are only available for services. "+
			"Use get_postgres for its current status", serviceId)
	case strings.HasPrefix(serviceId, keyValueIDPrefix):
		return fmt.Errorf("%s is a Key Value instance; events are only available for services. "+
			"Use get_key_value for its current status", serviceId)
	}
	return nil
}
