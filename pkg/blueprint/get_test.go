package blueprint

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	blueprintclient "github.com/render-oss/render-mcp-server/pkg/client/blueprints"
	"github.com/render-oss/render-mcp-server/pkg/fakes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBlueprintRequest(t *testing.T) {
	fakeClient := &fakes.FakeBlueprintRepoClient{}
	fakeClient.RetrieveBlueprintWithResponseReturns(&client.RetrieveBlueprintResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusOK},
		JSON200: &blueprintclient.BlueprintDetail{
			Id:        "exs-test",
			Name:      "my-app",
			Repo:      "https://github.com/example/my-app",
			Branch:    "main",
			Path:      "render.yaml",
			AutoSync:  true,
			Status:    blueprintclient.StatusCreated,
			Resources: []blueprintclient.ResourceRef{},
		},
	}, nil)

	result := callGetBlueprint(t, NewRepo(fakeClient), map[string]any{"blueprintId": " \texs-test\n"})

	require.False(t, result.IsError, textContent(t, result))
	require.Equal(t, 1, fakeClient.RetrieveBlueprintWithResponseCallCount())
	_, blueprintID, _ := fakeClient.RetrieveBlueprintWithResponseArgsForCall(0)
	assert.Equal(t, "exs-test", blueprintID)
}

func TestGetBlueprintResponse(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
	}{
		{
			name: "preserves details before the first sync is approved",
			body: `{
				"id": "exs-test",
				"name": "my-app",
				"repo": "https://github.com/example/my-app",
				"branch": "main",
				"path": "render.yaml",
				"autoSync": true,
				"status": "created",
				"resources": []
			}`,
		},
		{
			name: "preserves Blueprint settings, status, and managed resources",
			body: `{
				"id": "exs-test",
				"name": "my-app",
				"repo": "https://github.com/example/my-app",
				"branch": "release",
				"path": "deploy/render.yaml",
				"autoSync": false,
				"status": "paused",
				"lastSync": "2026-10-28T20:00:00Z",
				"resources": [
					{"id": "srv-web", "name": "web", "type": "web_service"},
					{"id": "dpg-db", "name": "db", "type": "postgres"}
				]
			}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var response blueprintclient.BlueprintDetail
			require.NoError(t, json.Unmarshal([]byte(tt.body), &response))
			fakeClient := &fakes.FakeBlueprintRepoClient{}
			fakeClient.RetrieveBlueprintWithResponseReturns(&client.RetrieveBlueprintResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
				JSON200:      &response,
			}, nil)

			result := callGetBlueprint(t, NewRepo(fakeClient), map[string]any{"blueprintId": "exs-test"})

			require.False(t, result.IsError, textContent(t, result))
			assert.JSONEq(t, tt.body, textContent(t, result))
		})
	}
}

func TestGetBlueprintIDValidation(t *testing.T) {
	fakeClient := &fakes.FakeBlueprintRepoClient{}

	result := callGetBlueprint(t, NewRepo(fakeClient), map[string]any{"blueprintId": "exe-test"})

	assert.True(t, result.IsError)
	assert.Contains(t, textContent(t, result), "Blueprint ID starting with exs-")
	assert.Zero(t, fakeClient.RetrieveBlueprintWithResponseCallCount())
}

func TestGetBlueprintClientErrors(t *testing.T) {
	for _, tt := range []struct {
		name     string
		response *client.RetrieveBlueprintResponse
		err      error
		message  string
	}{
		{
			name: "preserves the REST error message when the Blueprint is not found",
			response: &client.RetrieveBlueprintResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusNotFound},
				Body:         []byte(`{"message":"Blueprint exs-test not found."}`),
			},
			message: "Blueprint exs-test not found.",
		},
		{
			name: "missing success body is a tool error",
			response: &client.RetrieveBlueprintResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
			},
			message: "unexpected empty body",
		},
		{
			name:    "transport failure is a tool error",
			err:     assert.AnError,
			message: assert.AnError.Error(),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeBlueprintRepoClient{}
			fakeClient.RetrieveBlueprintWithResponseReturns(tt.response, tt.err)

			result := callGetBlueprint(t, NewRepo(fakeClient), map[string]any{"blueprintId": "exs-test"})

			assert.True(t, result.IsError)
			assert.Contains(t, textContent(t, result), tt.message)
		})
	}
}

func callGetBlueprint(t *testing.T, repo *Repo, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	request := mcp.CallToolRequest{}
	request.Params.Arguments = args
	result, err := getBlueprint(repo).Handler(t.Context(), request)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}
