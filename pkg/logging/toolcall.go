package logging

import (
	"context"
	"log/slog"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	toolCallOutcomeSuccess   toolCallOutcome = "success"
	toolCallOutcomeCancelled toolCallOutcome = "cancelled"
	toolCallOutcomeError     toolCallOutcome = "error"
	toolCallOutcomeToolError toolCallOutcome = "tool_error"
)

type toolCallOutcome string

var toolCallLogLevel = slog.LevelDebug

// SetToolCallLogLevel configures completion-event severity.
// Call it during startup, before constructing servers or starting goroutines.
// It must not run concurrently with tool-call logging.
func SetToolCallLogLevel(level slog.Level) {
	toolCallLogLevel = level
}

// ToolCallMiddleware logs the duration and outcome of each tool handler invocation.
func ToolCallMiddleware(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (result *mcp.CallToolResult, err error) {
		start := time.Now()
		defer func() {
			cancelled := ctx.Err() != nil
			outcome := toolCallOutcomeSuccess
			switch {
			case cancelled:
				outcome = toolCallOutcomeCancelled
			case err != nil || result == nil:
				outcome = toolCallOutcomeError
			case result.IsError:
				outcome = toolCallOutcomeToolError
			}

			From(ctx).Log(toolCallLogLevel, "tool call completed",
				OneOffField("name", request.Params.Name),
				OneOffField("duration_ms", time.Since(start).Milliseconds()),
				OneOffField("outcome", outcome),
				OneOffField("cancelled", cancelled),
			)
		}()
		return next(ctx, request)
	}
}
