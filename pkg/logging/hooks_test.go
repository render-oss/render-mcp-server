package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

func TestHooksLogToolErrorResults(t *testing.T) {
	t.Setenv("LOGGING", "1")
	var output bytes.Buffer
	previousWriter := logger.Writer()
	logger.SetOutput(&output)
	t.Cleanup(func() { logger.SetOutput(previousWriter) })

	hooks := new(server.Hooks)
	AddHooks(hooks)
	s := server.NewMCPServer("test", "1", server.WithHooks(hooks))
	s.AddTool(mcp.NewTool("test_tool"), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultError("tool failed"), nil
	})
	body, err := json.Marshal(mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(1),
		Method:  string(mcp.MethodToolsCall),
		Params:  mcp.CallToolParams{Name: "test_tool"},
	})
	require.NoError(t, err)

	s.HandleMessage(t.Context(), body)
	require.Contains(t, output.String(), "ERROR tool call failed name=test_tool error=tool failed")
	require.NotContains(t, output.String(), "tool call ok")
}
