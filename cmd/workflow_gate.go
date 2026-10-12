package cmd

import (
	"log"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/server"
)

const workflowWritesEnabledEnv = "WORKFLOW_WRITES_ENABLED"

// gateWorkflowWrites temporarily limits Workflows to read tools unless writes
// are enabled. It runs once when the server registers tools, before serving.
// To enable writes permanently, remove this file and its call in server.go.
// Also remove workflow_gate_test.go and the rollout instructions in the docs.
func gateWorkflowWrites(tools []server.ServerTool) []server.ServerTool {
	var enabled bool
	switch strings.ToLower(strings.TrimSpace(os.Getenv(workflowWritesEnabledEnv))) {
	case "", "false", "0", "no":
	case "true", "1", "yes":
		enabled = true
	default:
		log.Fatalf("Workflows configuration: %s must be true/false, 1/0, or yes/no", workflowWritesEnabledEnv)
	}
	log.Printf("Workflows writes enabled=%t", enabled)
	if enabled {
		return tools
	}

	reads := make([]server.ServerTool, 0, len(tools))
	for _, tool := range tools {
		// Only explicitly read-only tools are available while writes are off.
		if hint := tool.Tool.Annotations.ReadOnlyHint; hint != nil && *hint {
			reads = append(reads, tool)
		}
	}
	return reads
}
