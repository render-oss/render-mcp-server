package logs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	logsclient "github.com/render-oss/render-mcp-server/pkg/client/logs"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func toolRequest(args map[string]any) mcp.CallToolRequest {
	request := mcp.CallToolRequest{}
	request.Params.Arguments = args
	return request
}

func TestParseLogsLimit(t *testing.T) {
	limit, err := parseLogsLimit(toolRequest(nil))
	require.NoError(t, err)
	assert.Nil(t, limit)

	for _, v := range []float64{1, 50, 100} {
		limit, err := parseLogsLimit(toolRequest(map[string]any{"limit": v}))
		require.NoError(t, err, "limit %v", v)
		require.NotNil(t, limit)
		assert.Equal(t, int(v), *limit)
	}

	for _, v := range []float64{0, -5, 101, 1000, 2.5} {
		_, err := parseLogsLimit(toolRequest(map[string]any{"limit": v}))
		require.Error(t, err, "limit %v", v)
		assert.Contains(t, err.Error(), "invalid limit")
	}
}

func TestParseLogDirection(t *testing.T) {
	direction, err := parseLogDirection(toolRequest(nil))
	require.NoError(t, err)
	assert.Nil(t, direction)

	for _, v := range []string{"backward", "forward"} {
		direction, err := parseLogDirection(toolRequest(map[string]any{"direction": v}))
		require.NoError(t, err, "direction %q", v)
		require.NotNil(t, direction)
		assert.Equal(t, logsclient.LogDirection(v), *direction)
	}

	for _, v := range []string{"sideways", "", "BACKWARD"} {
		_, err := parseLogDirection(toolRequest(map[string]any{"direction": v}))
		require.Error(t, err, "direction %q", v)
		assert.Contains(t, err.Error(), "invalid direction")
	}
}

func TestListLogsToolRejectsInvalidParamsBeforeCallingAPI(t *testing.T) {
	t.Setenv("RENDER_CONFIG_PATH", filepath.Join(t.TempDir(), "mcp-server.yaml"))
	ctx := session.ContextWithStdioSession(context.Background())
	session.FromContext(ctx).SetWorkspace(ctx, "own-123")

	var requests atomic.Int32
	var lastQuery atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		lastQuery.Store(r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"logs":[],"hasMore":false}`))
	}))
	defer srv.Close()

	c, err := client.NewClientWithResponses(srv.URL)
	require.NoError(t, err)

	var handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
	for _, tool := range Tools(c) {
		if tool.Tool.Name == "list_logs" {
			handler = tool.Handler
		}
	}
	require.NotNil(t, handler)

	call := func(args map[string]any) *mcp.CallToolResult {
		result, err := handler(ctx, toolRequest(args))
		require.NoError(t, err)
		return result
	}

	for _, args := range []map[string]any{
		{"resource": []any{"srv-1"}, "limit": 101.0},
		{"resource": []any{"srv-1"}, "limit": 0.0},
		{"resource": []any{"srv-1"}, "direction": "sideways"},
	} {
		assert.True(t, call(args).IsError, "args %v", args)
	}
	assert.Zero(t, requests.Load(), "invalid params must not reach the API")

	result := call(map[string]any{"resource": []any{"srv-1"}, "limit": 50.0, "direction": "forward"})
	assert.False(t, result.IsError)
	assert.EqualValues(t, 1, requests.Load())
	query := lastQuery.Load().(url.Values)
	assert.Equal(t, []string{"50"}, query["limit"])
	assert.Equal(t, []string{"forward"}, query["direction"])
}
