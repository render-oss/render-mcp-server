package validate

import (
	"fmt"

	"github.com/render-oss/cli/pkg/config"
)

// WorkspaceMatches gets the workspace from the config and validates that it matches the provided input. If the
// workspace is not set, no error is returned
func WorkspaceMatches(workspaceID string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Workspace != "" && cfg.Workspace != workspaceID {
		return fmt.Errorf("resource in workspace does not match the workspace in the current workspace context %s. Run `render workspace set %s` to change contexts", cfg.Workspace, workspaceID)
	}
	return nil
}
