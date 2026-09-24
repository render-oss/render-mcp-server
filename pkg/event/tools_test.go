package event

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	eventsclient "github.com/render-oss/render-mcp-server/pkg/client/events"
	eventtypes "github.com/render-oss/render-mcp-server/pkg/client/eventtypes"
	"github.com/render-oss/render-mcp-server/pkg/fakes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testServiceID = "srv-123456"

func TestListEventsToolReturnsEventsWithDetails(t *testing.T) {
	fakeClient, repo := newTestRepo()

	// A server_failed event with an OOM kill. These failure details are why the
	// tool exists, so they have to survive the round trip.
	var details eventsclient.ServiceEventDetails
	require.NoError(t, details.FromServerFailedEvent(eventsclient.ServerFailedEvent{
		Reason: &eventsclient.FailureReason{
			OomKilled: &eventsclient.OomKilled{MemoryLimit: "512MB"},
		},
	}))

	fakeClient.ListEventsWithResponseReturns(&client.ListEventsResponse{
		JSON200: &[]client.ServiceEventWithCursor{
			{Event: eventsclient.ServiceEvent{
				Id:        "evt-abc",
				ServiceId: testServiceID,
				Type:      eventtypes.ServiceEventTypeServerFailed,
				Timestamp: time.Now(),
				Details:   details,
			}},
		},
		HTTPResponse: &http.Response{StatusCode: 200},
	}, nil)

	result := callListEvents(t, repo, map[string]any{"serviceId": testServiceID})

	require.False(t, result.IsError)
	text := textContent(t, result)
	assert.Contains(t, text, "evt-abc")
	assert.Contains(t, text, "server_failed")
	assert.Contains(t, text, "oomKilled")
	assert.Contains(t, text, "512MB")
}

func TestListEventsToolDefaults(t *testing.T) {
	fakeClient, repo := newTestRepo()
	fakeClient.ListEventsWithResponseReturns(emptyResponse(), nil)

	before := time.Now()
	result := callListEvents(t, repo, map[string]any{"serviceId": testServiceID})
	require.False(t, result.IsError)

	_, _, params, _ := fakeClient.ListEventsWithResponseArgsForCall(0)
	require.NotNil(t, params.StartTime)
	require.NotNil(t, params.Limit)

	assert.Equal(t, defaultLimit, *params.Limit)
	assert.WithinDuration(t, before.Add(-defaultLookback), *params.StartTime, time.Minute)
	assert.Nil(t, params.EndTime)
	assert.Nil(t, params.Type)
}

func TestListEventsToolExplicitParams(t *testing.T) {
	fakeClient, repo := newTestRepo()
	fakeClient.ListEventsWithResponseReturns(emptyResponse(), nil)

	result := callListEvents(t, repo, map[string]any{
		"serviceId": testServiceID,
		"startTime": "2026-01-01T00:00:00Z",
		"endTime":   "2026-01-02T00:00:00Z",
		"limit":     float64(50),
	})
	require.False(t, result.IsError)

	_, _, params, _ := fakeClient.ListEventsWithResponseArgsForCall(0)
	require.NotNil(t, params.StartTime)
	require.NotNil(t, params.EndTime)
	require.NotNil(t, params.Limit)

	assert.Equal(t, "2026-01-01T00:00:00Z", params.StartTime.Format(time.RFC3339))
	assert.Equal(t, "2026-01-02T00:00:00Z", params.EndTime.Format(time.RFC3339))
	assert.Equal(t, 50, *params.Limit)
}

// The schema declares one `type` value while the API accepts several separated
// by commas. Pinning the encoding means a change on either side shows up here
// instead of quietly dropping the filter.
func TestListEventsToolEventTypeEncoding(t *testing.T) {
	tests := []struct {
		name       string
		eventTypes []any
		wantType   []string
	}{
		{
			name:       "single type",
			eventTypes: []any{"deploy_ended"},
			wantType:   []string{"deploy_ended"},
		},
		{
			name:       "multiple types",
			eventTypes: []any{"deploy_ended", "server_failed", "build_ended"},
			wantType:   []string{"deploy_ended,server_failed,build_ended"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient, repo := newTestRepo()
			fakeClient.ListEventsWithResponseReturns(emptyResponse(), nil)

			result := callListEvents(t, repo, map[string]any{
				"serviceId":  testServiceID,
				"eventTypes": tt.eventTypes,
			})
			require.False(t, result.IsError)

			_, _, params, _ := fakeClient.ListEventsWithResponseArgsForCall(0)
			require.NotNil(t, params.Type)

			req, err := client.NewListEventsRequest("https://api.render.com/v1", testServiceID, params)
			require.NoError(t, err)
			assert.Equal(t, tt.wantType, req.URL.Query()["type"])
		})
	}
}

func TestListEventsToolRejectsNonServiceIDs(t *testing.T) {
	tests := []struct {
		name       string
		resourceID string
		wantText   string
	}{
		{name: "postgres", resourceID: "dpg-123456", wantText: "Postgres"},
		{name: "key value", resourceID: "red-123456", wantText: "Key Value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient, repo := newTestRepo()

			result := callListEvents(t, repo, map[string]any{"serviceId": tt.resourceID})

			require.True(t, result.IsError)
			assert.Contains(t, textContent(t, result), tt.wantText)
			// The request is rejected before any API call.
			assert.Equal(t, 0, fakeClient.ListEventsWithResponseCallCount())
		})
	}
}

func TestListEventsToolNoEvents(t *testing.T) {
	fakeClient, repo := newTestRepo()
	fakeClient.ListEventsWithResponseReturns(emptyResponse(), nil)

	result := callListEvents(t, repo, map[string]any{"serviceId": testServiceID})

	require.False(t, result.IsError)
	// An empty page still marshals as [] rather than null, and reports no cursor.
	assert.Equal(t, "[]\n\n cursor: \"\"", textContent(t, result))
}

// The cursor has to come from the last event in the page. Taking it from the
// first would make the next page repeat what this one already returned.
func TestListEventsToolReturnsCursorOfLastEvent(t *testing.T) {
	fakeClient, repo := newTestRepo()

	sharedTimestamp := time.Now()
	fakeClient.ListEventsWithResponseReturns(&client.ListEventsResponse{
		JSON200: &[]client.ServiceEventWithCursor{
			{Cursor: "cursor-newest", Event: eventsclient.ServiceEvent{Id: "evt-1", Timestamp: sharedTimestamp}},
			{Cursor: "cursor-oldest", Event: eventsclient.ServiceEvent{Id: "evt-2", Timestamp: sharedTimestamp}},
		},
		HTTPResponse: &http.Response{StatusCode: 200},
	}, nil)

	result := callListEvents(t, repo, map[string]any{"serviceId": testServiceID})

	require.False(t, result.IsError)
	text := textContent(t, result)
	assert.Contains(t, text, "evt-1")
	assert.Contains(t, text, "evt-2")
	// The cursor must come from the last (oldest) event, or the next page
	// would re-read what this one already returned.
	assert.True(t, strings.HasSuffix(text, "\n\n cursor: cursor-oldest"), text)
}

func TestListEventsToolPassesCursorThrough(t *testing.T) {
	fakeClient, repo := newTestRepo()
	fakeClient.ListEventsWithResponseReturns(emptyResponse(), nil)

	result := callListEvents(t, repo, map[string]any{
		"serviceId": testServiceID,
		"cursor":    "cursor-oldest",
	})
	require.False(t, result.IsError)

	_, _, params, _ := fakeClient.ListEventsWithResponseArgsForCall(0)
	require.NotNil(t, params.Cursor)
	assert.Equal(t, "cursor-oldest", *params.Cursor)
}

// A cursor page must not carry the default window. The API applies both, so
// sending it would stop paging at the lookback and hide older events.
func TestListEventsToolCursorSkipsDefaultWindow(t *testing.T) {
	fakeClient, repo := newTestRepo()
	fakeClient.ListEventsWithResponseReturns(emptyResponse(), nil)

	result := callListEvents(t, repo, map[string]any{
		"serviceId": testServiceID,
		"cursor":    "page-1-last",
	})
	require.False(t, result.IsError)

	_, _, params, _ := fakeClient.ListEventsWithResponseArgsForCall(0)
	require.NotNil(t, params.Cursor)
	assert.Nil(t, params.StartTime)
}

// An explicit startTime still applies alongside a cursor.
func TestListEventsToolCursorKeepsExplicitStartTime(t *testing.T) {
	fakeClient, repo := newTestRepo()
	fakeClient.ListEventsWithResponseReturns(emptyResponse(), nil)

	result := callListEvents(t, repo, map[string]any{
		"serviceId": testServiceID,
		"cursor":    "page-1-last",
		"startTime": "2026-01-01T00:00:00Z",
	})
	require.False(t, result.IsError)

	_, _, params, _ := fakeClient.ListEventsWithResponseArgsForCall(0)
	require.NotNil(t, params.StartTime)
	assert.Equal(t, "2026-01-01T00:00:00Z", params.StartTime.Format(time.RFC3339))
}

// The default empty-string cursor means "first page" and must not be sent.
func TestListEventsToolOmitsEmptyCursor(t *testing.T) {
	fakeClient, repo := newTestRepo()
	fakeClient.ListEventsWithResponseReturns(emptyResponse(), nil)

	result := callListEvents(t, repo, map[string]any{
		"serviceId": testServiceID,
		"cursor":    "",
	})
	require.False(t, result.IsError)

	_, _, params, _ := fakeClient.ListEventsWithResponseArgsForCall(0)
	assert.Nil(t, params.Cursor)
}

// Walking pages has to end. The second call sends the first page's cursor and
// gets back an empty page with no cursor.
func TestListEventsToolPaginationTerminates(t *testing.T) {
	fakeClient, repo := newTestRepo()

	fakeClient.ListEventsWithResponseReturnsOnCall(0, &client.ListEventsResponse{
		JSON200: &[]client.ServiceEventWithCursor{
			{Cursor: "page-1-last", Event: eventsclient.ServiceEvent{Id: "evt-1"}},
		},
		HTTPResponse: &http.Response{StatusCode: 200},
	}, nil)
	fakeClient.ListEventsWithResponseReturnsOnCall(1, emptyResponse(), nil)

	first := callListEvents(t, repo, map[string]any{"serviceId": testServiceID, "limit": float64(1)})
	require.False(t, first.IsError)
	require.True(t, strings.HasSuffix(textContent(t, first), "\n\n cursor: page-1-last"))

	second := callListEvents(t, repo, map[string]any{
		"serviceId": testServiceID,
		"limit":     float64(1),
		"cursor":    "page-1-last",
	})
	require.False(t, second.IsError)
	assert.Equal(t, "[]\n\n cursor: \"\"", textContent(t, second))

	_, _, secondParams, _ := fakeClient.ListEventsWithResponseArgsForCall(1)
	require.NotNil(t, secondParams.Cursor)
	assert.Equal(t, "page-1-last", *secondParams.Cursor)
}

func TestListEventsToolAPIError(t *testing.T) {
	fakeClient, repo := newTestRepo()

	fakeClient.ListEventsWithResponseReturns(&client.ListEventsResponse{
		HTTPResponse: &http.Response{StatusCode: 404},
		Body:         []byte(`{"message":"service not found"}`),
	}, nil)

	result := callListEvents(t, repo, map[string]any{"serviceId": testServiceID})

	require.True(t, result.IsError)
	assert.Contains(t, textContent(t, result), "service not found")
}

func TestListEventsToolTransportError(t *testing.T) {
	fakeClient, repo := newTestRepo()
	fakeClient.ListEventsWithResponseReturns(nil, errors.New("connection refused"))

	result := callListEvents(t, repo, map[string]any{"serviceId": testServiceID})

	require.True(t, result.IsError)
	assert.Contains(t, textContent(t, result), "connection refused")
}

func TestListEventsToolInvalidTimestamp(t *testing.T) {
	fakeClient, repo := newTestRepo()

	result := callListEvents(t, repo, map[string]any{
		"serviceId": testServiceID,
		"startTime": "yesterday",
	})

	require.True(t, result.IsError)
	assert.Contains(t, textContent(t, result), "startTime")
	assert.Equal(t, 0, fakeClient.ListEventsWithResponseCallCount())
}

// serviceEventTypes comes from the generated ServiceEventTypeValues, so the
// count doubles as a drift check. Regenerating the client after the API adds or
// removes a filterable event type fails here, which is the cue to update the
// README's list.
func TestServiceEventTypesEnum(t *testing.T) {
	types := serviceEventTypes()

	assert.Len(t, types, 43, "filterable event types changed; update the eventTypes list in README.md")
	assert.Contains(t, types, "server_failed")
	assert.Contains(t, types, "deploy_ended")
	// Not every event type is filterable, so the enum must stay narrower than
	// the set of events that can come back.
	assert.NotContains(t, types, "edge_cache_purged")
}

func callListEvents(t *testing.T, repo *Repo, args map[string]any) *mcp.CallToolResult {
	t.Helper()

	request := mcp.CallToolRequest{}
	request.Params.Arguments = args

	result, err := listEvents(repo).Handler(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

func newTestRepo() (*fakes.FakeEventRepoClient, *Repo) {
	fakeClient := &fakes.FakeEventRepoClient{}
	return fakeClient, NewRepo(fakeClient)
}

func emptyResponse() *client.ListEventsResponse {
	return &client.ListEventsResponse{
		JSON200:      &[]client.ServiceEventWithCursor{},
		HTTPResponse: &http.Response{StatusCode: 200},
	}
}

func textContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, result.Content)
	content, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	return content.Text
}
