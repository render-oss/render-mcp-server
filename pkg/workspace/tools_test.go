package workspace_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/render-oss/render-mcp-server/pkg/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScopeToolsUsesExplicitWorkspaceIDAcrossTransportSessions(t *testing.T) {
	redisServer := miniredis.RunT(t)
	redisStore, err := session.NewRedisStore("redis://" + redisServer.Addr())
	require.NoError(t, err)

	tests := []struct {
		name  string
		store session.Store
	}{
		{name: "in-memory", store: session.NewInMemoryStore()},
		{name: "redis", store: redisStore},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const workspaceID = "tea-explicit"

			sessionA := contextWithHTTPSession(t, tt.store, "session-a")
			require.NoError(t, session.FromContext(sessionA).SetWorkspace(sessionA, workspaceID))

			sessionB := contextWithHTTPSession(t, tt.store, "session-b")
			_, err := session.FromContext(sessionB).GetWorkspace(sessionB)
			require.Error(t, err)

			ownerClient := &fakeOwnerClient{
				owners: map[string]*client.Owner{
					workspaceID: {Id: workspaceID, Name: "Explicit workspace"},
				},
			}

			var handledWorkspace string
			baseTool := server.ServerTool{
				Tool: mcp.NewTool("workspace_sensitive_tool"),
				Handler: func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					handledWorkspace, err = session.FromContext(ctx).GetWorkspace(ctx)
					if err != nil {
						return mcp.NewToolResultError(err.Error()), nil
					}
					return mcp.NewToolResultText("ok"), nil
				},
			}

			scopedTool := workspace.ScopeTools(
				workspace.NewResolver(ownerClient),
				workspace.AddWorkspaceIDParam(baseTool)...,
			)[0]
			assert.NotContains(t, baseTool.Tool.InputSchema.Properties, "workspaceId")
			request := toolRequest(map[string]interface{}{"workspaceId": workspaceID})
			result, err := scopedTool.Handler(sessionB, request)

			require.NoError(t, err)
			require.False(t, result.IsError)
			assert.Equal(t, workspaceID, handledWorkspace)
			assert.Equal(t, 1, ownerClient.totalCalls())
		})
	}
}

func TestScopeToolsFallsBackToTransportSessionWorkspace(t *testing.T) {
	const workspaceID = "tea-session"

	store := session.NewInMemoryStore()
	ctx := contextWithHTTPSession(t, store, "session-a")
	require.NoError(t, session.FromContext(ctx).SetWorkspace(ctx, workspaceID))

	ownerClient := &fakeOwnerClient{}
	var handledWorkspace string
	scopedTool := scopedTestTool(
		ownerClient,
		"workspace_sensitive_tool",
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var err error
			handledWorkspace, err = session.FromContext(ctx).GetWorkspace(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText("ok"), nil
		},
	)
	result, err := scopedTool.Handler(ctx, toolRequest(nil))

	require.NoError(t, err)
	require.False(t, result.IsError)
	assert.Equal(t, workspaceID, handledWorkspace)
	assert.Equal(t, 0, ownerClient.totalCalls())
}

func TestScopeToolsExplainsHowToRecoverWithoutSessionState(t *testing.T) {
	scopedTool := scopedTestTool(
		&fakeOwnerClient{},
		"workspace_sensitive_tool",
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			_, err := session.FromContext(ctx).GetWorkspace(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText("ok"), nil
		},
	)
	result, err := scopedTool.Handler(
		contextWithHTTPSession(t, session.NewInMemoryStore(), "fresh-session"),
		toolRequest(nil),
	)

	require.NoError(t, err)
	require.True(t, result.IsError)
	message := result.Content[0].(mcp.TextContent).Text
	assert.Contains(t, message, "retry")
	assert.Contains(t, message, "original tool")
	assert.Contains(t, message, "workspaceId")
}

func TestScopeToolsRejectsInaccessibleWorkspaceIDBeforeCallingTool(t *testing.T) {
	ownerClient := &fakeOwnerClient{owners: map[string]*client.Owner{}}
	handlerCalled := false
	scopedTool := scopedTestTool(
		ownerClient,
		"destructive_workspace_tool",
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			handlerCalled = true
			return mcp.NewToolResultText("mutated"), nil
		},
	)
	result, err := scopedTool.Handler(
		contextWithHTTPSession(t, session.NewInMemoryStore(), "session-b"),
		toolRequest(map[string]interface{}{"workspaceId": "tea-inaccessible"}),
	)

	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.False(t, handlerCalled)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "tea-inaccessible")
}

func TestScopeToolsRejectsResourcesFromAnotherWorkspaceBeforeCallingTool(t *testing.T) {
	const (
		selectedWorkspaceID = "tea-selected"
		otherWorkspaceID    = "tea-other"
	)

	tests := []struct {
		name      string
		parameter string
		resource  string
	}{
		{name: "service tools", parameter: "serviceId", resource: "srv-other"},
		{name: "postgres tools", parameter: "postgresId", resource: "dpg-other"},
		{name: "key value tools", parameter: "keyValueId", resource: "red-other"},
		{name: "metrics tools", parameter: "resourceId", resource: "srv-other"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ownerClient := &fakeOwnerClient{
				services: map[string]*client.Service{
					"srv-other": {Id: "srv-other", OwnerId: otherWorkspaceID},
				},
				postgres: map[string]*client.PostgresDetail{
					"dpg-other": {Id: "dpg-other", Owner: client.Owner{Id: otherWorkspaceID}},
				},
				keyValues: map[string]*client.KeyValueDetail{
					"red-other": {Id: "red-other", Owner: client.Owner{Id: otherWorkspaceID}},
				},
			}
			handlerCalled := false
			scopedTool := scopedTestTool(
				ownerClient,
				"workspace_sensitive_tool",
				func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					handlerCalled = true
					return mcp.NewToolResultText("read resource"), nil
				},
			)
			result, err := scopedTool.Handler(
				contextWithHTTPSession(t, session.NewInMemoryStore(), "session-b"),
				toolRequest(map[string]interface{}{
					"workspaceId": selectedWorkspaceID,
					tt.parameter:  tt.resource,
				}),
			)

			require.NoError(t, err)
			require.True(t, result.IsError)
			assert.False(t, handlerCalled)
			assert.Contains(t, result.Content[0].(mcp.TextContent).Text, otherWorkspaceID)
			assert.Contains(t, result.Content[0].(mcp.TextContent).Text, selectedWorkspaceID)
		})
	}
}

func TestScopeToolsAllowsResourcesFromExplicitWorkspace(t *testing.T) {
	const workspaceID = "tea-selected"

	ownerClient := &fakeOwnerClient{
		services: map[string]*client.Service{
			"srv-selected": {Id: "srv-selected", OwnerId: workspaceID},
		},
	}
	handlerCalled := false
	scopedTool := scopedTestTool(
		ownerClient,
		"get_service",
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			handlerCalled = true
			return mcp.NewToolResultText("read resource"), nil
		},
	)
	result, err := scopedTool.Handler(
		contextWithHTTPSession(t, session.NewInMemoryStore(), "session-b"),
		toolRequest(map[string]interface{}{
			"workspaceId": workspaceID,
			"serviceId":   "srv-selected",
		}),
	)

	require.NoError(t, err)
	require.False(t, result.IsError)
	assert.True(t, handlerCalled)
	assert.Equal(t, 0, ownerClient.totalCalls(), "resource ownership should avoid a redundant owner lookup")
}

func TestScopeToolsValidatesMetricsResourcesEfficiently(t *testing.T) {
	const workspaceID = "tea-selected"

	tests := []struct {
		name                  string
		resourceID            string
		services              map[string]*client.Service
		postgres              map[string]*client.PostgresDetail
		keyValues             map[string]*client.KeyValueDetail
		expectedResourceCalls int
	}{
		{
			name:       "service",
			resourceID: "srv-selected",
			services: map[string]*client.Service{
				"srv-selected": {Id: "srv-selected", OwnerId: workspaceID},
			},
			expectedResourceCalls: 1,
		},
		{
			name:       "postgres",
			resourceID: "dpg-selected",
			postgres: map[string]*client.PostgresDetail{
				"dpg-selected": {Id: "dpg-selected", Owner: client.Owner{Id: workspaceID}},
			},
			expectedResourceCalls: 1,
		},
		{
			name:       "key value",
			resourceID: "red-selected",
			keyValues: map[string]*client.KeyValueDetail{
				"red-selected": {Id: "red-selected", Owner: client.Owner{Id: workspaceID}},
			},
			expectedResourceCalls: 1,
		},
		{
			name:       "unknown prefix fallback",
			resourceID: "future-selected",
			keyValues: map[string]*client.KeyValueDetail{
				"future-selected": {Id: "future-selected", Owner: client.Owner{Id: workspaceID}},
			},
			expectedResourceCalls: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ownerClient := &fakeOwnerClient{
				services:  tt.services,
				postgres:  tt.postgres,
				keyValues: tt.keyValues,
			}
			scopedTool := scopedTestTool(
				ownerClient,
				"get_metrics",
				func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return mcp.NewToolResultText("read metrics"), nil
				},
			)
			result, err := scopedTool.Handler(
				contextWithHTTPSession(t, session.NewInMemoryStore(), "session-b"),
				toolRequest(map[string]interface{}{
					"workspaceId": workspaceID,
					"resourceId":  tt.resourceID,
				}),
			)

			require.NoError(t, err)
			require.False(t, result.IsError)
			assert.Equal(t, tt.expectedResourceCalls, ownerClient.totalResourceCalls())
			assert.Equal(t, 0, ownerClient.totalCalls())
		})
	}
}

func TestScopeToolsKeepsConcurrentWorkspaceContextsIndependent(t *testing.T) {
	ownerClient := &fakeOwnerClient{
		owners: map[string]*client.Owner{
			"tea-a": {Id: "tea-a", Name: "Workspace A"},
			"tea-b": {Id: "tea-b", Name: "Workspace B"},
		},
	}

	results := make(chan string, 2)
	scopedTool := scopedTestTool(
		ownerClient,
		"workspace_sensitive_tool",
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			workspaceID, err := session.FromContext(ctx).GetWorkspace(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			results <- workspaceID
			return mcp.NewToolResultText("ok"), nil
		},
	)

	ctx := contextWithHTTPSession(t, session.NewInMemoryStore(), "shared-session")
	var waitGroup sync.WaitGroup
	for _, workspaceID := range []string{"tea-a", "tea-b"} {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			result, err := scopedTool.Handler(ctx, toolRequest(map[string]interface{}{"workspaceId": workspaceID}))
			assert.NoError(t, err)
			assert.False(t, result.IsError)
		}()
	}
	waitGroup.Wait()
	close(results)

	actual := make([]string, 0, 2)
	for workspaceID := range results {
		actual = append(actual, workspaceID)
	}
	assert.ElementsMatch(t, []string{"tea-a", "tea-b"}, actual)
}

func TestScopeToolsDoesNotPersistExplicitWorkspaceToSession(t *testing.T) {
	const workspaceID = "tea-explicit"

	ctx := contextWithHTTPSession(t, session.NewInMemoryStore(), "session-a")
	ownerClient := &fakeOwnerClient{
		owners: map[string]*client.Owner{
			workspaceID: {Id: workspaceID, Name: "Explicit workspace"},
		},
	}
	scopedTool := scopedTestTool(
		ownerClient,
		"workspace_sensitive_tool",
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("ok"), nil
		},
	)
	result, err := scopedTool.Handler(ctx, toolRequest(map[string]interface{}{"workspaceId": workspaceID}))

	require.NoError(t, err)
	require.False(t, result.IsError)
	_, err = session.FromContext(ctx).GetWorkspace(ctx)
	require.Error(t, err, "explicit workspaceId must stay request-scoped, not select a session workspace")
}

func TestScopeToolsExplicitWorkspaceOverridesSessionSelection(t *testing.T) {
	const (
		sessionWorkspaceID  = "tea-session"
		explicitWorkspaceID = "tea-explicit"
	)

	ctx := contextWithHTTPSession(t, session.NewInMemoryStore(), "session-a")
	require.NoError(t, session.FromContext(ctx).SetWorkspace(ctx, sessionWorkspaceID))

	ownerClient := &fakeOwnerClient{
		owners: map[string]*client.Owner{
			explicitWorkspaceID: {Id: explicitWorkspaceID, Name: "Explicit workspace"},
		},
	}
	var handledWorkspace string
	scopedTool := scopedTestTool(
		ownerClient,
		"workspace_sensitive_tool",
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var err error
			handledWorkspace, err = session.FromContext(ctx).GetWorkspace(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText("ok"), nil
		},
	)
	result, err := scopedTool.Handler(ctx, toolRequest(map[string]interface{}{"workspaceId": explicitWorkspaceID}))

	require.NoError(t, err)
	require.False(t, result.IsError)
	assert.Equal(t, explicitWorkspaceID, handledWorkspace,
		"explicit workspaceId must win over the session selection")

	persisted, err := session.FromContext(ctx).GetWorkspace(ctx)
	require.NoError(t, err)
	assert.Equal(t, sessionWorkspaceID, persisted,
		"session selection must survive a call with an explicit workspaceId")
}

func TestScopeToolsRejectsInvalidWorkspaceIDParameter(t *testing.T) {
	tests := []struct {
		name        string
		workspaceID interface{}
	}{
		{name: "empty string", workspaceID: ""},
		{name: "wrong type", workspaceID: 123},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ownerClient := &fakeOwnerClient{}
			handlerCalled := false
			scopedTool := scopedTestTool(
				ownerClient,
				"workspace_sensitive_tool",
				func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					handlerCalled = true
					return mcp.NewToolResultText("ok"), nil
				},
			)
			result, err := scopedTool.Handler(
				contextWithHTTPSession(t, session.NewInMemoryStore(), "session-a"),
				toolRequest(map[string]interface{}{"workspaceId": tt.workspaceID}),
			)

			require.NoError(t, err)
			require.True(t, result.IsError)
			assert.False(t, handlerCalled)
			assert.Equal(t, 0, ownerClient.totalCalls())
		})
	}
}

func TestScopeToolsSurfacesWorkspaceLookupFailure(t *testing.T) {
	ownerClient := &fakeOwnerClient{ownerErr: assert.AnError}
	handlerCalled := false
	scopedTool := scopedTestTool(
		ownerClient,
		"workspace_sensitive_tool",
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			handlerCalled = true
			return mcp.NewToolResultText("ok"), nil
		},
	)
	result, err := scopedTool.Handler(
		contextWithHTTPSession(t, session.NewInMemoryStore(), "session-a"),
		toolRequest(map[string]interface{}{"workspaceId": "tea-unreachable"}),
	)

	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.False(t, handlerCalled)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "failed to validate workspace tea-unreachable")
}

func TestScopeToolsSurfacesResourceLookupFailures(t *testing.T) {
	tests := []struct {
		name            string
		arguments       map[string]interface{}
		resourceErr     error
		forbidden       map[string]bool
		expectedMessage string
	}{
		{
			name:            "forbidden service names the resource",
			arguments:       map[string]interface{}{"workspaceId": "tea-mine", "serviceId": "srv-foreign"},
			forbidden:       map[string]bool{"srv-foreign": true},
			expectedMessage: "service srv-foreign: forbidden",
		},
		{
			name:            "forbidden postgres names the resource",
			arguments:       map[string]interface{}{"workspaceId": "tea-mine", "postgresId": "dpg-foreign"},
			forbidden:       map[string]bool{"dpg-foreign": true},
			expectedMessage: "postgres dpg-foreign: forbidden",
		},
		{
			name:            "forbidden key value store names the resource",
			arguments:       map[string]interface{}{"workspaceId": "tea-mine", "keyValueId": "red-foreign"},
			forbidden:       map[string]bool{"red-foreign": true},
			expectedMessage: "key value store red-foreign: forbidden",
		},
		{
			name:            "transport error names the resource",
			arguments:       map[string]interface{}{"workspaceId": "tea-mine", "serviceId": "srv-flaky"},
			resourceErr:     assert.AnError,
			expectedMessage: "service srv-flaky",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ownerClient := &fakeOwnerClient{
				resourceErr: tt.resourceErr,
				forbidden:   tt.forbidden,
			}
			handlerCalled := false
			scopedTool := scopedTestTool(
				ownerClient,
				"workspace_sensitive_tool",
				func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					handlerCalled = true
					return mcp.NewToolResultText("ok"), nil
				},
			)
			result, err := scopedTool.Handler(
				contextWithHTTPSession(t, session.NewInMemoryStore(), "session-a"),
				toolRequest(tt.arguments),
			)

			require.NoError(t, err)
			require.True(t, result.IsError)
			assert.False(t, handlerCalled)
			assert.Contains(t, result.Content[0].(mcp.TextContent).Text, tt.expectedMessage)
		})
	}
}

func TestScopeToolsFallsBackToOwnerCheckWhenResourceMissing(t *testing.T) {
	const workspaceID = "tea-valid"

	tests := []struct {
		name      string
		parameter string
		resource  string
	}{
		{name: "service", parameter: "serviceId", resource: "srv-typo"},
		{name: "postgres", parameter: "postgresId", resource: "dpg-typo"},
		{name: "key value", parameter: "keyValueId", resource: "red-typo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ownerClient := &fakeOwnerClient{
				owners: map[string]*client.Owner{
					workspaceID: {Id: workspaceID, Name: "Valid workspace"},
				},
			}
			handlerCalled := false
			scopedTool := scopedTestTool(
				ownerClient,
				"workspace_sensitive_tool",
				func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					handlerCalled = true
					return mcp.NewToolResultText("ok"), nil
				},
			)
			result, err := scopedTool.Handler(
				contextWithHTTPSession(t, session.NewInMemoryStore(), "session-a"),
				toolRequest(map[string]interface{}{
					"workspaceId": workspaceID,
					tt.parameter:  tt.resource,
				}),
			)

			require.NoError(t, err)
			require.False(t, result.IsError, "a missing resource must fall back to the owner check, not fail in the resolver")
			assert.True(t, handlerCalled)
			assert.Equal(t, 1, ownerClient.totalResourceCalls())
			assert.Equal(t, 1, ownerClient.totalCalls())
		})
	}
}

func TestScopeToolsRejectsMissingMetricsResources(t *testing.T) {
	tests := []struct {
		name                  string
		resourceID            string
		expectedResourceCalls int
	}{
		{name: "known prefix", resourceID: "srv-missing", expectedResourceCalls: 1},
		{name: "unknown prefix exhausts all lookups", resourceID: "zzz-missing", expectedResourceCalls: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ownerClient := &fakeOwnerClient{}
			handlerCalled := false
			scopedTool := scopedTestTool(
				ownerClient,
				"get_metrics",
				func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					handlerCalled = true
					return mcp.NewToolResultText("read metrics"), nil
				},
			)
			result, err := scopedTool.Handler(
				contextWithHTTPSession(t, session.NewInMemoryStore(), "session-a"),
				toolRequest(map[string]interface{}{
					"workspaceId": "tea-valid",
					"resourceId":  tt.resourceID,
				}),
			)

			require.NoError(t, err)
			require.True(t, result.IsError)
			assert.False(t, handlerCalled)
			assert.Contains(t, result.Content[0].(mcp.TextContent).Text,
				"resource "+tt.resourceID+" was not found")
			assert.Equal(t, tt.expectedResourceCalls, ownerClient.totalResourceCalls())
		})
	}
}

func contextWithHTTPSession(t *testing.T, store session.Store, sessionID string) context.Context {
	t.Helper()

	ctx := (&server.MCPServer{}).WithContext(context.Background(), fakeMCPSession{sessionID: sessionID})
	return session.ContextWithHTTPSession(store)(ctx, nil)
}

func toolRequest(arguments map[string]interface{}) mcp.CallToolRequest {
	request := mcp.CallToolRequest{}
	request.Params.Arguments = arguments
	return request
}

func scopedTestTool(
	ownerClient *fakeOwnerClient,
	name string,
	handler server.ToolHandlerFunc,
) server.ServerTool {
	baseTool := server.ServerTool{
		Tool:    mcp.NewTool(name),
		Handler: handler,
	}
	return workspace.ScopeTools(
		workspace.NewResolver(ownerClient),
		workspace.AddWorkspaceIDParam(baseTool)...,
	)[0]
}

type fakeOwnerClient struct {
	mu            sync.Mutex
	owners        map[string]*client.Owner
	services      map[string]*client.Service
	postgres      map[string]*client.PostgresDetail
	keyValues     map[string]*client.KeyValueDetail
	ownerErr      error
	resourceErr   error
	forbidden     map[string]bool
	calls         int
	resourceCalls int
}

func (f *fakeOwnerClient) RetrieveOwnerWithResponse(
	_ context.Context,
	workspaceID string,
	_ ...client.RequestEditorFn,
) (*client.RetrieveOwnerResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++

	if f.ownerErr != nil {
		return nil, f.ownerErr
	}
	owner, ok := f.owners[workspaceID]
	if !ok {
		return &client.RetrieveOwnerResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusNotFound},
		}, nil
	}
	return &client.RetrieveOwnerResponse{
		JSON200:      owner,
		HTTPResponse: &http.Response{StatusCode: http.StatusOK},
	}, nil
}

func (f *fakeOwnerClient) RetrieveServiceWithResponse(
	_ context.Context,
	serviceID string,
	_ ...client.RequestEditorFn,
) (*client.RetrieveServiceResponse, error) {
	f.recordResourceCall()
	if f.resourceErr != nil {
		return nil, f.resourceErr
	}
	if f.forbidden[serviceID] {
		return &client.RetrieveServiceResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusForbidden},
		}, nil
	}
	service, ok := f.services[serviceID]
	if !ok {
		return &client.RetrieveServiceResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusNotFound},
		}, nil
	}
	return &client.RetrieveServiceResponse{
		JSON200:      service,
		HTTPResponse: &http.Response{StatusCode: http.StatusOK},
	}, nil
}

func (f *fakeOwnerClient) RetrievePostgresWithResponse(
	_ context.Context,
	postgresID string,
	_ ...client.RequestEditorFn,
) (*client.RetrievePostgresResponse, error) {
	f.recordResourceCall()
	if f.resourceErr != nil {
		return nil, f.resourceErr
	}
	if f.forbidden[postgresID] {
		return &client.RetrievePostgresResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusForbidden},
		}, nil
	}
	postgres, ok := f.postgres[postgresID]
	if !ok {
		return &client.RetrievePostgresResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusNotFound},
		}, nil
	}
	return &client.RetrievePostgresResponse{
		JSON200:      postgres,
		HTTPResponse: &http.Response{StatusCode: http.StatusOK},
	}, nil
}

func (f *fakeOwnerClient) RetrieveKeyValueWithResponse(
	_ context.Context,
	keyValueID string,
	_ ...client.RequestEditorFn,
) (*client.RetrieveKeyValueResponse, error) {
	f.recordResourceCall()
	if f.resourceErr != nil {
		return nil, f.resourceErr
	}
	if f.forbidden[keyValueID] {
		return &client.RetrieveKeyValueResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusForbidden},
		}, nil
	}
	keyValue, ok := f.keyValues[keyValueID]
	if !ok {
		return &client.RetrieveKeyValueResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusNotFound},
		}, nil
	}
	return &client.RetrieveKeyValueResponse{
		JSON200:      keyValue,
		HTTPResponse: &http.Response{StatusCode: http.StatusOK},
	}, nil
}

func (f *fakeOwnerClient) totalCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

func (f *fakeOwnerClient) recordResourceCall() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.resourceCalls++
}

func (f *fakeOwnerClient) totalResourceCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.resourceCalls
}

type fakeMCPSession struct {
	sessionID string
}

func (f fakeMCPSession) SessionID() string {
	return f.sessionID
}

func (fakeMCPSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return nil
}

func (fakeMCPSession) Initialize() {}

func (fakeMCPSession) Initialized() bool {
	return true
}
