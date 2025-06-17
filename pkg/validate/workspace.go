package validate

import (
	"context"
	"fmt"

	"github.com/render-oss/render-mcp-server/pkg/session"
)

// WorkspaceMatches gets the workspace from the config and validates that it matches the provided input. If the
// workspace is not set, no error is returned
func WorkspaceMatches(ctx context.Context, workspaceID string) error {
	workspace, err := session.FromContext(ctx).GetWorkspace()
	if err != nil {
		return err
	}
	if workspace != "" && workspace != workspaceID {
		return fmt.Errorf("resource in workspace does not match the workspace in the current "+
			"workspace context %s. You can use the `select_workspace` tool to change contexts to %s "+
			", but you should only do this after asking the user to confirm", workspace, workspaceID)
	}
	return nil
}
