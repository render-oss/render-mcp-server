package validate

import (
	"context"
	"fmt"

	"github.com/render-oss/render-mcp-server/pkg/session"
)

// WorkspaceMatches gets the workspace from the config and validates that it matches the provided input. If the
// workspace is not set, no error is returned
func WorkspaceMatches(ctx context.Context, workspaceID string) error {
	workspace, err := session.FromContext(ctx).GetWorkspace(ctx)
	if err != nil {
		return err
	}
	if workspace != "" && workspace != workspaceID {
		return fmt.Errorf("resource belongs to workspace %s, but this tool call targets workspace %s. "+
			"Ask the user to confirm the intended workspace before retrying with the matching `workspaceId`",
			workspaceID, workspace)
	}
	return nil
}
