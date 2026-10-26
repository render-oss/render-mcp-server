package blueprint

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	blueprintclient "github.com/render-oss/render-mcp-server/pkg/client/blueprints"
	"github.com/render-oss/render-mcp-server/pkg/config"
	"github.com/render-oss/render-mcp-server/pkg/fakes"
	"github.com/render-oss/render-mcp-server/pkg/session"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateBlueprintResourceModes(t *testing.T) {
	property := createBlueprint(nil).Tool.InputSchema.Properties["existingResources"].(map[string]any)
	modes := property["enum"].([]string)
	require.NotEmpty(t, modes)

	for _, mode := range modes {
		t.Run("accepts advertised mode "+mode, func(t *testing.T) {
			_, repo := newTestRepo()
			result := callCreateBlueprint(t, repo, map[string]any{
				"repoUrl":           "https://github.com/example/my-app",
				"existingResources": mode,
			})
			assert.False(t, result.IsError, textContent(t, result))
		})
	}
}

func TestValidateCreateBlueprintParams(t *testing.T) {
	t.Run("applies tool parameter defaults", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{"repoUrl": "https://github.com/example/my-app.git"}

		got, err := validateCreateBlueprintParams(request)

		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "my-app", got.name)
		assert.Equal(t, "render.yaml", got.path)
		assert.True(t, got.autoSync)
		assert.Equal(t, blueprintclient.CreateNew, got.existingResources)
	})

	t.Run("does not populate defaults determined by the REST API", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{"repoUrl": "https://github.com/example/my-app.git"}

		got, err := validateCreateBlueprintParams(request)

		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Nil(t, got.branch)
	})

	t.Run("converts an SSH repository URL to HTTPS", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{"repoUrl": "ssh://git@github.com/example/my-app.git"}

		got, err := validateCreateBlueprintParams(request)

		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "https://github.com/example/my-app.git", got.repoURL)
	})

	t.Run("preserves explicit parameter values", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"repoUrl":           "https://github.com/example/my-app.git",
			"name":              "preview",
			"branch":            "feature",
			"path":              "deploy/render.yaml",
			"autoSync":          false,
			"existingResources": "adopt",
		}

		got, err := validateCreateBlueprintParams(request)

		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "https://github.com/example/my-app.git", got.repoURL)
		assert.Equal(t, "preview", got.name)
		assert.Equal(t, new("feature"), got.branch)
		assert.Equal(t, "deploy/render.yaml", got.path)
		assert.False(t, got.autoSync)
		assert.Equal(t, blueprintclient.AdoptExisting, got.existingResources)
	})
}

func TestCreateBlueprintMapsToolInputsToRESTRequest(t *testing.T) {
	fakeClient, repo := newTestRepo()
	result := callCreateBlueprint(t, repo, map[string]any{
		"repoUrl":           "https://github.com/example/my-app.git",
		"name":              "preview",
		"branch":            "feature",
		"path":              "deploy/render.yaml",
		"autoSync":          false,
		"existingResources": "adopt",
	})
	require.False(t, result.IsError, textContent(t, result))
	require.Equal(t, 1, fakeClient.CreateBlueprintWithResponseCallCount())
	_, body, _ := fakeClient.CreateBlueprintWithResponseArgsForCall(0)
	assert.Equal(t, client.CreateBlueprintJSONRequestBody{
		OwnerId: "tea-test",
		Name:    "preview",
		Source: blueprintclient.BlueprintSource{
			Repo:   "https://github.com/example/my-app.git",
			Branch: new("feature"),
			Path:   new("deploy/render.yaml"),
		},
		AutoSync:          new(false),
		ExistingResources: new(blueprintclient.AdoptExisting),
	}, body)
}

func TestCreateBlueprintValidation(t *testing.T) {
	for _, tt := range []struct {
		name    string
		args    map[string]any
		message string
	}{
		{
			name:    "missing repository URL",
			args:    nil,
			message: "required parameter not present: repoUrl",
		},
		{
			name:    "non-string repository URL",
			args:    map[string]any{"repoUrl": 1},
			message: "parameter repoUrl is not of expected type",
		},
		{
			name:    "empty repository URL",
			args:    map[string]any{"repoUrl": ""},
			message: "repoUrl must be an HTTPS or SSH repository URL",
		},
		{
			name:    "scp style SSH remote",
			args:    map[string]any{"repoUrl": "git@github.com:example/my-app.git"},
			message: "repoUrl must be an HTTPS or SSH repository URL",
		},
		{
			name:    "empty name",
			args:    map[string]any{"name": ""},
			message: "name cannot be empty",
		},
		{
			name:    "whitespace-only name",
			args:    map[string]any{"name": " "},
			message: "name cannot be empty",
		},
		{
			name:    "empty branch",
			args:    map[string]any{"branch": ""},
			message: "branch cannot be empty",
		},
		{
			name:    "whitespace-only branch",
			args:    map[string]any{"branch": " "},
			message: "branch cannot be empty",
		},
		{
			name:    "empty path",
			args:    map[string]any{"path": ""},
			message: "path cannot be empty",
		},
		{
			name:    "whitespace-only path",
			args:    map[string]any{"path": " "},
			message: "path cannot be empty",
		},
		{
			name:    "non-boolean autoSync",
			args:    map[string]any{"autoSync": "false"},
			message: "parameter autoSync is not of expected type",
		},
		{
			name:    "empty resource mode",
			args:    map[string]any{"existingResources": ""},
			message: "existingResources must be adopt or create_new",
		},
		{
			name:    "unknown resource mode",
			args:    map[string]any{"existingResources": "replace"},
			message: "existingResources must be adopt or create_new",
		},
	} {
		t.Run("rejects "+tt.name+" before calling REST", func(t *testing.T) {
			if tt.args != nil {
				if _, ok := tt.args["repoUrl"]; !ok {
					tt.args["repoUrl"] = "https://github.com/example/my-app"
				}
			}
			fakeClient := &fakes.FakeBlueprintRepoClient{}
			result := callCreateBlueprint(t, NewRepo(fakeClient), tt.args)
			assert.True(t, result.IsError)
			assert.Contains(t, textContent(t, result), tt.message)
			assert.Zero(t, fakeClient.CreateBlueprintWithResponseCallCount())
		})
	}
}

func TestCreateBlueprintRequiresWorkspace(t *testing.T) {
	fakeClient := &fakes.FakeBlueprintRepoClient{}
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]any{"repoUrl": "https://github.com/example/my-app"}
	ctx := session.ContextWithWorkspace(t.Context(), "")
	result, err := createBlueprint(NewRepo(fakeClient)).Handler(ctx, request)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Equal(t, config.ErrNoWorkspace.Error(), textContent(t, result))
	assert.Zero(t, fakeClient.CreateBlueprintWithResponseCallCount())
}

func TestCreateBlueprintPreservesRESTPlanningResponse(t *testing.T) {
	for _, tt := range []struct {
		name            string
		planningOutcome blueprintclient.PlanningOutcome
		sync            *blueprintclient.SyncWithPlan
		notices         []blueprintclient.BlueprintNotice
	}{
		{
			name:            "returns the planned sync",
			planningOutcome: blueprintclient.PlanningOutcomePlanned,
			sync:            &blueprintclient.SyncWithPlan{Id: "exe-planned", State: blueprintclient.SyncStateCreated},
			notices:         []blueprintclient.BlueprintNotice{},
		},
		{
			name:            "returns the raw plan",
			planningOutcome: blueprintclient.PlanningOutcomePlanned,
			sync: &blueprintclient.SyncWithPlan{
				Id:    "exe-planned",
				State: blueprintclient.SyncStateCreated,
				RawPlan: &map[string]any{
					"actions": []any{map[string]any{
						"type":   "CreateService",
						"action": map[string]any{"name": "web-app"},
					}},
				},
			},
			notices: []blueprintclient.BlueprintNotice{},
		},
		{
			name:            "returns required manual environment variables",
			planningOutcome: blueprintclient.PlanningOutcomePlanned,
			sync: &blueprintclient.SyncWithPlan{
				Id:    "exe-planned",
				State: blueprintclient.SyncStateCreated,
				RequiredManualEnvVars: []blueprintclient.RequiredManualEnvVar{
					{ServiceType: blueprintclient.ManualEnvVarTargetTypeWeb, ServiceName: "web-app", Key: "API_KEY"},
				},
			},
			notices: []blueprintclient.BlueprintNotice{},
		},
		{
			name:            "returns a successful tool result for failed planning without a sync",
			planningOutcome: blueprintclient.PlanningOutcomeFailed,
			notices: []blueprintclient.BlueprintNotice{
				{
					Severity: blueprintclient.BlueprintNoticeSeverityError,
					Message:  "Planning timed out.",
				},
			},
		},
		{
			name:            "returns a successful tool result for failed planning with a sync",
			planningOutcome: blueprintclient.PlanningOutcomeFailed,
			sync:            &blueprintclient.SyncWithPlan{Id: "exe-failed", State: blueprintclient.SyncStateError},
			notices: []blueprintclient.BlueprintNotice{
				{
					Severity: blueprintclient.BlueprintNoticeSeverityError,
					Message:  "Invalid plan.",
				},
			},
		},
		{
			name:            "returns planning error locations",
			planningOutcome: blueprintclient.PlanningOutcomeFailed,
			notices: []blueprintclient.BlueprintNotice{
				{
					Severity: blueprintclient.BlueprintNoticeSeverityError,
					Message:  "Invalid plan.",
					Path:     new("services[0].plan"),
					Line:     new(3),
				},
			},
		},
		{
			name:            "returns no sync when there are no changes",
			planningOutcome: blueprintclient.PlanningOutcomeNoChanges,
			notices:         []blueprintclient.BlueprintNotice{},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response := &blueprintclient.CreateBlueprintResponse{
				Blueprint:       blueprintclient.Blueprint{Id: "exs-created"},
				PlanningOutcome: tt.planningOutcome,
				Sync:            tt.sync,
				Notices:         tt.notices,
			}
			fakeClient := &fakes.FakeBlueprintRepoClient{}
			fakeClient.CreateBlueprintWithResponseReturns(&client.CreateBlueprintResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusCreated},
				JSON201:      response,
			}, nil)

			result := callCreateBlueprint(t, NewRepo(fakeClient), map[string]any{"repoUrl": "https://github.com/example/my-app"})
			require.False(t, result.IsError)
			var got blueprintclient.CreateBlueprintResponse
			require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &got))
			assert.Equal(t, *response, got)
		})
	}
}

func TestCreateBlueprintClientErrors(t *testing.T) {
	for _, tt := range []struct {
		name     string
		response *client.CreateBlueprintResponse
		err      error
		message  string
	}{
		{
			name:    "reports transport failure as a tool error",
			err:     errors.New("connection lost"),
			message: "connection lost",
		},
		{
			name: "reports a missing success body as a tool error",
			response: &client.CreateBlueprintResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusCreated},
			},
			message: "unexpected empty body",
		},
		{
			name: "reports an adoption conflict as a tool error",
			response: &client.CreateBlueprintResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusConflict},
				Body:         []byte(`{"message":"Blueprint exs-existing already manages render.yaml on branch \"main\" in this repository"}`),
			},
			message: `Blueprint exs-existing already manages render.yaml on branch "main" in this repository`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeBlueprintRepoClient{}
			fakeClient.CreateBlueprintWithResponseReturns(tt.response, tt.err)
			result := callCreateBlueprint(t, NewRepo(fakeClient), map[string]any{"repoUrl": "https://github.com/example/my-app"})
			assert.True(t, result.IsError)
			assert.Contains(t, textContent(t, result), tt.message)
		})
	}
}

func newTestRepo() (*fakes.FakeBlueprintRepoClient, *Repo) {
	fakeClient := &fakes.FakeBlueprintRepoClient{}
	fakeClient.CreateBlueprintWithResponseReturns(&client.CreateBlueprintResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusCreated},
		JSON201: &blueprintclient.CreateBlueprintResponse{
			Blueprint:       blueprintclient.Blueprint{Id: "exs-created"},
			PlanningOutcome: blueprintclient.PlanningOutcomeNoChanges,
			Notices:         []blueprintclient.BlueprintNotice{},
		},
	}, nil)
	return fakeClient, NewRepo(fakeClient)
}

func callCreateBlueprint(t *testing.T, repo *Repo, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	request := mcp.CallToolRequest{}
	request.Params.Arguments = args
	result, err := createBlueprint(repo).Handler(session.ContextWithWorkspace(t.Context(), "tea-test"), request)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

func textContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, result.Content)
	content, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	return content.Text
}
