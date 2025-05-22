package keyvalue

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/config"
	"github.com/render-oss/render-mcp-server/pkg/mcpserver"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

func Tools(c *client.ClientWithResponses) []server.ServerTool {
	keyValueRepo := NewRepo(c)

	return []server.ServerTool{
		listKeyValue(keyValueRepo),
		getKeyValue(keyValueRepo),
		createKeyValue(keyValueRepo),
	}
}

func listKeyValue(keyValueRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("list_key_value",
			mcp.WithDescription("List all Key Value instances in your Render account"),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:          "List Key Value instances",
				ReadOnlyHint:   true,
				IdempotentHint: true,
				OpenWorldHint:  true,
			}),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			keyValues, err := keyValueRepo.ListKeyValue(ctx, &client.ListKeyValueParams{})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			if len(keyValues) == 0 {
				return mcp.NewToolResultText("No Key Value instances found"), nil
			}

			respJSON, err := json.Marshal(keyValues)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(string(respJSON)), nil
		},
	}
}

func getKeyValue(keyValueRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("get_key_value",
			mcp.WithDescription("Retrieve a Key Value instance by ID"),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:          "Get Key Value instance details",
				ReadOnlyHint:   true,
				IdempotentHint: true,
				OpenWorldHint:  true,
			}),
			mcp.WithString("keyValueId",
				mcp.Required(),
				mcp.Description("The ID of the Key Value instance to retrieve"),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			keyValueId, err := validate.RequiredToolParam[string](request, "keyValueId")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			keyValue, err := keyValueRepo.GetKeyValue(ctx, keyValueId)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			respJSON, err := json.Marshal(keyValue)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(string(respJSON)), nil
		},
	}
}

func createKeyValue(keyValueRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("create_key_value",
			mcp.WithDescription("Create a new Key Value instance in your Render account"),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:          "Create Key Value instance",
				ReadOnlyHint:   false,
				IdempotentHint: false,
				OpenWorldHint:  true,
			}),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description("Name of the Key Value instance"),
			),
			mcp.WithString("plan",
				mcp.Required(),
				mcp.Description("Pricing plan for the Key Value instance"),
				mcp.Enum(mcpserver.EnumValuesFromClientType(client.KeyValuePlanFree, client.KeyValuePlanStarter, client.KeyValuePlanStandard, client.KeyValuePlanPro, client.KeyValuePlanProPlus)...),
				mcp.DefaultString(string(client.KeyValuePlanFree)),
			),
			mcp.WithString("region",
				mcp.Description("Region where the Key Value instance will be deployed"),
				mcp.Enum(mcpserver.RegionEnumValues()...),
				mcp.DefaultString(string(client.Oregon)),
			),
			mcp.WithString("maxmemoryPolicy",
				mcp.Description("The eviction policy for the Key Value store"),
				mcp.Enum(mcpserver.EnumValuesFromClientType(client.Noeviction, client.AllkeysLfu, client.AllkeysLru, client.AllkeysRandom, client.VolatileLfu, client.VolatileLru, client.VolatileRandom, client.VolatileTtl)...),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, err := validate.RequiredToolParam[string](request, "name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ownerId, err := config.WorkspaceID()
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			plan, err := validate.RequiredToolParam[string](request, "plan")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			keyValuePlan, err := validate.KeyValuePlan(plan)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			createParams := client.KeyValuePOSTInput{
				Name:    name,
				OwnerId: ownerId,
				Plan:    *keyValuePlan,
			}

			if region, ok, err := validate.OptionalToolParam[string](request, "region"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				createParams.Region = &region
			}

			if maxmemoryPolicy, ok, err := validate.OptionalToolParam[string](request, "maxmemoryPolicy"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				createParams.MaxmemoryPolicy = pointers.From(client.MaxmemoryPolicy(maxmemoryPolicy))
			}

			keyValue, err := keyValueRepo.CreateKeyValue(ctx, createParams)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			respJSON, err := json.Marshal(keyValue)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(string(respJSON)), nil
		},
	}
}
