package keyvalue

import (
	"context"
	"net/http"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/fakes"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateKeyValueToolSchemaDefault(t *testing.T) {
	fakeClient := &fakes.FakeKeyValueRepoClient{}
	repo := NewRepo(fakeClient)
	tool := createKeyValue(repo)

	planProp := tool.Tool.InputSchema.Properties["plan"].(map[string]any)
	assert.Equal(t, "free", planProp["default"])
}

func TestCreateKeyValueTool(t *testing.T) {
	ownerId := "own-123456"
	kvName := "test-keyvalue"

	tests := []struct {
		name         string
		plan         *string
		expectedPlan client.KeyValuePlan
	}{
		{
			name:         "Create key value with no plan defaults to free",
			plan:         nil,
			expectedPlan: client.KeyValuePlanFree,
		},
		{
			name:         "Create key value with free plan",
			plan:         pointers.From("free"),
			expectedPlan: client.KeyValuePlanFree,
		},
		{
			name:         "Create key value with starter plan",
			plan:         pointers.From("starter"),
			expectedPlan: client.KeyValuePlanStarter,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeKeyValueRepoClient{}
			repo := NewRepo(fakeClient)

			fakeClient.CreateKeyValueWithResponseReturns(&client.CreateKeyValueResponse{
				JSON201: &client.KeyValueDetail{
					Id:   "kv-123",
					Name: kvName,
				},
				HTTPResponse: &http.Response{
					StatusCode: 201,
				},
			}, nil)

			ctx := createTestContext(ownerId)

			args := map[string]any{
				"name": kvName,
			}
			if tt.plan != nil {
				args["plan"] = *tt.plan
			}
			request := mcp.CallToolRequest{}
			request.Params.Arguments = args

			tool := createKeyValue(repo)
			result, err := tool.Handler(ctx, request)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.False(t, result.IsError, "expected no error but got: %v", result.Content)

			assert.Equal(t, 1, fakeClient.CreateKeyValueWithResponseCallCount())
			_, requestBody, _ := fakeClient.CreateKeyValueWithResponseArgsForCall(0)
			assert.Equal(t, kvName, requestBody.Name)
			assert.Equal(t, ownerId, requestBody.OwnerId)
			assert.Equal(t, tt.expectedPlan, requestBody.Plan)
		})
	}
}

func createTestContext(workspaceID string) context.Context {
	ctx := session.ContextWithStdioSession(context.Background())
	sess := session.FromContext(ctx)
	sess.SetWorkspace(ctx, workspaceID)
	return ctx
}
