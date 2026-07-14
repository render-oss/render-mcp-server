package deploy

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/fakes"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTriggerDeployTool(t *testing.T) {
	ownerId := "own-123456"
	serviceId := "srv-123456"
	deployId := "dep-123456"

	tests := []struct {
		name               string
		args               map[string]any
		workspace          string
		expectedClearCache client.CreateDeployJSONBodyClearCache
	}{
		{
			name:               "Trigger deploy with default clear cache",
			args:               map[string]any{"serviceId": serviceId},
			workspace:          ownerId,
			expectedClearCache: client.DoNotClear,
		},
		{
			name:               "Trigger deploy with clear cache",
			args:               map[string]any{"serviceId": serviceId, "clearCache": true},
			workspace:          ownerId,
			expectedClearCache: client.Clear,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeDeployRepoClient{}
			repo := NewRepo(fakeClient)

			fakeClient.RetrieveServiceWithResponseReturns(&client.RetrieveServiceResponse{
				JSON200: &client.Service{Id: serviceId, OwnerId: ownerId},
				HTTPResponse: &http.Response{
					StatusCode: 200,
				},
			}, nil)

			fakeClient.CreateDeployWithResponseReturns(&client.CreateDeployResponse{
				JSON201: &client.Deploy{Id: deployId},
				HTTPResponse: &http.Response{
					StatusCode: 201,
				},
			}, nil)

			ctx := createTestContext(t, tt.workspace)

			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.args

			tool := triggerDeploy(repo)
			result, err := tool.Handler(ctx, request)

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.False(t, result.IsError)
			assert.Contains(t, textContent(t, result), deployId)

			require.Equal(t, 1, fakeClient.CreateDeployWithResponseCallCount())
			_, calledServiceId, body, _ := fakeClient.CreateDeployWithResponseArgsForCall(0)
			assert.Equal(t, serviceId, calledServiceId)
			require.NotNil(t, body.ClearCache)
			assert.Equal(t, tt.expectedClearCache, *body.ClearCache)
		})
	}
}

func TestTriggerDeployToolWorkspaceMismatch(t *testing.T) {
	fakeClient := &fakes.FakeDeployRepoClient{}
	repo := NewRepo(fakeClient)

	fakeClient.RetrieveServiceWithResponseReturns(&client.RetrieveServiceResponse{
		JSON200: &client.Service{Id: "srv-123456", OwnerId: "own-123456"},
		HTTPResponse: &http.Response{
			StatusCode: 200,
		},
	}, nil)

	ctx := createTestContext(t, "own-other")

	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]any{"serviceId": "srv-123456"}

	tool := triggerDeploy(repo)
	result, err := tool.Handler(ctx, request)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
	assert.Equal(t, 0, fakeClient.CreateDeployWithResponseCallCount())
}

func TestTriggerDeployToolAcceptedWithoutDeploy(t *testing.T) {
	fakeClient := &fakes.FakeDeployRepoClient{}
	repo := NewRepo(fakeClient)

	fakeClient.RetrieveServiceWithResponseReturns(&client.RetrieveServiceResponse{
		JSON200: &client.Service{Id: "srv-123456", OwnerId: "own-123456"},
		HTTPResponse: &http.Response{
			StatusCode: 200,
		},
	}, nil)

	// The API responds 202 with an empty body when it accepts the deploy
	// request without synchronously creating a deploy.
	fakeClient.CreateDeployWithResponseReturns(&client.CreateDeployResponse{
		HTTPResponse: &http.Response{
			StatusCode: 202,
		},
	}, nil)

	ctx := createTestContext(t, "own-123456")

	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]any{"serviceId": "srv-123456"}

	tool := triggerDeploy(repo)
	result, err := tool.Handler(ctx, request)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)
	assert.Contains(t, textContent(t, result), "accepted")
	assert.NotContains(t, textContent(t, result), "null")
}

func TestTriggerDeployToolServiceNotFound(t *testing.T) {
	fakeClient := &fakes.FakeDeployRepoClient{}
	repo := NewRepo(fakeClient)

	fakeClient.RetrieveServiceWithResponseReturns(&client.RetrieveServiceResponse{
		HTTPResponse: &http.Response{
			StatusCode: 404,
		},
		Body: []byte(`{"message":"service not found"}`),
	}, nil)

	ctx := createTestContext(t, "own-123456")

	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]any{"serviceId": "srv-123456"}

	tool := triggerDeploy(repo)
	result, err := tool.Handler(ctx, request)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
	assert.Contains(t, textContent(t, result), "service not found")
	assert.Equal(t, 0, fakeClient.CreateDeployWithResponseCallCount())
}

func TestTriggerDeployToolMissingServiceId(t *testing.T) {
	fakeClient := &fakes.FakeDeployRepoClient{}
	repo := NewRepo(fakeClient)

	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]any{}

	tool := triggerDeploy(repo)
	result, err := tool.Handler(context.Background(), request)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
	assert.Equal(t, 0, fakeClient.CreateDeployWithResponseCallCount())
}

func textContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, result.Content)
	content, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	return content.Text
}

func createTestContext(t *testing.T, workspaceID string) context.Context {
	t.Helper()
	t.Setenv("RENDER_CONFIG_PATH", filepath.Join(t.TempDir(), "mcp-server.yaml"))
	ctx := session.ContextWithStdioSession(context.Background())
	sess := session.FromContext(ctx)
	sess.SetWorkspace(ctx, workspaceID)
	return ctx
}
