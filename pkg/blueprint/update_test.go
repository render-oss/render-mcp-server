package blueprint

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	blueprintclient "github.com/render-oss/render-mcp-server/pkg/client/blueprints"
	"github.com/render-oss/render-mcp-server/pkg/fakes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateBlueprintRequestValuesFaithfulToToolCallInputs(t *testing.T) {
	for _, tt := range []struct {
		name string
		args map[string]any
		want client.UpdateBlueprintJSONRequestBody
	}{
		{
			name: "name-only",
			args: map[string]any{"name": "renamed"},
			want: client.UpdateBlueprintJSONRequestBody{Name: new("renamed")},
		},
		{
			name: "explicit false for auto sync",
			args: map[string]any{"autoSync": false},
			want: client.UpdateBlueprintJSONRequestBody{AutoSync: new(false)},
		},
		{
			name: "branch-only",
			args: map[string]any{"branch": "preview"},
			want: client.UpdateBlueprintJSONRequestBody{Branch: new("preview")},
		},
		{
			name: "path-only",
			args: map[string]any{"path": "deploy/render.yaml"},
			want: client.UpdateBlueprintJSONRequestBody{Path: new("deploy/render.yaml")},
		},
		{
			name: "all parameters provided",
			args: map[string]any{"name": "renamed", "branch": "preview", "path": "deploy/render.yaml", "autoSync": true},
			want: client.UpdateBlueprintJSONRequestBody{Name: new("renamed"), Branch: new("preview"), Path: new("deploy/render.yaml"), AutoSync: new(true)},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeBlueprintRepoClient{}
			fakeClient.RetrieveBlueprintWithResponseReturns(&client.RetrieveBlueprintResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
				JSON200:      &blueprintclient.BlueprintDetail{Id: "exs-test", Branch: "main", Path: "render.yaml", AutoSync: true},
			}, nil)
			fakeClient.UpdateBlueprintWithResponseReturns(nil, assert.AnError)
			tt.args["blueprintId"] = "exs-test"

			callUpdateBlueprint(t, NewRepo(fakeClient), tt.args)

			require.Equal(t, 1, fakeClient.UpdateBlueprintWithResponseCallCount())
			_, id, body, _ := fakeClient.UpdateBlueprintWithResponseArgsForCall(0)
			assert.Equal(t, "exs-test", id)
			assert.Equal(t, tt.want, body)
		})
	}
}

func TestUpdateBlueprintResponseValuesFaithfulToClientResponse(t *testing.T) {
	fakeClient := &fakes.FakeBlueprintRepoClient{}
	fakeClient.RetrieveBlueprintWithResponseReturns(&client.RetrieveBlueprintResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusOK},
		JSON200:      &blueprintclient.BlueprintDetail{Id: "exs-test", Name: "old-name"},
	}, nil)
	want := blueprintclient.Blueprint{
		Id:       "exs-test",
		Name:     "renamed",
		Branch:   "main",
		Path:     "deploy/render.yaml",
		AutoSync: true,
		Repo:     "https://github.com/example/app",
		Status:   blueprintclient.StatusCreated,
		LastSync: new(time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)),
	}
	fakeClient.UpdateBlueprintWithResponseReturns(&client.UpdateBlueprintResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusOK}, JSON200: &want,
	}, nil)

	result := callUpdateBlueprint(t, NewRepo(fakeClient), map[string]any{"blueprintId": "exs-test", "name": "renamed"})

	require.False(t, result.IsError, textContent(t, result))
	var got updateBlueprintResponse
	require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &got))
	assert.Equal(t, want, got.Blueprint)
}

func TestUpdateBlueprintOutputFaithfulToPlanningResponse(t *testing.T) {
	for _, planning := range []blueprintclient.CreateBlueprintSyncResponse{
		{
			PlanningOutcome: blueprintclient.PlanningOutcomePlanned,
			Notices:         []blueprintclient.BlueprintNotice{},
			Sync: &blueprintclient.SyncWithPlan{
				Id: "exe-planned", State: blueprintclient.SyncStateCreated,
				DashboardUrl: "https://dashboard.render.com/blueprint/exs-test/sync/exe-planned",
				RawPlan:      &map[string]any{"actions": []any{"create service"}},
				RequiredManualEnvVars: []blueprintclient.RequiredManualEnvVar{
					{ServiceType: blueprintclient.ManualEnvVarTargetTypeWeb, ServiceName: "app", Key: "TOKEN"},
				},
			},
		},
		{
			PlanningOutcome: blueprintclient.PlanningOutcomeNoChanges,
			Notices:         []blueprintclient.BlueprintNotice{},
		},
		{
			PlanningOutcome: blueprintclient.PlanningOutcomeFailed,
			Notices: []blueprintclient.BlueprintNotice{{
				Severity: blueprintclient.BlueprintNoticeSeverityError, Message: "Invalid plan",
				Path: new("services[0].plan"), Line: new(3), Column: new(5),
			}},
		},
	} {
		t.Run(string(planning.PlanningOutcome), func(t *testing.T) {
			blueprint := blueprintclient.Blueprint{Id: "exs-test", Name: "my-app", Branch: "preview", Path: "render.yaml"}
			fakeClient := &fakes.FakeBlueprintRepoClient{}
			fakeClient.RetrieveBlueprintWithResponseReturns(&client.RetrieveBlueprintResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
				JSON200:      &blueprintclient.BlueprintDetail{Id: "exs-test", Branch: "main", Path: "render.yaml", AutoSync: true},
			}, nil)
			fakeClient.UpdateBlueprintWithResponseReturns(&client.UpdateBlueprintResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusOK}, JSON200: &blueprint,
			}, nil)
			fakeClient.CreateBlueprintSyncWithResponseReturns(&client.CreateBlueprintSyncResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusOK}, JSON200: &planning,
			}, nil)

			result := callUpdateBlueprint(t, NewRepo(fakeClient), map[string]any{"blueprintId": "exs-test", "branch": "preview"})

			require.False(t, result.IsError, textContent(t, result))
			var got updateBlueprintResponse
			require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &got))
			assert.Equal(t, &planning.PlanningOutcome, got.PlanningOutcome)
			assert.Equal(t, planning.Sync, got.Sync)
			assert.Equal(t, planning.Notices, got.Notices)
		})
	}
}

func TestUpdateBlueprintBadInputsRejectedBeforeAPI(t *testing.T) {
	for _, tt := range []struct {
		name    string
		args    map[string]any
		message string
	}{
		{
			name:    "invalid Blueprint ID",
			args:    map[string]any{"blueprintId": "exe-test", "name": "renamed"},
			message: "Blueprint ID starting with exs-",
		},
		{
			name:    "no settings",
			args:    map[string]any{"blueprintId": "exs-test"},
			message: "Provide at least one of name, branch, path, or autoSync.",
		},
		{
			name:    "blank name",
			args:    map[string]any{"blueprintId": "exs-test", "name": " "},
			message: "The name setting cannot be empty",
		},
		{
			name:    "blank branch",
			args:    map[string]any{"blueprintId": "exs-test", "branch": "\t"},
			message: "The branch setting cannot be empty",
		},
		{
			name:    "blank path",
			args:    map[string]any{"blueprintId": "exs-test", "path": ""},
			message: "The path setting cannot be empty",
		},
		{
			name:    "wrong string type",
			args:    map[string]any{"blueprintId": "exs-test", "name": false},
			message: "parameter name is not of expected type: string",
		},
		{
			name:    "wrong boolean type",
			args:    map[string]any{"blueprintId": "exs-test", "autoSync": "false"},
			message: "parameter autoSync is not of expected type: bool",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeBlueprintRepoClient{}
			result := callUpdateBlueprint(t, NewRepo(fakeClient), tt.args)
			assert.True(t, result.IsError)
			assert.Contains(t, textContent(t, result), tt.message)
			assert.Zero(t, fakeClient.RetrieveBlueprintWithResponseCallCount())
			assert.Zero(t, fakeClient.UpdateBlueprintWithResponseCallCount())
			assert.Zero(t, fakeClient.CreateBlueprintSyncWithResponseCallCount())
		})
	}
}

func TestUpdateBlueprintRequestErrors(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		err     error
		message string
	}{
		{name: "API rejection", status: http.StatusBadRequest, message: "branch does not exist"},
		{name: "server error", status: http.StatusInternalServerError, message: "internal server error"},
		{name: "timeout", err: context.DeadlineExceeded, message: "context deadline exceeded"},
		{name: "HTTP timeout", status: http.StatusRequestTimeout, message: "request timed out"},
		{name: "missing success body", status: http.StatusOK, message: "unexpected empty body"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeBlueprintRepoClient{}
			fakeClient.RetrieveBlueprintWithResponseReturns(&client.RetrieveBlueprintResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
				JSON200:      &blueprintclient.BlueprintDetail{Id: "exs-test", Branch: "main", Path: "render.yaml", AutoSync: true},
			}, nil)
			body, err := json.Marshal(map[string]string{"message": tt.message})
			require.NoError(t, err)
			fakeClient.UpdateBlueprintWithResponseReturns(&client.UpdateBlueprintResponse{
				HTTPResponse: &http.Response{StatusCode: tt.status}, Body: body,
			}, tt.err)

			result := callUpdateBlueprint(t, NewRepo(fakeClient), map[string]any{"blueprintId": "exs-test", "branch": "preview"})

			assert.True(t, result.IsError)
			message := textContent(t, result)
			assert.Contains(t, message, "The Blueprint update failed:")
			assert.Contains(t, message, tt.message)
			assert.Contains(t, message, "This call did not start planning")
			assert.Equal(t, 1, fakeClient.UpdateBlueprintWithResponseCallCount())
			assert.Zero(t, fakeClient.CreateBlueprintSyncWithResponseCallCount())
		})
	}
}

func TestUpdateBlueprintPlanningRequestErrors(t *testing.T) {
	for _, tt := range []struct {
		name     string
		response *client.CreateBlueprintSyncResponse
		err      error
		message  string
	}{
		{
			name: "active sync rejection",
			response: &client.CreateBlueprintSyncResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusConflict},
				Body:         []byte(`{"message":"Wait for sync exe-active to finish, then re-plan."}`),
			},
			message: "Wait for sync exe-active to finish, then re-plan.",
		},
		{
			name: "timeout", err: context.DeadlineExceeded,
			response: &client.CreateBlueprintSyncResponse{},
			message:  "context deadline exceeded",
		},
		{
			name: "missing success body",
			response: &client.CreateBlueprintSyncResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusCreated},
			},
			message: "unexpected empty body",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			blueprint := blueprintclient.Blueprint{Id: "exs-test", Branch: "preview"}
			fakeClient := &fakes.FakeBlueprintRepoClient{}
			fakeClient.RetrieveBlueprintWithResponseReturns(&client.RetrieveBlueprintResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
				JSON200:      &blueprintclient.BlueprintDetail{Id: "exs-test", Branch: "main", Path: "render.yaml", AutoSync: true},
			}, nil)
			fakeClient.UpdateBlueprintWithResponseReturns(&client.UpdateBlueprintResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusOK}, JSON200: &blueprint,
			}, nil)
			fakeClient.CreateBlueprintSyncWithResponseReturns(tt.response, tt.err)

			result := callUpdateBlueprint(t, NewRepo(fakeClient), map[string]any{"blueprintId": "exs-test", "branch": "preview"})

			require.False(t, result.IsError, textContent(t, result))
			var got updateBlueprintResponse
			require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &got))
			assert.Equal(t, blueprint, got.Blueprint)
			assert.Nil(t, got.PlanningOutcome)
			assert.Nil(t, got.Sync)
			assert.Equal(t, []blueprintclient.BlueprintNotice{}, got.Notices)
			require.NotNil(t, got.Error)
			assert.Equal(t, "planning", got.Error.Stage)
			assert.Contains(t, got.Error.Message, tt.message)
			assert.Contains(t, got.Error.Message, "Blueprint was updated")
			assert.Contains(t, got.Error.Message, "Do not approve")
			assert.Contains(t, got.Error.Message, "plan_blueprint")
			assert.Equal(t, 1, fakeClient.UpdateBlueprintWithResponseCallCount())
			assert.Equal(t, 1, fakeClient.CreateBlueprintSyncWithResponseCallCount())
		})
	}
}

func TestUpdateBlueprintPlansAfterUpdate(t *testing.T) {
	var calls []string
	fakeClient := &fakes.FakeBlueprintRepoClient{}
	fakeClient.RetrieveBlueprintWithResponseStub = func(context.Context, string, ...client.RequestEditorFn) (*client.RetrieveBlueprintResponse, error) {
		calls = append(calls, "retrieve")
		return &client.RetrieveBlueprintResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusOK},
			JSON200:      &blueprintclient.BlueprintDetail{Id: "exs-test", Branch: "main"},
		}, nil
	}
	fakeClient.UpdateBlueprintWithResponseStub = func(context.Context, string, client.UpdateBlueprintJSONRequestBody, ...client.RequestEditorFn) (*client.UpdateBlueprintResponse, error) {
		calls = append(calls, "update")
		return &client.UpdateBlueprintResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusOK},
			JSON200:      &blueprintclient.Blueprint{Id: "exs-test", Branch: "preview"},
		}, nil
	}
	fakeClient.CreateBlueprintSyncWithResponseStub = func(context.Context, string, ...client.RequestEditorFn) (*client.CreateBlueprintSyncResponse, error) {
		calls = append(calls, "plan")
		return nil, assert.AnError
	}

	callUpdateBlueprint(t, NewRepo(fakeClient), map[string]any{"blueprintId": "exs-test", "branch": "preview"})

	assert.Equal(t, []string{"retrieve", "update", "plan"}, calls)
}

func callUpdateBlueprint(t *testing.T, repo *Repo, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	request := mcp.CallToolRequest{}
	request.Params.Arguments = args
	result, err := updateBlueprint(repo).Handler(t.Context(), request)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

func TestUpdateBlueprintUnchanged(t *testing.T) {
	for _, tt := range []struct {
		name string
		args map[string]any
	}{
		{name: "name", args: map[string]any{"name": "my-app"}},
		{name: "branch", args: map[string]any{"branch": "main"}},
		{name: "normalized branch", args: map[string]any{"branch": " main\t"}},
		{name: "path", args: map[string]any{"path": "render.yaml"}},
		{name: "explicit false", args: map[string]any{"autoSync": false}},
		{name: "all settings", args: map[string]any{"name": "my-app", "branch": "main", "path": "render.yaml", "autoSync": false}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeBlueprintRepoClient{}
			fakeClient.RetrieveBlueprintWithResponseReturns(&client.RetrieveBlueprintResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
				JSON200:      &blueprintclient.BlueprintDetail{Id: "exs-test", Name: "my-app", Branch: "main", Path: "render.yaml", Repo: "https://github.com/example/app", Status: blueprintclient.StatusCreated},
			}, nil)
			tt.args["blueprintId"] = "exs-test"

			result := callUpdateBlueprint(t, NewRepo(fakeClient), tt.args)

			require.False(t, result.IsError)
			assert.JSONEq(t, `{"blueprint":{"id":"exs-test","name":"my-app","branch":"main","path":"render.yaml","repo":"https://github.com/example/app","status":"created","autoSync":false},"planningOutcome":null,"sync":null,"notices":[],"error":null}`, textContent(t, result))
			require.Len(t, result.Content, 2)
			assert.Equal(t, mcp.NewTextContent("The Blueprint already has the supplied settings. No update was needed, and planning was skipped."), result.Content[1])
			assert.Equal(t, 1, fakeClient.RetrieveBlueprintWithResponseCallCount())
			assert.Zero(t, fakeClient.UpdateBlueprintWithResponseCallCount())
			assert.Zero(t, fakeClient.CreateBlueprintSyncWithResponseCallCount())
		})
	}
}

func TestUpdateBlueprintChangesThatDoNotCausePlanning(t *testing.T) {
	t.Run("name change", func(t *testing.T) {
		fakeClient := &fakes.FakeBlueprintRepoClient{}
		fakeClient.RetrieveBlueprintWithResponseReturns(&client.RetrieveBlueprintResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusOK},
			JSON200:      &blueprintclient.BlueprintDetail{Id: "exs-test", Name: "old-name", Branch: "main", Path: "render.yaml"},
		}, nil)
		blueprint := &blueprintclient.Blueprint{Id: "exs-test", Name: "new-name", Branch: "main", Path: "render.yaml"}
		fakeClient.UpdateBlueprintWithResponseReturns(&client.UpdateBlueprintResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusOK}, JSON200: blueprint,
		}, nil)

		result := callUpdateBlueprint(t, NewRepo(fakeClient), map[string]any{
			"blueprintId": "exs-test", "name": "new-name", "branch": " main ", "path": "render.yaml",
		})

		require.False(t, result.IsError)
		require.Len(t, result.Content, 1)
		var got updateBlueprintResponse
		require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &got))
		assert.Equal(t, *blueprint, got.Blueprint)
		assert.Nil(t, got.PlanningOutcome)
		assert.Nil(t, got.Error)
		require.Equal(t, 1, fakeClient.UpdateBlueprintWithResponseCallCount())
		_, _, body, _ := fakeClient.UpdateBlueprintWithResponseArgsForCall(0)
		assert.Equal(t, client.UpdateBlueprintJSONRequestBody{Name: new("new-name"), Branch: new(" main "), Path: new("render.yaml")}, body)
		assert.Zero(t, fakeClient.CreateBlueprintSyncWithResponseCallCount())
	})

	t.Run("auto-sync change", func(t *testing.T) {
		fakeClient := &fakes.FakeBlueprintRepoClient{}
		fakeClient.RetrieveBlueprintWithResponseReturns(&client.RetrieveBlueprintResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusOK},
			JSON200:      &blueprintclient.BlueprintDetail{Id: "exs-test", Branch: "main", Path: "render.yaml", AutoSync: true},
		}, nil)
		fakeClient.UpdateBlueprintWithResponseReturns(&client.UpdateBlueprintResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusOK},
			JSON200:      &blueprintclient.Blueprint{Id: "exs-test", Branch: "main", Path: "render.yaml", AutoSync: false},
		}, nil)

		result := callUpdateBlueprint(t, NewRepo(fakeClient), map[string]any{
			"blueprintId": "exs-test", "autoSync": false,
		})

		require.False(t, result.IsError, textContent(t, result))
		assert.Equal(t, 1, fakeClient.UpdateBlueprintWithResponseCallCount())
		assert.Zero(t, fakeClient.CreateBlueprintSyncWithResponseCallCount())
	})
}

func TestUpdateBlueprintReadErrors(t *testing.T) {
	for _, tt := range []struct {
		name     string
		response *client.RetrieveBlueprintResponse
		err      error
		message  string
	}{
		{
			name:     "API rejection",
			response: &client.RetrieveBlueprintResponse{HTTPResponse: &http.Response{StatusCode: http.StatusNotFound}, Body: []byte(`{"message":"not found"}`)},
			message:  "not found",
		},
		{
			name:    "timeout",
			err:     context.DeadlineExceeded,
			message: "context deadline exceeded",
		},
		{
			name:     "missing success body",
			response: &client.RetrieveBlueprintResponse{HTTPResponse: &http.Response{StatusCode: http.StatusOK}},
			message:  "unexpected empty body",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeBlueprintRepoClient{}
			fakeClient.RetrieveBlueprintWithResponseReturns(tt.response, tt.err)

			result := callUpdateBlueprint(t, NewRepo(fakeClient), map[string]any{"blueprintId": "exs-test", "branch": "preview"})

			assert.True(t, result.IsError)
			assert.Contains(t, textContent(t, result), tt.message)
			assert.Contains(t, textContent(t, result), "This call did not update it or start planning")
			assert.Zero(t, fakeClient.UpdateBlueprintWithResponseCallCount())
			assert.Zero(t, fakeClient.CreateBlueprintSyncWithResponseCallCount())
		})
	}
}
