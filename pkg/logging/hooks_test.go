package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

func TestHooksLogToolErrorResults(t *testing.T) {
	var output bytes.Buffer
	ctx := ContextWithLogger(t.Context(), New(&output, slog.LevelDebug))

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

	s.HandleMessage(ctx, body)
	records := readLogRecords(t, &output)
	require.Len(t, records, 2)
	require.Equal(t, "tool call failed", records[1]["msg"])
	require.Equal(t, "DEBUG", records[1]["level"])
	require.Equal(t, "test_tool", records[1]["name"])
	require.Equal(t, "tool failed", records[1]["error"])
	require.NotContains(t, output.String(), "tool call ok")
}

func readLogRecords(t *testing.T, output *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	for decoder.More() {
		var record map[string]any
		require.NoError(t, decoder.Decode(&record))
		records = append(records, record)
	}
	return records
}
