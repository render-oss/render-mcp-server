package deploy

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

func Tools(c *client.ClientWithResponses) []server.ServerTool {
	deployRepo := NewRepo(c)

	return []server.ServerTool{
		listDeploys(deployRepo),
		getDeploy(deployRepo),
	}
}

func listDeploys(deployRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("list_deploys",
			mcp.WithDescription("List deploys matching the provided filters. If no filters are provided, all deploys for the service are returned."),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:          "List deploys",
				ReadOnlyHint:   pointers.From(true),
				IdempotentHint: pointers.From(true),
				OpenWorldHint:  pointers.From(true),
			}),
			mcp.WithString("serviceId",
				mcp.Required(),
				mcp.Description("The ID of the service to get deployments for"),
			),
			mcp.WithNumber("limit",
				mcp.Description("The maximum number of deploys to return in a single page. To fetch "+
					"additional pages of results, set the cursor to the last deploy in the previous page. "+
					"It should be rare to need to set this value greater than 20."),
				mcp.DefaultNumber(10),
				mcp.Min(1),
				mcp.Max(100),
			),
			mcp.WithString("cursor",
				mcp.Description("A unique string that corresponds to a position in the result list. "+
					"If provided, the endpoint returns results that appear after the corresponding position. "+
					"To fetch the first page of results, set to the empty string."),
				mcp.DefaultString(""),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			serviceId, err := validate.RequiredToolParam[string](request, "serviceId")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			params := &client.ListDeploysParams{}
			if limit, ok, err := validate.OptionalToolParam[float64](request, "limit"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				params.Limit = pointers.From(int(limit))
			}

			if cursor, ok, err := validate.OptionalToolParam[string](request, "cursor"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				params.Cursor = &cursor
			}

			deploys, cursor, err := deployRepo.ListDeploys(ctx, serviceId, params)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			respJSON, err := json.Marshal(deploys)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			respText := string(respJSON) + "\n\n cursor: "

			if cursor == nil {
				respText += `""`
			} else {
				respText += *cursor
			}

			return mcp.NewToolResultText(respText), nil
		},
	}
}

func getDeploy(deployRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("get_deploy",
			mcp.WithDescription("Retrieve the details of a particular deploy for a particular service."),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:          "Get deploy details",
				ReadOnlyHint:   pointers.From(true),
				IdempotentHint: pointers.From(true),
				OpenWorldHint:  pointers.From(true),
			}),
			mcp.WithString("serviceId",
				mcp.Required(),
				mcp.Description("The ID of the service to get deployments for"),
			),
			mcp.WithString("deployId",
				mcp.Required(),
				mcp.Description("The ID of the deployment to retrieve"),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			serviceId, err := validate.RequiredToolParam[string](request, "serviceId")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			deployId, err := validate.RequiredToolParam[string](request, "deployId")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			response, err := deployRepo.GetDeploy(ctx, serviceId, deployId)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			respJSON, err := json.Marshal(response)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(string(respJSON)), nil
		},
	}
}
