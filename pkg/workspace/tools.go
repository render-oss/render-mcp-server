package workspace

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

const workspaceIDDescription = "The ID of the Render workspace to use. Reuse the workspaceId the user confirmed " +
	"from list_workspaces."

const (
	serviceResourceIDPrefix  = "srv-"
	postgresResourceIDPrefix = "dpg-"
	keyValueResourceIDPrefix = "red-"
)

type renderClient interface {
	RetrieveOwnerWithResponse(
		ctx context.Context,
		workspaceID string,
		reqEditors ...client.RequestEditorFn,
	) (*client.RetrieveOwnerResponse, error)
	RetrieveServiceWithResponse(
		ctx context.Context,
		serviceID string,
		reqEditors ...client.RequestEditorFn,
	) (*client.RetrieveServiceResponse, error)
	RetrievePostgresWithResponse(
		ctx context.Context,
		postgresID string,
		reqEditors ...client.RequestEditorFn,
	) (*client.RetrievePostgresResponse, error)
	RetrieveKeyValueWithResponse(
		ctx context.Context,
		keyValueID string,
		reqEditors ...client.RequestEditorFn,
	) (*client.RetrieveKeyValueResponse, error)
}

type Resolver struct {
	client renderClient
}

func NewResolver(client renderClient) *Resolver {
	return &Resolver{client: client}
}

func (r *Resolver) Resolve(ctx context.Context, request mcp.CallToolRequest) (context.Context, error) {
	workspaceID, ok, err := validate.OptionalToolParam[string](request, "workspaceId")
	if err != nil {
		return ctx, err
	}
	if !ok {
		return ctx, nil
	}
	if workspaceID == "" {
		return ctx, fmt.Errorf("workspaceId cannot be empty")
	}

	resolvedContext := session.ContextWithWorkspace(ctx, workspaceID)
	resourceProvedWorkspaceAccess, err := r.validateResourceWorkspace(resolvedContext, request)
	if err != nil {
		return ctx, err
	}
	// The authenticated resource lookup and owner match prove workspace access
	// for this request, so a separate owner lookup would be redundant.
	if resourceProvedWorkspaceAccess {
		return resolvedContext, nil
	}

	response, err := r.client.RetrieveOwnerWithResponse(ctx, workspaceID)
	if err != nil {
		return ctx, fmt.Errorf("failed to validate workspace %s: %w", workspaceID, err)
	}
	if _, err := client.BodyFromResponse(response.JSON200, response); err != nil {
		return ctx, fmt.Errorf("cannot access workspace %s: %w", workspaceID, err)
	}

	return resolvedContext, nil
}

// validateResourceWorkspace does not cover the logs tools' `resource` array;
// the logs API authorizes each resource individually rather than against the
// supplied owner, so those entries are not pinned to the workspace here.
func (r *Resolver) validateResourceWorkspace(ctx context.Context, request mcp.CallToolRequest) (bool, error) {
	if serviceID, ok, err := validate.OptionalToolParam[string](request, "serviceId"); err != nil {
		return false, err
	} else if ok {
		return r.validateServiceWorkspace(ctx, serviceID)
	}

	if postgresID, ok, err := validate.OptionalToolParam[string](request, "postgresId"); err != nil {
		return false, err
	} else if ok {
		return r.validatePostgresWorkspace(ctx, postgresID)
	}

	if keyValueID, ok, err := validate.OptionalToolParam[string](request, "keyValueId"); err != nil {
		return false, err
	} else if ok {
		return r.validateKeyValueWorkspace(ctx, keyValueID)
	}

	if resourceID, ok, err := validate.OptionalToolParam[string](request, "resourceId"); err != nil {
		return false, err
	} else if ok {
		err := r.validateMetricsResourceWorkspace(ctx, resourceID)
		return err == nil, err
	}

	return false, nil
}

func (r *Resolver) validateServiceWorkspace(ctx context.Context, serviceID string) (bool, error) {
	response, err := r.client.RetrieveServiceWithResponse(ctx, serviceID)
	if err != nil {
		return false, fmt.Errorf("service %s: %w", serviceID, err)
	}
	if response.StatusCode() == 404 {
		return false, nil
	}
	service, err := client.BodyFromResponse(response.JSON200, response)
	if err != nil {
		return false, fmt.Errorf("service %s: %w", serviceID, err)
	}
	return true, validate.WorkspaceMatches(ctx, service.OwnerId)
}

func (r *Resolver) validatePostgresWorkspace(ctx context.Context, postgresID string) (bool, error) {
	response, err := r.client.RetrievePostgresWithResponse(ctx, postgresID)
	if err != nil {
		return false, fmt.Errorf("postgres %s: %w", postgresID, err)
	}
	if response.StatusCode() == 404 {
		return false, nil
	}
	postgres, err := client.BodyFromResponse(response.JSON200, response)
	if err != nil {
		return false, fmt.Errorf("postgres %s: %w", postgresID, err)
	}
	return true, validate.WorkspaceMatches(ctx, postgres.Owner.Id)
}

func (r *Resolver) validateKeyValueWorkspace(ctx context.Context, keyValueID string) (bool, error) {
	response, err := r.client.RetrieveKeyValueWithResponse(ctx, keyValueID)
	if err != nil {
		return false, fmt.Errorf("key value store %s: %w", keyValueID, err)
	}
	if response.StatusCode() == 404 {
		return false, nil
	}
	keyValue, err := client.BodyFromResponse(response.JSON200, response)
	if err != nil {
		return false, fmt.Errorf("key value store %s: %w", keyValueID, err)
	}
	return true, validate.WorkspaceMatches(ctx, keyValue.Owner.Id)
}

func (r *Resolver) validateMetricsResourceWorkspace(ctx context.Context, resourceID string) error {
	switch {
	case strings.HasPrefix(resourceID, serviceResourceIDPrefix):
		found, err := r.validateServiceWorkspace(ctx, resourceID)
		return metricsResourceValidationError(resourceID, found, err)
	case strings.HasPrefix(resourceID, postgresResourceIDPrefix):
		found, err := r.validatePostgresWorkspace(ctx, resourceID)
		return metricsResourceValidationError(resourceID, found, err)
	case strings.HasPrefix(resourceID, keyValueResourceIDPrefix):
		found, err := r.validateKeyValueWorkspace(ctx, resourceID)
		return metricsResourceValidationError(resourceID, found, err)
	}

	// Fall back to discovery for resource ID formats introduced after this
	// server version.
	if found, err := r.validateServiceWorkspace(ctx, resourceID); found || err != nil {
		return err
	}
	if found, err := r.validatePostgresWorkspace(ctx, resourceID); found || err != nil {
		return err
	}
	if found, err := r.validateKeyValueWorkspace(ctx, resourceID); found || err != nil {
		return err
	}
	return fmt.Errorf("resource %s was not found", resourceID)
}

func metricsResourceValidationError(resourceID string, found bool, err error) error {
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("resource %s was not found", resourceID)
	}
	return nil
}

// AddWorkspaceIDParam declares the shared workspaceId parameter on each tool.
func AddWorkspaceIDParam(tools ...server.ServerTool) []server.ServerTool {
	withWorkspaceID := mcp.WithString("workspaceId", mcp.Description(workspaceIDDescription))
	out := make([]server.ServerTool, len(tools))
	for i, tool := range tools {
		tool.Tool.InputSchema.Properties = maps.Clone(tool.Tool.InputSchema.Properties)
		withWorkspaceID(&tool.Tool)
		out[i] = tool
	}
	return out
}

// ScopeTools wraps each tool's handler so it runs with the workspace context
// the resolver derives from the request.
func ScopeTools(resolver *Resolver, tools ...server.ServerTool) []server.ServerTool {
	scopedTools := make([]server.ServerTool, len(tools))

	for i, tool := range tools {
		handler := tool.Handler
		tool.Handler = func(
			ctx context.Context,
			request mcp.CallToolRequest,
		) (*mcp.CallToolResult, error) {
			resolvedContext, err := resolver.Resolve(ctx, request)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return handler(resolvedContext, request)
		}
		scopedTools[i] = tool
	}

	return scopedTools
}
