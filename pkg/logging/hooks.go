package logging

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func AddHooks(hooks *server.Hooks) {
	hooks.AddBeforeCallTool(func(ctx context.Context, _ any, message *mcp.CallToolRequest) {
		From(ctx).Debug("tool call start", OneOffField("name", message.Params.Name))
	})

	hooks.AddAfterCallTool(func(ctx context.Context, _ any, message *mcp.CallToolRequest, result any) {
		// The SDK hook receives CallToolResult for synchronous calls or CreateTaskResult
		// for task creation. Our tools currently run synchronously, so inspect their
		// results for errors. Revisit logging if we add tasks: task creation succeeding
		// does not mean the underlying operation succeeded.
		if result, ok := result.(*mcp.CallToolResult); ok && result != nil && result.IsError {
			From(ctx).Debug("tool call failed",
				OneOffField("name", message.Params.Name),
				OneOffField(errorField, toolResultText(result)),
			)
			return
		}
		From(ctx).Debug("tool call ok", OneOffField("name", message.Params.Name))
	})

	hooks.AddOnError(func(ctx context.Context, _ any, method mcp.MCPMethod, _ any, err error) {
		From(ctx).WithError(err).Error("mcp error", OneOffField("method", method))
	})
}

func toolResultText(result *mcp.CallToolResult) string {
	for _, content := range result.Content {
		if text, ok := mcp.AsTextContent(content); ok && text.Text != "" {
			return text.Text
		}
	}
	return "unknown tool error"
}
