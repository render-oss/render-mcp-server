package logging

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolCallMiddlewareOutcomes(t *testing.T) {
	previousLevel := toolCallLogLevel
	SetToolCallLogLevel(slog.LevelInfo)
	t.Cleanup(func() { SetToolCallLogLevel(previousLevel) })

	tests := []struct {
		name      string
		result    *mcp.CallToolResult
		err       error
		cancelled bool
		outcome   string
	}{
		{
			name:    "success",
			result:  mcp.NewToolResultText("done"),
			outcome: "success",
		},
		{
			name:    "tool error",
			result:  mcp.NewToolResultError("invalid input"),
			outcome: "tool_error",
		},
		{
			name:    "Go error",
			err:     assert.AnError,
			outcome: "error",
		},
		{
			name:    "handler deadline error with active context",
			err:     context.DeadlineExceeded,
			outcome: "error",
		},
		{
			name:      "cancelled during handler",
			err:       context.Canceled,
			cancelled: true,
			outcome:   "cancelled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			ctx := ContextWithLogger(t.Context(), New(&output, slog.LevelInfo))
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			calls := 0
			handler := ToolCallMiddleware(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				calls++
				if tt.cancelled {
					cancel()
				}
				return tt.result, tt.err
			})

			result, err := handler(ctx, mcp.CallToolRequest{})
			require.Equal(t, 1, calls)
			require.Equal(t, tt.result, result, "middleware should preserve the handler's result")
			require.ErrorIs(t, err, tt.err, "middleware should preserve the handler's error")
			records := readLogRecords(t, &output)
			require.Len(t, records, 1, "each handler invocation should emit exactly one completion record")
			require.Equal(t, "INFO", records[0]["level"])
			require.Equal(t, tt.outcome, records[0]["outcome"])
			require.Equal(t, tt.cancelled, records[0]["cancelled"])
		})
	}
}

func TestToolCallMiddlewareExpiredDeadline(t *testing.T) {
	previousLevel := toolCallLogLevel
	SetToolCallLogLevel(slog.LevelInfo)
	t.Cleanup(func() { SetToolCallLogLevel(previousLevel) })

	var output bytes.Buffer
	ctx := ContextWithLogger(t.Context(), New(&output, slog.LevelInfo))
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancel()
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	handler := ToolCallMiddleware(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return nil, context.DeadlineExceeded
	})

	_, err := handler(ctx, mcp.CallToolRequest{})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	records := readLogRecords(t, &output)
	require.Len(t, records, 1)
	require.Equal(t, "cancelled", records[0]["outcome"])
	require.Equal(t, true, records[0]["cancelled"])
}

func TestToolCallMiddlewareCompletion(t *testing.T) {
	previousLevel := toolCallLogLevel
	SetToolCallLogLevel(slog.LevelInfo)
	t.Cleanup(func() { SetToolCallLogLevel(previousLevel) })

	synctest.Test(t, func(t *testing.T) {
		var output bytes.Buffer
		ctx := ContextWithLogger(t.Context(), New(&output, slog.LevelInfo))
		ctx = ContextWithFields(ctx, OneOffField("request_id", "request-123"))
		request := mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "test_tool"}}
		handler := ToolCallMiddleware(func(gotCtx context.Context, gotRequest mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			require.Equal(t, ctx, gotCtx)
			require.Equal(t, request, gotRequest)
			require.Empty(t, output.String(), "completion should not be logged before the handler returns")
			synctest.Sleep(125 * time.Millisecond)
			return mcp.NewToolResultText("done"), nil
		})

		_, err := handler(ctx, request)
		require.NoError(t, err)
		records := readLogRecords(t, &output)
		require.Len(t, records, 1, "the completed handler should emit exactly one completion record")
		require.Equal(t, "INFO", records[0]["level"])
		require.Equal(t, "tool call completed", records[0]["msg"])
		require.Equal(t, "test_tool", records[0]["name"])
		require.Equal(t, float64(125), records[0]["duration_ms"], "duration should include time spent inside the handler")
		require.Equal(t, "request-123", records[0]["request_id"], "completion should inherit correlation fields from the context logger")
	})
}

func TestToolCallMiddlewareLogLevel(t *testing.T) {
	tests := []struct {
		name        string
		setDebug    bool
		loggerLevel slog.Level
		wantRecords int
	}{
		{name: "default at INFO", setDebug: false, loggerLevel: slog.LevelInfo, wantRecords: 0},
		{name: "default at DEBUG", setDebug: false, loggerLevel: slog.LevelDebug, wantRecords: 1},
		{name: "explicit DEBUG at INFO", setDebug: true, loggerLevel: slog.LevelInfo, wantRecords: 0},
		{name: "explicit DEBUG at DEBUG", setDebug: true, loggerLevel: slog.LevelDebug, wantRecords: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousLevel := toolCallLogLevel
			t.Cleanup(func() { SetToolCallLogLevel(previousLevel) })
			if tt.setDebug {
				SetToolCallLogLevel(slog.LevelDebug)
			}
			var output bytes.Buffer
			ctx := ContextWithLogger(t.Context(), New(&output, tt.loggerLevel))
			handler := ToolCallMiddleware(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return mcp.NewToolResultText("done"), nil
			})

			_, err := handler(ctx, mcp.CallToolRequest{})

			require.NoError(t, err)
			records := readLogRecords(t, &output)
			require.Len(t, records, tt.wantRecords)
			for _, record := range records {
				require.Equal(t, "DEBUG", record["level"])
			}
		})
	}
}
