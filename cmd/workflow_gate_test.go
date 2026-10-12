package cmd

import (
	"os"
	"os/exec"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/workflow"
	"github.com/stretchr/testify/require"
)

func TestWorkflowWriteGate(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		enabled     bool
	}{
		{"unset", "", false}, {"empty", "", false},
		{"false", "false", false}, {"zero", "0", false}, {"no", " NO ", false},
		{"true", "true", true}, {"one", "1", true}, {"yes", " YES ", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(workflowWritesEnabledEnv, tc.value)
			if tc.name == "unset" {
				require.NoError(t, os.Unsetenv(workflowWritesEnabledEnv))
			}
			all := workflow.Tools(nil)
			original := make([]mcp.Tool, len(all))
			for i, tool := range all {
				original[i] = tool.Tool
			}
			gated := gateWorkflowWrites(all)
			var want []mcp.Tool
			for _, tool := range original {
				if tc.enabled || (tool.Name != "create_workflow" && tool.Name != "create_workflow_version") {
					want = append(want, tool)
				}
			}
			var got []mcp.Tool
			for _, tool := range gated {
				got = append(got, tool.Tool)
			}
			require.Equal(t, want, got)
			for i, tool := range all {
				require.Equal(t, original[i], tool.Tool, "gate changed the original tool catalog")
			}
		})
	}
}

func TestWorkflowWriteGateRequiresExplicitReadOnlyAnnotation(t *testing.T) {
	t.Setenv(workflowWritesEnabledEnv, "false")
	unknown := server.ServerTool{Tool: mcp.Tool{Name: "future_tool"}}
	require.Empty(t, gateWorkflowWrites([]server.ServerTool{unknown}))
}

func TestWorkflowWriteGatePreservesOtherTools(t *testing.T) {
	t.Setenv(workflowWritesEnabledEnv, "true")
	all := buildWorkspaceScopedTools(nil)
	t.Setenv(workflowWritesEnabledEnv, "false")
	gated := buildWorkspaceScopedTools(nil)
	var want, got []mcp.Tool
	for _, tool := range all {
		if tool.Tool.Name != "create_workflow" && tool.Tool.Name != "create_workflow_version" {
			want = append(want, tool.Tool)
		}
	}
	for _, tool := range gated {
		got = append(got, tool.Tool)
		require.Contains(t, tool.Tool.InputSchema.Properties, "workspaceId")
	}
	require.Equal(t, want, got)
}

func TestWorkflowWriteGateRejectsInvalidConfig(t *testing.T) {
	// Exercise the real server constructor in a child process because bad
	// startup configuration must stop the process before it serves requests.
	const childEnv = "WORKFLOW_GATE_INVALID_CONFIG_CHILD"
	if os.Getenv(childEnv) == "1" {
		newMCPServer(nil)
		return
	}
	t.Setenv(workflowWritesEnabledEnv, "typo")
	executable, err := os.Executable()
	require.NoError(t, err)
	child := exec.Command(executable, "-test.run=^TestWorkflowWriteGateRejectsInvalidConfig$")
	child.Env = append(os.Environ(), childEnv+"=1")
	output, err := child.CombinedOutput()
	var exitError *exec.ExitError
	require.ErrorAs(t, err, &exitError)
	require.Equal(t, 1, exitError.ExitCode())
	require.Contains(t, string(output), "WORKFLOW_WRITES_ENABLED must be true/false, 1/0, or yes/no")
}

func TestWorkflowWriteGateThroughTransports(t *testing.T) {
	t.Setenv(workflowWritesEnabledEnv, "false")
	testWorkflowWritesThroughTransports(t, false)
}
