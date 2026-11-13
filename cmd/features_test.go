package cmd

import (
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"testing"

	"github.com/render-oss/render-mcp-server/pkg/logging"
	"github.com/stretchr/testify/require"
)

func TestEnabledFeatures(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		enabled     bool
	}{
		{"unset", "", false}, {"empty", "", false}, {"blank entries", " , ", false},
		{"enabled", "durability-2.1", true}, {"case and spacing", " Durability-2.1 ,", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(featuresEnabledEnv, tc.value)
			if tc.name == "unset" {
				require.NoError(t, os.Unsetenv(featuresEnabledEnv))
			}
			require.Equal(t, tc.enabled, enabledFeatures()[featureDurability21])
			require.Equal(t, tc.enabled, workflowOptions().Durability21)
		})
	}
}

func TestWorkflowWritesAreAlwaysRegistered(t *testing.T) {
	t.Setenv(featuresEnabledEnv, "")
	var names []string
	for _, tool := range buildWorkspaceScopedTools(nil) {
		names = append(names, tool.Tool.Name)
		require.Contains(t, tool.Tool.InputSchema.Properties, "workspaceId")
	}
	require.Subset(t, names, []string{"create_workflow", "create_workflow_version", "start_flow_run", "cancel_flow_run"})
}

func TestDurability21FeatureReachesFlowTools(t *testing.T) {
	cancelDescription := func() string {
		for _, tool := range buildWorkspaceScopedTools(nil) {
			if tool.Tool.Name == "cancel_flow_run" {
				return tool.Tool.Description
			}
		}
		t.Fatal("cancel_flow_run was not registered")
		return ""
	}
	t.Setenv(featuresEnabledEnv, "")
	require.NotContains(t, cancelDescription(), "paused")
	t.Setenv(featuresEnabledEnv, featureDurability21)
	require.Contains(t, cancelDescription(), "paused")
}

func TestFeaturesRejectUnknownNames(t *testing.T) {
	// Exercise the real server constructor in a child process because bad
	// startup configuration must stop the process before it serves requests.
	const childEnv = "FEATURES_INVALID_CONFIG_CHILD"
	if os.Getenv(childEnv) == "1" {
		logging.SetDefault(logging.New(os.Stderr, slog.LevelError))
		newMCPServer(nil)
		return
	}
	t.Setenv(featuresEnabledEnv, "durability-2.1,typo")
	executable, err := os.Executable()
	require.NoError(t, err)
	child := exec.Command(executable, "-test.run=^TestFeaturesRejectUnknownNames$")
	child.Env = append(os.Environ(), childEnv+"=1")
	output, err := child.CombinedOutput()
	var exitError *exec.ExitError
	require.ErrorAs(t, err, &exitError)
	require.Equal(t, 1, exitError.ExitCode())
	require.NotEmpty(t, output, "invalid feature configuration must be logged at ERROR level")
	var record map[string]any
	require.NoError(t, json.Unmarshal(output, &record))
	require.Equal(t, "ERROR", record["level"])
	require.Equal(t, "unknown feature", record["msg"])
	require.Equal(t, featuresEnabledEnv, record["env"])
	require.Equal(t, "typo", record["feature"])
	require.Equal(t, []any{featureDurability21}, record["known_features"])
}
