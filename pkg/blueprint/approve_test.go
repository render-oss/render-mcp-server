package blueprint

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	blueprintclient "github.com/render-oss/render-mcp-server/pkg/client/blueprints"
	"github.com/render-oss/render-mcp-server/pkg/fakes"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApproveBlueprintAcceptsAdvertisedServiceTypes(t *testing.T) {
	items := approveBlueprint(nil).Tool.InputSchema.Properties["manualEnvVars"].(map[string]any)["items"].(map[string]any)
	properties := items["properties"].(map[string]any)
	serviceTypes := properties["serviceType"].(map[string]any)["enum"].([]string)
	require.NotEmpty(t, serviceTypes)

	for _, serviceType := range serviceTypes {
		t.Run("tool call accepts "+serviceType, func(t *testing.T) {
			fakeClient := &fakes.FakeBlueprintRepoClient{}
			fakeClient.ApproveBlueprintWithResponseReturns(&client.ApproveBlueprintResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
				JSON200:      &blueprintclient.ApprovedSync{},
			}, nil)
			result := callApproveBlueprint(t, NewRepo(fakeClient), map[string]any{
				"blueprintId": "exs-test",
				"manualEnvVars": []any{map[string]any{
					"serviceType": serviceType, "serviceName": "app", "key": "TOKEN", "value": "secret",
				}},
			})
			assert.False(t, result.IsError, textContent(t, result))
		})
	}
}

func TestValidateApproveBlueprintParams(t *testing.T) {
	t.Run("omitted manual environment variables remain omitted", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{"blueprintId": "exs-test"}

		_, body, err := validateApproveBlueprintParams(request)

		require.NoError(t, err)
		assert.Nil(t, body.ManualEnvVars)
	})

	t.Run("explicit empty manual environment variable list remains empty", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"blueprintId":   "exs-test",
			"manualEnvVars": []any{},
		}

		_, body, err := validateApproveBlueprintParams(request)

		require.NoError(t, err)
		require.NotNil(t, body.ManualEnvVars)
		assert.Equal(t, []blueprintclient.SuppliedManualEnvVar{}, *body.ManualEnvVars)
	})

	t.Run("preserves whitespace and special characters in supplied values", func(t *testing.T) {
		value := " secret\n\"雪\" "
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"blueprintId": "exs-test",
			"manualEnvVars": []any{map[string]any{
				"serviceType": "web",
				"serviceName": "app",
				"key":         "TOKEN",
				"value":       value,
			}},
		}

		_, body, err := validateApproveBlueprintParams(request)

		require.NoError(t, err)
		require.NotNil(t, body.ManualEnvVars)
		require.Len(t, *body.ManualEnvVars, 1)
		assert.Equal(t, new(value), (*body.ManualEnvVars)[0].Value)
	})

	t.Run("preserves an explicitly empty supplied value", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"blueprintId": "exs-test",
			"manualEnvVars": []any{map[string]any{
				"serviceType": "web",
				"serviceName": "app",
				"key":         "TOKEN",
				"value":       "",
			}},
		}

		_, body, err := validateApproveBlueprintParams(request)

		require.NoError(t, err)
		require.NotNil(t, body.ManualEnvVars)
		require.Len(t, *body.ManualEnvVars, 1)
		assert.Equal(t, new(""), (*body.ManualEnvVars)[0].Value)
	})

	t.Run("accepts the same name and key for different service types", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"blueprintId": "exs-test",
			"manualEnvVars": []any{
				map[string]any{
					"serviceType": "web",
					"serviceName": "app",
					"key":         "TOKEN",
					"value":       "web-secret",
				},
				map[string]any{
					"serviceType": "worker",
					"serviceName": "app",
					"key":         "TOKEN",
					"value":       "worker-secret",
				},
			},
		}

		_, body, err := validateApproveBlueprintParams(request)

		require.NoError(t, err)
		require.NotNil(t, body.ManualEnvVars)
		assert.Len(t, *body.ManualEnvVars, 2)
	})
}

func TestApproveBlueprintMapsToolInputsToRESTRequest(t *testing.T) {
	fakeClient := &fakes.FakeBlueprintRepoClient{}
	fakeClient.ApproveBlueprintWithResponseReturns(&client.ApproveBlueprintResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusOK},
		JSON200:      &blueprintclient.ApprovedSync{},
	}, nil)
	result := callApproveBlueprint(t, NewRepo(fakeClient), map[string]any{
		"blueprintId": " \texs-test\n\u00a0",
		"manualEnvVars": []any{
			map[string]any{
				"serviceType": "web",
				"serviceName": "app",
				"key":         "TOKEN",
				"value":       "web-secret",
			},
			map[string]any{
				"serviceType": "worker",
				"serviceName": "jobs",
				"key":         "API_KEY",
				"value":       "worker-secret",
			},
		},
	})

	require.False(t, result.IsError, textContent(t, result))
	require.Equal(t, 1, fakeClient.ApproveBlueprintWithResponseCallCount())
	_, id, body, _ := fakeClient.ApproveBlueprintWithResponseArgsForCall(0)
	assert.Equal(t, "exs-test", id)
	assert.Equal(t, client.ApproveBlueprintJSONRequestBody{
		ManualEnvVars: new([]blueprintclient.SuppliedManualEnvVar{
			{
				ServiceType: blueprintclient.ManualEnvVarTargetTypeWeb,
				ServiceName: "app",
				Key:         "TOKEN",
				Value:       new("web-secret"),
			},
			{
				ServiceType: blueprintclient.ManualEnvVarTargetTypeWorker,
				ServiceName: "jobs",
				Key:         "API_KEY",
				Value:       new("worker-secret"),
			},
		}),
	}, body)
}

func TestApproveBlueprintPreservesRESTApprovalResponse(t *testing.T) {
	response := &blueprintclient.ApprovedSync{
		Id:           "exe-queued",
		State:        blueprintclient.SyncStatePending,
		Commit:       blueprintclient.CommitRef{Id: "commit-123"},
		DashboardUrl: "https://dashboard.render.com/blueprint/exs-test/sync/exe-queued",
	}
	fakeClient := &fakes.FakeBlueprintRepoClient{}
	fakeClient.ApproveBlueprintWithResponseReturns(&client.ApproveBlueprintResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusOK},
		JSON200:      response,
	}, nil)
	result := callApproveBlueprint(t, NewRepo(fakeClient), map[string]any{
		"blueprintId": "exs-test",
	})

	require.False(t, result.IsError, textContent(t, result))
	var got blueprintclient.ApprovedSync
	require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &got))
	assert.Equal(t, *response, got)
}

func TestApproveBlueprintIDValidation(t *testing.T) {
	for _, tt := range []struct {
		name    string
		args    map[string]any
		message string
	}{
		{
			name:    "missing Blueprint ID",
			args:    nil,
			message: "required parameter not present: blueprintId",
		},
		{
			name:    "sync ID",
			args:    map[string]any{"blueprintId": "exe-test"},
			message: "Blueprint ID starting with exs-",
		},
		{
			name:    "empty Blueprint ID",
			args:    map[string]any{"blueprintId": ""},
			message: "Blueprint ID starting with exs-",
		},
	} {
		t.Run("rejects "+tt.name+" before calling REST", func(t *testing.T) {
			fakeClient := &fakes.FakeBlueprintRepoClient{}
			result := callApproveBlueprint(t, NewRepo(fakeClient), tt.args)
			assert.True(t, result.IsError)
			assert.Contains(t, textContent(t, result), tt.message)
			assert.Zero(t, fakeClient.ApproveBlueprintWithResponseCallCount())
		})
	}
}

func TestApproveBlueprintManualEnvVarValidation(t *testing.T) {
	entry := map[string]any{
		"serviceType": "web",
		"serviceName": "app",
		"key":         "TOKEN",
		"value":       "input-secret",
	}
	for _, tt := range []struct {
		name          string
		manualEnvVars any
		message       string
	}{
		{
			name:          "non-array manual environment variables",
			manualEnvVars: "input-secret",
			message:       "manualEnvVars is not a valid array",
		},
		{
			name:          "duplicate resource and key",
			manualEnvVars: []any{entry, entry},
			message:       "manualEnvVars[1] duplicates manualEnvVars[0] for the same resource and key",
		},
		{
			name: "invalid resource type",
			manualEnvVars: []any{map[string]any{
				"serviceType": "input-secret",
				"serviceName": "app",
				"key":         "TOKEN",
				"value":       "input-secret",
			}},
			message: "manualEnvVars[0].serviceType must be one of:",
		},
		{
			name: "empty resource name",
			manualEnvVars: []any{map[string]any{
				"serviceType": "web",
				"serviceName": "",
				"key":         "TOKEN",
				"value":       "input-secret",
			}},
			message: "manualEnvVars[0].serviceName cannot be empty",
		},
		{
			name: "missing key",
			manualEnvVars: []any{map[string]any{
				"serviceType": "web",
				"serviceName": "app",
				"value":       "input-secret",
			}},
			message: "manualEnvVars[0].key is required and must be a string",
		},
		{
			name: "missing value",
			manualEnvVars: []any{map[string]any{
				"serviceType": "web",
				"serviceName": "app",
				"key":         "input-secret",
			}},
			message: "manualEnvVars[0].value is required and must be a string",
		},
		{
			name: "non-string value",
			manualEnvVars: []any{map[string]any{
				"serviceType": "web",
				"serviceName": "app",
				"key":         "TOKEN",
				"value":       map[string]any{"nested": "input-secret"},
			}},
			message: "manualEnvVars[0].value is required and must be a string",
		},
		{
			name: "unknown field",
			manualEnvVars: []any{map[string]any{
				"serviceType":  "web",
				"serviceName":  "app",
				"key":          "TOKEN",
				"value":        "input-secret",
				"input-secret": "input-secret",
			}},
			message: "manualEnvVars[0] contains an unsupported field",
		},
	} {
		t.Run("rejects "+tt.name+" before calling REST", func(t *testing.T) {
			fakeClient := &fakes.FakeBlueprintRepoClient{}
			result := callApproveBlueprint(t, NewRepo(fakeClient), map[string]any{
				"blueprintId":   "exs-test",
				"manualEnvVars": tt.manualEnvVars,
			})
			assert.True(t, result.IsError)
			assert.Contains(t, textContent(t, result), tt.message)
			assert.NotContains(t, textContent(t, result), "input-secret")
			assert.Zero(t, fakeClient.ApproveBlueprintWithResponseCallCount())
		})
	}
}

func TestApproveBlueprintDoesNotRequireWorkspace(t *testing.T) {
	fakeClient := &fakes.FakeBlueprintRepoClient{}
	fakeClient.ApproveBlueprintWithResponseReturns(&client.ApproveBlueprintResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusOK},
		JSON200:      &blueprintclient.ApprovedSync{},
	}, nil)
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]any{"blueprintId": "exs-test"}
	result, err := approveBlueprint(NewRepo(fakeClient)).Handler(session.ContextWithWorkspace(t.Context(), ""), request)

	require.NoError(t, err)
	require.False(t, result.IsError, textContent(t, result))
	assert.Equal(t, 1, fakeClient.ApproveBlueprintWithResponseCallCount())
}

func TestApproveBlueprintClientErrors(t *testing.T) {
	for _, tt := range []struct {
		name     string
		response *client.ApproveBlueprintResponse
		err      error
		message  string
	}{
		{
			name: "preserves REST guidance for missing required values",
			response: &client.ApproveBlueprintResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusBadRequest},
				Body:         []byte(`{"message":"Missing values for 1 sync: false environment variables: web/app TOKEN. Nothing was approved."}`),
			},
			message: "web/app TOKEN. Nothing was approved.",
		},
		{
			name: "preserves REST re-plan guidance when no sync can be approved",
			response: &client.ApproveBlueprintResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusConflict},
				Body:         []byte(`{"message":"Blueprint exs-test has no syncs and cannot be approved. Re-plan to create one."}`),
			},
			message: "Re-plan to create one.",
		},
		{
			name: "missing success body is an error",
			response: &client.ApproveBlueprintResponse{
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
			},
			message: "unexpected empty body",
		},
		{
			name:    "reports transport failure as a tool error",
			err:     errors.New("connection lost"),
			message: "connection lost",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeBlueprintRepoClient{}
			fakeClient.ApproveBlueprintWithResponseReturns(tt.response, tt.err)
			result := callApproveBlueprint(t, NewRepo(fakeClient), map[string]any{
				"blueprintId": "exs-test",
			})
			assert.True(t, result.IsError)
			assert.Contains(t, textContent(t, result), tt.message)
		})
	}
}

func callApproveBlueprint(t *testing.T, repo *Repo, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	request := mcp.CallToolRequest{}
	request.Params.Arguments = args
	result, err := approveBlueprint(repo).Handler(session.ContextWithWorkspace(t.Context(), "tea-test"), request)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}
