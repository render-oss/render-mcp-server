package owner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

func Tools(c *client.ClientWithResponses) []server.ServerTool {
	ownerRepo := NewRepo(c)

	return []server.ServerTool{
		listWorkspaces(ownerRepo),
		selectWorkspace(ownerRepo),
		getSelectedWorkspace(),
	}
}

func listWorkspaces(ownerRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("list_workspaces",
			mcp.WithDescription("List the workspaces that you have access to"),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "List workspaces",
				ReadOnlyHint:    pointers.From(false),
				DestructiveHint: pointers.From(false),
				IdempotentHint:  pointers.From(true),
				OpenWorldHint:   pointers.From(false),
			}),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			workspaces, err := ownerRepo.ListOwners(ctx, ListInput{})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			respJSON, err := json.Marshal(workspaces)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			if len(workspaces) == 1 {
				err = session.FromContext(ctx).SetWorkspace(ctx, workspaces[0].Id)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
			}

			return mcp.NewToolResultText(string(respJSON)), nil
		},
	}
}

func selectWorkspace(ownerRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("select_workspace",
			mcp.WithDescription("Deprecated: this tool is scheduled for removal; pass the confirmed "+
				"workspaceId directly on each tool call instead. Select a workspace for clients that "+
				"rely on MCP session state. This tool should only be used after explicitly asking the "+
				"user to select one, it should not be invoked as part of an automated process. Having "+
				"the wrong workspace selected can lead to destructive actions being performed on "+
				"unintended resources."),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "Select workspace",
				ReadOnlyHint:    pointers.From(false),
				DestructiveHint: pointers.From(false),
				IdempotentHint:  pointers.From(true),
				OpenWorldHint:   pointers.From(false),
			}),
			mcp.WithString("ownerID",
				mcp.Required(),
				mcp.Description("The ID of the workspace to select"),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			workspaceID, err := validate.RequiredToolParam[string](request, "ownerID")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			if _, err = ownerRepo.RetrieveOwner(ctx, workspaceID); err != nil {
				return mcp.NewToolResultError(
					fmt.Sprintf("cannot access workspace %s: %s", workspaceID, err),
				), nil
			}

			err = session.FromContext(ctx).SetWorkspace(ctx, workspaceID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(fmt.Sprintf("Workspace %s selected", workspaceID)), nil
		},
	}
}

func getSelectedWorkspace() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("get_selected_workspace",
			mcp.WithDescription("Get the workspace stored in the session compatibility fallback. "+
				"Request-scoped workspaces supplied with an explicit workspaceId are not reflected by this tool."),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "Get selected workspace",
				ReadOnlyHint:    pointers.From(true),
				DestructiveHint: pointers.From(false),
				IdempotentHint:  pointers.From(true),
				OpenWorldHint:   pointers.From(false),
			}),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			workspace, err := session.FromContext(ctx).GetWorkspace(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(
				fmt.Sprintf("The currently selected workspace is: %s", workspace),
			), nil
		},
	}
}
