package owner

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectWorkspaceValidatesAccessBeforePersistingSelection(t *testing.T) {
	tests := []struct {
		name        string
		workspaceID string
		owner       *client.Owner
	}{
		{
			name:        "accessible workspace",
			workspaceID: "tea-accessible",
			owner:       &client.Owner{Id: "tea-accessible", Name: "Workspace"},
		},
		{
			name:        "inaccessible workspace",
			workspaceID: "tea-inaccessible",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ownerTestContext(t)
			fakeClient := &fakeOwnerRepoClient{owner: tt.owner}

			request := mcp.CallToolRequest{}
			request.Params.Arguments = map[string]interface{}{"ownerID": tt.workspaceID}
			tool := selectWorkspace(NewRepo(fakeClient))
			result, err := tool.Handler(ctx, request)

			require.NoError(t, err)
			assert.Equal(t, tt.owner == nil, result.IsError)
			assert.Equal(t, 1, fakeClient.retrieveCalls)
			assert.Equal(t, tt.workspaceID, fakeClient.requestedWorkspaceID)

			workspaceID, workspaceErr := session.FromContext(ctx).GetWorkspace(ctx)
			if tt.owner != nil {
				require.NoError(t, workspaceErr)
				assert.Equal(t, tt.workspaceID, workspaceID)
				assert.Equal(t, "Workspace "+tt.workspaceID+" selected", resultText(t, result))
			} else {
				require.Error(t, workspaceErr)
			}
		})
	}
}

func TestSelectWorkspaceIsMarkedDeprecated(t *testing.T) {
	tool := selectWorkspace(NewRepo(&fakeOwnerRepoClient{}))
	assert.Contains(t, tool.Tool.Description, "scheduled for removal")
}

func TestListWorkspacesReturnsValidJSONWhenItAutoSelectsOneWorkspace(t *testing.T) {
	ctx := ownerTestContext(t)
	fakeClient := &fakeOwnerRepoClient{
		owners: []*client.Owner{
			{Id: "tea-only", Name: "Only workspace"},
		},
	}

	tool := listWorkspaces(NewRepo(fakeClient))
	result, err := tool.Handler(ctx, mcp.CallToolRequest{})

	require.NoError(t, err)
	require.False(t, result.IsError)

	var fallbackWorkspaces []*client.Owner
	require.NoError(t, json.Unmarshal([]byte(resultText(t, result)), &fallbackWorkspaces))
	require.Len(t, fallbackWorkspaces, 1)
	assert.Equal(t, "tea-only", fallbackWorkspaces[0].Id)

	selectedWorkspace, err := session.FromContext(ctx).GetWorkspace(ctx)
	require.NoError(t, err)
	assert.Equal(t, "tea-only", selectedWorkspace)
}

func TestGetSelectedWorkspaceReturnsSessionFallback(t *testing.T) {
	ctx := ownerTestContext(t)
	require.NoError(t, session.FromContext(ctx).SetWorkspace(ctx, "tea-selected"))

	tool := getSelectedWorkspace()
	result, err := tool.Handler(ctx, mcp.CallToolRequest{})

	require.NoError(t, err)
	require.False(t, result.IsError)
	assert.Contains(t, tool.Tool.Description, "session compatibility fallback")
	assert.Contains(t, tool.Tool.Description, "explicit workspaceId")
	assert.Equal(t, "The currently selected workspace is: tea-selected", resultText(t, result))
}

func ownerTestContext(t *testing.T) context.Context {
	t.Helper()
	t.Setenv("RENDER_CONFIG_PATH", filepath.Join(t.TempDir(), "mcp-server.yaml"))
	return session.ContextWithStdioSession(context.Background())
}

func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.Len(t, result.Content, 1)
	content, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	return content.Text
}

type fakeOwnerRepoClient struct {
	owner                *client.Owner
	owners               []*client.Owner
	retrieveCalls        int
	requestedWorkspaceID string
}

func (f *fakeOwnerRepoClient) ListOwnersWithResponse(
	context.Context,
	*client.ListOwnersParams,
	...client.RequestEditorFn,
) (*client.ListOwnersResponse, error) {
	owners := make([]client.OwnerWithCursor, 0, len(f.owners))
	for _, owner := range f.owners {
		owners = append(owners, client.OwnerWithCursor{Owner: owner})
	}
	return &client.ListOwnersResponse{
		JSON200:      &owners,
		HTTPResponse: &http.Response{StatusCode: http.StatusOK},
	}, nil
}

func (f *fakeOwnerRepoClient) RetrieveOwnerWithResponse(
	_ context.Context,
	workspaceID string,
	_ ...client.RequestEditorFn,
) (*client.RetrieveOwnerResponse, error) {
	f.retrieveCalls++
	f.requestedWorkspaceID = workspaceID
	if f.owner == nil {
		return &client.RetrieveOwnerResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusNotFound},
		}, nil
	}
	return &client.RetrieveOwnerResponse{
		JSON200:      f.owner,
		HTTPResponse: &http.Response{StatusCode: http.StatusOK},
	}, nil
}
