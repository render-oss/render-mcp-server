package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestHooksLogSDKErrors(t *testing.T) {
	tests := []struct {
		name       string
		toolName   string
		handlerErr error
		wantCode   int
		wantLevel  string
		wantError  string
	}{
		{
			name:       "request rejection is debug",
			toolName:   "missing_tool",
			handlerErr: nil,
			wantCode:   mcp.INVALID_PARAMS,
			wantLevel:  "DEBUG",
			wantError:  "tool 'missing_tool' not found",
		},
		{
			name:       "internal failure is error",
			toolName:   "test_tool",
			handlerErr: errors.New("handler failed"),
			wantCode:   mcp.INTERNAL_ERROR,
			wantLevel:  "ERROR",
			wantError:  "handler failed",
		},
		{
			name:       "wrapped cancellation is debug",
			toolName:   "test_tool",
			handlerErr: fmt.Errorf("handler canceled: %w", context.Canceled),
			wantCode:   mcp.INTERNAL_ERROR,
			wantLevel:  "DEBUG",
			wantError:  "handler canceled: context canceled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			ctx := ContextWithLogger(t.Context(), New(&output, slog.LevelDebug))
			hooks := new(server.Hooks)
			AddHooks(hooks)
			s := server.NewMCPServer("test", "1", server.WithHooks(hooks))
			s.AddTool(mcp.NewTool("test_tool"), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return nil, tt.handlerErr
			})
			body, err := json.Marshal(mcp.JSONRPCRequest{
				JSONRPC: mcp.JSONRPC_VERSION,
				ID:      mcp.NewRequestId(1),
				Method:  string(mcp.MethodToolsCall),
				Params:  mcp.CallToolParams{Name: tt.toolName},
			})
			require.NoError(t, err)

			response, ok := s.HandleMessage(ctx, body).(mcp.JSONRPCError)
			require.True(t, ok)
			require.Equal(t, tt.wantCode, response.Error.Code)
			records := readLogRecords(t, &output)
			require.Len(t, records, 2)
			require.Equal(t, "mcp error", records[1]["msg"])
			require.Equal(t, tt.wantLevel, records[1]["level"])
			require.Equal(t, string(mcp.MethodToolsCall), records[1]["method"])
			require.Contains(t, records[1]["error"], tt.wantError)
		})
	}
}

func TestHooksLogUnsupportedResources(t *testing.T) {
	for _, method := range []mcp.MCPMethod{mcp.MethodResourcesList, mcp.MethodResourcesTemplatesList} {
		t.Run(string(method), func(t *testing.T) {
			var output bytes.Buffer
			ctx := ContextWithLogger(t.Context(), New(&output, slog.LevelDebug))
			hooks := new(server.Hooks)
			AddHooks(hooks)
			s := server.NewMCPServer("test", "1", server.WithHooks(hooks))
			body, err := json.Marshal(mcp.JSONRPCRequest{
				JSONRPC: mcp.JSONRPC_VERSION,
				ID:      mcp.NewRequestId(1),
				Method:  string(method),
			})
			require.NoError(t, err)

			response, ok := s.HandleMessage(ctx, body).(mcp.JSONRPCError)
			require.True(t, ok)
			require.Equal(t, mcp.METHOD_NOT_FOUND, response.Error.Code)
			records := readLogRecords(t, &output)
			require.Len(t, records, 1)
			require.Equal(t, "DEBUG", records[0]["level"])
			require.Equal(t, "mcp error", records[0]["msg"])
			require.Equal(t, string(method), records[0]["method"])
			require.Equal(t, "request error: resources not supported", records[0]["error"])
		})
	}
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
