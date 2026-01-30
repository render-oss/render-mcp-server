package postgres

import (
	"context"
	"net/http"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	pgclient "github.com/render-oss/render-mcp-server/pkg/client/postgres"
	"github.com/render-oss/render-mcp-server/pkg/fakes"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreatePostgresToolSchemaDefault(t *testing.T) {
	fakeClient := &fakes.FakePostgresRepoClient{}
	repo := NewRepo(fakeClient)
	tool := createPostgres(repo)

	planProp := tool.Tool.InputSchema.Properties["plan"].(map[string]any)
	assert.Equal(t, "free", planProp["default"])
}

func TestCreatePostgresTool(t *testing.T) {
	ownerId := "own-123456"
	dbName := "test-database"

	tests := []struct {
		name         string
		plan         *string
		expectedPlan pgclient.PostgresPlans
	}{
		{
			name:         "Create postgres with no plan defaults to free",
			plan:         nil,
			expectedPlan: pgclient.Free,
		},
		{
			name:         "Create postgres with free plan",
			plan:         pointers.From("free"),
			expectedPlan: pgclient.Free,
		},
		{
			name:         "Create postgres with basic_256mb plan",
			plan:         pointers.From("basic_256mb"),
			expectedPlan: pgclient.Basic256mb,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakePostgresRepoClient{}
			repo := NewRepo(fakeClient)

			fakeClient.CreatePostgresWithResponseReturns(&client.CreatePostgresResponse{
				JSON201: &client.PostgresDetail{
					Id:   "pg-123",
					Name: dbName,
				},
				HTTPResponse: &http.Response{
					StatusCode: 201,
				},
			}, nil)

			ctx := createTestContext(ownerId)

			args := map[string]any{
				"name": dbName,
			}
			if tt.plan != nil {
				args["plan"] = *tt.plan
			}
			request := mcp.CallToolRequest{}
			request.Params.Arguments = args

			tool := createPostgres(repo)
			result, err := tool.Handler(ctx, request)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.False(t, result.IsError, "expected no error but got: %v", result.Content)

			assert.Equal(t, 1, fakeClient.CreatePostgresWithResponseCallCount())
			_, requestBody, _ := fakeClient.CreatePostgresWithResponseArgsForCall(0)
			assert.Equal(t, dbName, requestBody.Name)
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
