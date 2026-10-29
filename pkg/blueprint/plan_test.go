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

func TestPlanBlueprintMapsToolInputsToRESTRequest(t *testing.T) {
	fakeClient := &fakes.FakeBlueprintRepoClient{}
	fakeClient.CreateBlueprintSyncWithResponseReturns(&client.CreateBlueprintSyncResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusOK},
		JSON200: &blueprintclient.CreateBlueprintSyncResponse{
			PlanningOutcome: blueprintclient.PlanningOutcomeNoChanges,
			Notices:         []blueprintclient.BlueprintNotice{},
		},
	}, nil)

	result := callPlanBlueprint(t, NewRepo(fakeClient), map[string]any{"blueprintId": "exs-test"})

	require.False(t, result.IsError, textContent(t, result))
	require.Equal(t, 1, fakeClient.CreateBlueprintSyncWithResponseCallCount())
	_, blueprintID, _ := fakeClient.CreateBlueprintSyncWithResponseArgsForCall(0)
	assert.Equal(t, "exs-test", blueprintID)
}

func TestPlanBlueprintPreservesRESTPlanningResponse(t *testing.T) {
	for _, tt := range []struct {
		name            string
		status          int
		planningOutcome blueprintclient.PlanningOutcome
		sync            *blueprintclient.SyncWithPlan
		notices         []blueprintclient.BlueprintNotice
	}{
		{
			name:            "returns the planned sync",
			status:          http.StatusCreated,
			planningOutcome: blueprintclient.PlanningOutcomePlanned,
			sync: &blueprintclient.SyncWithPlan{
				Id:           "exe-planned",
				State:        blueprintclient.SyncStateCreated,
				Commit:       blueprintclient.CommitRef{Id: "commit-123"},
				DashboardUrl: "https://dashboard.render.com/blueprint/exs-test/sync/exe-planned",
			},
			notices: []blueprintclient.BlueprintNotice{},
		},
		{
			name:            "returns the raw plan",
			status:          http.StatusCreated,
			planningOutcome: blueprintclient.PlanningOutcomePlanned,
			sync: &blueprintclient.SyncWithPlan{
				Id:    "exe-planned",
				State: blueprintclient.SyncStateCreated,
				RawPlan: &map[string]any{
					"actions": []any{map[string]any{
						"type":   "CreateService",
						"action": map[string]any{"name": "app"},
					}},
				},
			},
			notices: []blueprintclient.BlueprintNotice{},
		},
		{
			name:            "returns required manual environment variables",
			status:          http.StatusCreated,
			planningOutcome: blueprintclient.PlanningOutcomePlanned,
			sync: &blueprintclient.SyncWithPlan{
				Id:    "exe-planned",
				State: blueprintclient.SyncStateCreated,
				RequiredManualEnvVars: []blueprintclient.RequiredManualEnvVar{
					{
						ServiceType: blueprintclient.ManualEnvVarTargetTypeWeb,
						ServiceName: "app",
						Key:         "TOKEN",
					},
				},
			},
			notices: []blueprintclient.BlueprintNotice{},
		},
		{
			name:            "returns a successful tool result for failed planning without a sync",
			status:          http.StatusOK,
			planningOutcome: blueprintclient.PlanningOutcomeFailed,
			notices: []blueprintclient.BlueprintNotice{
				{
					Severity: blueprintclient.BlueprintNoticeSeverityError,
					Message:  "Blueprint planning failed.",
				},
			},
		},
		{
			name:            "returns a successful tool result for failed planning with a sync",
			status:          http.StatusCreated,
			planningOutcome: blueprintclient.PlanningOutcomeFailed,
			sync: &blueprintclient.SyncWithPlan{
				Id:                    "exe-failed",
				State:                 blueprintclient.SyncStateError,
				RequiredManualEnvVars: []blueprintclient.RequiredManualEnvVar{},
			},
			notices: []blueprintclient.BlueprintNotice{
				{
					Severity: blueprintclient.BlueprintNoticeSeverityError,
					Message:  "Invalid plan.",
				},
			},
		},
		{
			name:            "returns planning error locations",
			status:          http.StatusOK,
			planningOutcome: blueprintclient.PlanningOutcomeFailed,
			notices: []blueprintclient.BlueprintNotice{
				{
					Severity: blueprintclient.BlueprintNoticeSeverityError,
					Message:  "Invalid plan.",
					Path:     new("services[0].plan"),
					Line:     new(3),
					Column:   new(5),
				},
			},
		},
		{
			name:            "returns no sync when there are no changes",
			status:          http.StatusOK,
			planningOutcome: blueprintclient.PlanningOutcomeNoChanges,
			notices:         []blueprintclient.BlueprintNotice{},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response := &blueprintclient.CreateBlueprintSyncResponse{
				PlanningOutcome: tt.planningOutcome,
				Sync:            tt.sync,
				Notices:         tt.notices,
			}
			clientResponse := &client.CreateBlueprintSyncResponse{
				HTTPResponse: &http.Response{StatusCode: tt.status},
			}
			if tt.status == http.StatusCreated {
				clientResponse.JSON201 = response
			} else {
				clientResponse.JSON200 = response
			}
			fakeClient := &fakes.FakeBlueprintRepoClient{}
			fakeClient.CreateBlueprintSyncWithResponseReturns(clientResponse, nil)

			result := callPlanBlueprint(t, NewRepo(fakeClient), map[string]any{"blueprintId": "exs-test"})

			require.False(t, result.IsError, textContent(t, result))
			var got blueprintclient.CreateBlueprintSyncResponse
			require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &got))
			assert.Equal(t, *response, got)
		})
	}
}

func TestPlanBlueprintIDValidation(t *testing.T) {
	t.Run("rejects an invalid Blueprint ID before calling REST", func(t *testing.T) {
		fakeClient := &fakes.FakeBlueprintRepoClient{}
		result := callPlanBlueprint(t, NewRepo(fakeClient), map[string]any{"blueprintId": "exe-test"})

		assert.True(t, result.IsError)
		assert.Contains(t, textContent(t, result), "Blueprint ID starting with exs-")
		assert.Zero(t, fakeClient.CreateBlueprintSyncWithResponseCallCount())
	})
}

func TestPlanBlueprintClientErrors(t *testing.T) {
	t.Run("preserves REST response error message when another sync is active", func(t *testing.T) {
		fakeClient := &fakes.FakeBlueprintRepoClient{}
		fakeClient.CreateBlueprintSyncWithResponseReturns(&client.CreateBlueprintSyncResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusConflict},
			Body:         []byte(`{"message":"Wait for sync exe-active to finish, then re-plan."}`),
		}, nil)

		result := callPlanBlueprint(t, NewRepo(fakeClient), map[string]any{"blueprintId": "exs-test"})

		assert.True(t, result.IsError)
		assert.Contains(t, textContent(t, result), "Wait for sync exe-active to finish, then re-plan.")
	})

	t.Run("missing success body is a tool error", func(t *testing.T) {
		fakeClient := &fakes.FakeBlueprintRepoClient{}
		fakeClient.CreateBlueprintSyncWithResponseReturns(&client.CreateBlueprintSyncResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusCreated},
		}, nil)

		result := callPlanBlueprint(t, NewRepo(fakeClient), map[string]any{"blueprintId": "exs-test"})

		assert.True(t, result.IsError)
		assert.Contains(t, textContent(t, result), "unexpected empty body")
	})

	t.Run("transport failure is a tool error", func(t *testing.T) {
		fakeClient := &fakes.FakeBlueprintRepoClient{}
		fakeClient.CreateBlueprintSyncWithResponseReturns(nil, assert.AnError)

		result := callPlanBlueprint(t, NewRepo(fakeClient), map[string]any{"blueprintId": "exs-test"})

		assert.True(t, result.IsError)
		assert.Equal(t, assert.AnError.Error(), textContent(t, result))
	})
}

func callPlanBlueprint(t *testing.T, repo *Repo, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	request := mcp.CallToolRequest{}
	request.Params.Arguments = args
	result, err := planBlueprint(repo).Handler(t.Context(), request)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}
