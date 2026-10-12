package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	envvar "github.com/render-oss/render-mcp-server/pkg/client/envvar"
	workflowclient "github.com/render-oss/render-mcp-server/pkg/client/workflows"
	"github.com/render-oss/render-mcp-server/pkg/mcpserver"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

// These generated Workflow enums have Valid but no Values helpers yet. Use
// their generated constants, as RegionEnumValues does, rather than API strings.
var workflowRuntimes = mcpserver.EnumValuesFromClientType(workflowclient.Node, workflowclient.Python, workflowclient.Go, workflowclient.Ruby, workflowclient.Elixir)
var workflowAutoDeploy = mcpserver.EnumValuesFromClientType(workflowclient.Commit, workflowclient.ChecksPass, workflowclient.Off)

func writeTool(name, description string, destructive bool, handler func(context.Context, mcp.CallToolRequest) (any, error), options ...mcp.ToolOption) server.ServerTool {
	options = append([]mcp.ToolOption{mcp.WithDescription(description), mcp.WithToolAnnotation(mcp.ToolAnnotation{
		ReadOnlyHint: pointers.From(false), DestructiveHint: pointers.From(destructive),
		IdempotentHint: pointers.From(false), OpenWorldHint: pointers.From(true),
	})}, options...)
	tool := mcp.NewTool(name, options...)
	return server.ServerTool{Tool: tool, Handler: func(ctx context.Context, q mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		for key := range q.GetArguments() {
			if key != "workspaceId" {
				if _, ok := tool.InputSchema.Properties[key]; !ok {
					return mcp.NewToolResultError("unsupported Workflows parameter; use the tool's declared inputs"), nil
				}
			}
		}
		value, err := handler(ctx, q)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		data, err := json.Marshal(value)
		if err != nil || len(data) > maxToolOutputBytes {
			return mcp.NewToolResultError("Workflows request was accepted, but its result could not be returned within the 256 KiB output limit. Inspect workflows or versions with the read tools; do not repeat the write"), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	}}
}

func createWorkflow(r *Repo) server.ServerTool {
	// Match create_web_service: this creates a new resource, rather than replacing
	// an existing deployment. The separately exposed version tool is destructive.
	return writeTool("create_workflow", "Create a real workflow in the selected workspace from a Git repository Render can clone. The configuration request is limited to 256 KiB. Automatic deployment defaults to commit; creation starts the initial build unless autoDeploy is off. Do not immediately call create_workflow_version after creation. Use list_workflow_versions and get_workflow_version to check readiness. A timeout does not prove creation failed; reconcile with list_workflows before retrying. Task execution is separate.", false,
		func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
			body, err := createWorkflowInput(q)
			if err != nil {
				return nil, err
			}
			owners, err := workspaceOwner(ctx)
			if err != nil {
				return nil, err
			}
			body.OwnerId = (*owners)[0]
			return r.Create(ctx, body)
		},
		idOption("name", "Name of the new workflow; must not be blank"),
		idOption("repo", "Git repository URL Render can clone. Pass its branch separately; do not include credentials in the URL"),
		mcp.WithString("runtime", mcp.Required(), mcp.Enum(workflowRuntimes...), mcp.Description("Workflow language runtime")),
		idOption("buildCommand", "Command that installs dependencies and builds the workflow; must not be blank"),
		idOption("runCommand", "Command that registers the workflow's tasks; must not be blank"),
		filterOption("branch", "Repository branch; omit to use the repository's default branch"),
		filterOption("rootDir", "Repository subdirectory containing the workflow; omit to use the repository root"),
		mcp.WithString("region", mcp.Enum(mcpserver.RegionEnumValues()...), mcp.DefaultString(string(workflowclient.Oregon)), mcp.Description("Workflow region; defaults to oregon")),
		mcp.WithString("autoDeploy", mcp.Enum(workflowAutoDeploy...), mcp.Description("commit deploys on pushes, checksPass waits for repository checks, off requires a manual deploy. Omit for Render's commit default. Creation starts an initial build for commit or checksPass. Sent to the API as autoDeployTrigger")),
		mcp.WithArray("envVars", mcp.Description("Environment variables for builds and task execution, as key/value objects. Empty values are allowed. No fromDatabase or generateValue inputs"), mcp.Items(map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"key", "value"},
			"properties": map[string]any{"key": map[string]any{"type": "string"}, "value": map[string]any{"type": "string"}},
		})),
	)
}

func createWorkflowInput(q mcp.CallToolRequest) (client.CreateWorkflowJSONRequestBody, error) {
	var body client.CreateWorkflowJSONRequestBody
	var err error
	if body.Name, err = requiredID(q, "name"); err != nil {
		return body, err
	}
	if body.BuildConfig.Repo, err = requiredID(q, "repo"); err != nil {
		return body, err
	}
	runtime, err := requiredID(q, "runtime")
	if err != nil {
		return body, err
	}
	body.BuildConfig.Runtime = workflowclient.Runtime(runtime)
	if !body.BuildConfig.Runtime.Valid() || !slices.Contains(workflowRuntimes, runtime) {
		return body, fmt.Errorf("runtime must be one of %s", strings.Join(workflowRuntimes, ", "))
	}
	if body.BuildConfig.BuildCommand, err = requiredID(q, "buildCommand"); err != nil {
		return body, err
	}
	if body.RunCommand, err = requiredID(q, "runCommand"); err != nil {
		return body, err
	}
	if body.BuildConfig.Branch, err = optionalString(q, "branch"); err != nil {
		return body, err
	}
	if body.BuildConfig.RootDir, err = optionalString(q, "rootDir"); err != nil {
		return body, err
	}
	region, err := optionalString(q, "region")
	if err != nil {
		return body, err
	}
	body.Region = workflowclient.Oregon
	if region != nil {
		body.Region = workflowclient.Region(*region)
	}
	if !body.Region.Valid() || !slices.Contains(mcpserver.RegionEnumValues(), string(body.Region)) {
		return body, fmt.Errorf("region must be one of %s", strings.Join(mcpserver.RegionEnumValues(), ", "))
	}
	trigger, err := optionalString(q, "autoDeploy")
	if err != nil {
		return body, err
	}
	if trigger != nil {
		value := workflowclient.AutoDeployTrigger(*trigger)
		if !value.Valid() || !slices.Contains(workflowAutoDeploy, *trigger) {
			return body, fmt.Errorf("autoDeploy must be one of %s", strings.Join(workflowAutoDeploy, ", "))
		}
		body.AutoDeployTrigger = &value
	}
	// The shared parser extracts key/value entries. Reject unsupported fields
	// before calling it so a misspelled or generated secret cannot be ignored.
	if raw, exists := q.GetArguments()["envVars"]; exists {
		items, ok := raw.([]any)
		if !ok {
			return body, errors.New("envVars must be an array of key/value objects")
		}
		seen := make(map[string]bool, len(items))
		for _, item := range items {
			fields, ok := item.(map[string]any)
			if !ok || len(fields) != 2 {
				return body, errors.New("each envVars entry must contain only key and value")
			}
			key, ok := fields["key"].(string)
			if !ok || strings.TrimSpace(key) == "" || strings.ContainsAny(key, "=\x00") || seen[key] {
				return body, errors.New("envVars keys must be nonblank, unique, and contain neither '=' nor NUL")
			}
			if _, ok := fields["value"].(string); !ok {
				return body, errors.New("envVars values must be strings")
			}
			seen[key] = true
		}
		envVars, _, err := validate.EnvVars(q)
		if err != nil {
			return body, err
		}
		if envVars == nil {
			envVars = make(envvar.EnvVarInputArray, 0)
		}
		body.EnvVars = &envVars
	}
	return body, nil
}

func createWorkflowVersion(r *Repo) server.ServerTool {
	return writeTool("create_workflow_version", "Request a new deployment of an existing workflow. This changes which version subsequent task runs use once ready; it does not start a task run. Skip after creation or a push when an automatic deployment is already expected: commit starts on pushes, checksPass waits for repository checks. Use for manual deployment when autoDeploy is off, an explicitly requested redeploy, or a chosen commit. Returns acceptance, not readiness; follow with get_workflow_version or list_workflow_versions. Do not automatically retry a timed-out or disconnected request.", true,
		func(ctx context.Context, q mcp.CallToolRequest) (any, error) {
			id, err := requiredID(q, "workflowId")
			if err != nil {
				return nil, err
			}
			commit, err := optionalString(q, "commit")
			if err != nil {
				return nil, err
			}
			return r.CreateVersion(ctx, client.CreateWorkflowVersionJSONRequestBody{WorkflowId: id, Commit: commit})
		}, idOption("workflowId", "Exact ID of the workflow to deploy in the selected workspace"), filterOption("commit", "Git commit to deploy; omit to deploy the linked branch's current commit"))
}
