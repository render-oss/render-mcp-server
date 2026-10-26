package blueprint

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	blueprintclient "github.com/render-oss/render-mcp-server/pkg/client/blueprints"
	"github.com/render-oss/render-mcp-server/pkg/config"
	"github.com/render-oss/render-mcp-server/pkg/mcpserver"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

const planningOutcomeGuidance = "Read planningOutcome to determine the next step. " +
	"If planningOutcome is \"failed\", inspect the errors in notices. Address the reported " +
	"issues and push any Blueprint file changes, then call plan_blueprint with the returned " +
	"Blueprint ID to retry. Do not approve a failed plan, even if a sync is present. " +
	"If planningOutcome is \"no_changes\", there are no infrastructure changes to approve. " +
	"If planningOutcome is \"planned\", sync contains the planned infrastructure changes. " +
	"Summarize those changes for the user, or offer sync.dashboardUrl for review. If " +
	"sync.requiredManualEnvVars lists environment variables, ask the user for each value. " +
	"To apply the changes, call approve_blueprint with the Blueprint ID and any supplied " +
	"values in manualEnvVars. Never invent values. " +
	"If the user prefers to keep secret values outside the agent context, they can supply " +
	"manualEnvVars directly through POST /v1/blueprints/{blueprintId}/approve, or open " +
	"sync.dashboardUrl, approve the sync, and then configure the values on each service’s " +
	"Environment page. With the Dashboard option, initial deployments may fail until the " +
	"required values are set."

func Tools(c *client.ClientWithResponses) []server.ServerTool {
	repo := NewRepo(c)
	return []server.ServerTool{
		createBlueprint(repo),
	}
}

func createBlueprint(repo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("create_blueprint",
			mcp.WithDescription("Experimental (early access): Tool access and contract are subject to change. "+
				"Create a Render Blueprint from a Blueprint YAML file in a public repository or a "+
				"repository accessible through a connected Git provider, and plan its first sync. "+
				"By default, the plan creates separate resources and leaves existing resources "+
				"unchanged. Resource changes are applied only after approval with "+
				"approve_blueprint. After the first approval, later pushes deploy automatically "+
				"when autoSync is enabled. All Blueprint changes are read from the remote "+
				"repository. When working in a local checkout, push your changes first and pass "+
				"its origin URL as repoUrl and current branch as branch. Returns blueprint, "+
				"planningOutcome, sync, and notices. A successful tool call can return "+
				"planningOutcome: \"failed\". A failed planning attempt may include a persisted "+
				"sync or sync: null. "+
				planningOutcomeGuidance),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "Create Blueprint",
				ReadOnlyHint:    new(false),
				DestructiveHint: new(false),
				IdempotentHint:  new(false),
				OpenWorldHint:   new(true),
			}),
			mcp.WithString("repoUrl",
				mcp.Required(),
				mcp.Description("Repository URL starting with https:// or ssh://, without a query string or fragment. "+
					"SSH URLs are converted to HTTPS. SCP-style remotes (git@host:path) are not accepted. Specify the branch separately with branch."),
			),
			mcp.WithString("branch",
				mcp.Description("Branch containing the Blueprint file. Defaults to the repository's default branch."),
			),
			mcp.WithString("path",
				mcp.DefaultString("render.yaml"),
				mcp.Description("Blueprint file path relative to the repository root. Defaults to render.yaml."),
			),
			mcp.WithString("name",
				mcp.Description("Blueprint name. Defaults to the repository name without the .git suffix."),
			),
			mcp.WithBoolean("autoSync",
				mcp.DefaultBool(true),
				mcp.Description("Automatically apply later pushes to the branch after the first sync "+
					"is approved. Defaults to true."),
			),
			mcp.WithString("existingResources",
				mcp.DefaultString(string(blueprintclient.CreateNew)),
				mcp.Enum(mcpserver.EnumValuesFromClientType(blueprintclient.AdoptExisting, blueprintclient.CreateNew)...),
				mcp.Description("How Sync planning should handle existing resources. Defaults to create_new. "+
					"create_new creates a separate set of resources using a Blueprint-specific "+
					"random suffix, leaving existing resources unchanged. adopt brings matching "+
					"existing resources under this Blueprint's management and updates their "+
					"configuration to match the Blueprint file."),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			input, err := createBlueprintRequest(ctx, request)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			response, err := repo.CreateBlueprint(ctx, *input)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			responseJSON, err := json.Marshal(response)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(string(responseJSON)), nil
		},
	}
}

func createBlueprintRequest(ctx context.Context, request mcp.CallToolRequest) (*client.CreateBlueprintJSONRequestBody, error) {
	params, err := validateCreateBlueprintParams(request)
	if err != nil {
		return nil, err
	}

	ownerID, err := session.FromContext(ctx).GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if ownerID == "" {
		return nil, config.ErrNoWorkspace
	}

	return &client.CreateBlueprintJSONRequestBody{
		OwnerId: ownerID,
		Name:    params.name,
		Source: blueprintclient.BlueprintSource{
			Repo:   params.repoURL,
			Branch: params.branch,
			Path:   &params.path,
		},
		AutoSync:          &params.autoSync,
		ExistingResources: &params.existingResources,
	}, nil
}

type createBlueprintParams struct {
	repoURL           string
	branch            *string
	path              string
	name              string
	autoSync          bool
	existingResources blueprintclient.ExistingResourceMode
}

func validateCreateBlueprintParams(request mcp.CallToolRequest) (*createBlueprintParams, error) {
	repo, err := validate.RequiredToolParam[string](request, "repoUrl")
	if err != nil {
		return nil, err
	}
	repoURL, err := parseRepoURL(repo)
	if err != nil {
		return nil, err
	}

	input := &createBlueprintParams{
		repoURL:           repoURL.String(),
		name:              strings.TrimSuffix(path.Base(repoURL.Path), ".git"),
		path:              "render.yaml",
		autoSync:          true,
		existingResources: blueprintclient.CreateNew,
	}
	if name, ok, err := validate.OptionalToolParam[string](request, "name"); err != nil {
		return nil, err
	} else if ok {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("name cannot be empty; omit it to use the repository name")
		}
		input.name = name
	}
	if branch, ok, err := validate.OptionalToolParam[string](request, "branch"); err != nil {
		return nil, err
	} else if ok {
		if strings.TrimSpace(branch) == "" {
			return nil, fmt.Errorf("branch cannot be empty; omit it to use the repository's default branch")
		}
		input.branch = &branch
	}
	if filePath, ok, err := validate.OptionalToolParam[string](request, "path"); err != nil {
		return nil, err
	} else if ok {
		if strings.TrimSpace(filePath) == "" {
			return nil, fmt.Errorf("path cannot be empty; omit it to use render.yaml")
		}
		input.path = filePath
	}
	if autoSync, ok, err := validate.OptionalToolParam[bool](request, "autoSync"); err != nil {
		return nil, err
	} else if ok {
		input.autoSync = autoSync
	}
	if mode, ok, err := validate.OptionalToolParam[string](request, "existingResources"); err != nil {
		return nil, err
	} else if ok {
		existingResources := blueprintclient.ExistingResourceMode(mode)
		if !existingResources.Valid() {
			return nil, fmt.Errorf("existingResources must be %s or %s", blueprintclient.AdoptExisting, blueprintclient.CreateNew)
		}
		input.existingResources = existingResources
	}
	return input, nil
}
