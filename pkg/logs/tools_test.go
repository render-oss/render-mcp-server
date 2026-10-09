package logs

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	logsclient "github.com/render-oss/render-mcp-server/pkg/client/logs"
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
