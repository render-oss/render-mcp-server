package owner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/config"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

func Tools(c *client.ClientWithResponses) []server.ServerTool {
	ownerRepo := NewRepo(c)

	return []server.ServerTool{
		listWorkspaces(ownerRepo),
		selectWorkspace(),
		getSelectedWorkspace(),
	}
}

func listWorkspaces(ownerRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("list_workspaces",
			mcp.WithDescription("List the workspaces that you have access to"),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:          "List workspaces",
				ReadOnlyHint:   true,
				IdempotentHint: true,
				OpenWorldHint:  true,
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

			resultText := ""

			if len(workspaces) == 1 {
				err = config.SelectWorkspace(workspaces[0].Id)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				resultText = "Only one workspace found, automatically selected it"
			}

			resultText += string(respJSON)
			return mcp.NewToolResultText(resultText), nil
		},
	}
}

func selectWorkspace() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("select_workspace",
			mcp.WithDescription("Select a workspace to use for all actions. This tool should "+
				"only be used after explicitly asking the user to select one, it should not be invoked "+
				"as part of an automated process. Having the wrong workspace selected can lead to "+
				"destructive actions being performed on unintended resources."),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:          "Select workspace",
				IdempotentHint: true,
				OpenWorldHint:  true,
			}),
			mcp.WithString("ownerID",
				mcp.Required(),
				mcp.Description("The ID of the owner to select"),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ownerID, err := validate.RequiredToolParam[string](request, "ownerID")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			err = config.SelectWorkspace(ownerID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText("Workspace selected"), nil
		},
	}
}

func getSelectedWorkspace() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("get_selected_workspace",
			mcp.WithDescription("Get the currently selected workspace"),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:          "Get selected workspace",
				ReadOnlyHint:   true,
				IdempotentHint: true,
				OpenWorldHint:  true,
			}),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			workspace, err := config.WorkspaceID()
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(
				fmt.Sprintf("The currently selected workspace is: %s", workspace),
			), nil
		},
	}
}
