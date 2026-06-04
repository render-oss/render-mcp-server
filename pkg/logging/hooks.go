package logging

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func NewHooks() *server.Hooks {
	if !enabled() {
		return nil
	}

	hooks := &server.Hooks{}

	hooks.AddBeforeCallTool(func(_ context.Context, _ any, message *mcp.CallToolRequest) {
		Info("tool call start name=%s", message.Params.Name)
	})

	hooks.AddAfterCallTool(func(_ context.Context, _ any, message *mcp.CallToolRequest, result *mcp.CallToolResult) {
		if result != nil && result.IsError {
			Error("tool call failed name=%s error=%s", message.Params.Name, toolResultText(result))
			return
		}
		Info("tool call ok name=%s", message.Params.Name)
	})

	hooks.AddOnError(func(_ context.Context, _ any, method mcp.MCPMethod, _ any, err error) {
		Error("mcp error method=%s err=%v", method, err)
	})

	return hooks
}

func toolResultText(result *mcp.CallToolResult) string {
	for _, content := range result.Content {
		if text, ok := mcp.AsTextContent(content); ok && text.Text != "" {
			return text.Text
		}
	}
	return "unknown tool error"
}
