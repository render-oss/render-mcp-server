package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	envvar "github.com/render-oss/render-mcp-server/pkg/client/envvar"
	eventsclient "github.com/render-oss/render-mcp-server/pkg/client/events"
	eventstatuses "github.com/render-oss/render-mcp-server/pkg/client/eventstatuses"
	eventtypes "github.com/render-oss/render-mcp-server/pkg/client/eventtypes"
	"github.com/render-oss/render-mcp-server/pkg/deploy"
	"github.com/render-oss/render-mcp-server/pkg/fakes"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/render-oss/render-mcp-server/pkg/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateEnvVarsTool(t *testing.T) {
	ownerId := "own-123456"
	serviceId := "srv-123456"
	deployId := "dep-123456"
	existingEnvVars := []*client.EnvVar{
		{Key: "KEY1", Value: "old_value1"},
		{Key: "KEY2", Value: "old_value2"},
	}
	newEnvVars := []envvar.EnvVarInput{
		envVarInput("KEY1", "new_value1"),
		envVarInput("KEY3", "new_value3"),
	}
	expectedResponseIncludes := "Environment variables updated. A new deploy has been triggered to pick up the changes."
	sensitiveInfo := "sensitive information"

	tests := []struct {
		name            string
		replace         bool
		expectedEnvVars []envvar.EnvVarInput
	}{
		{
			name:            "Replace existing env vars, does not include sensitive info",
			replace:         true,
			expectedEnvVars: newEnvVars,
		},
		{
			name:    "Merge with existing env vars, does not include sensitive info",
			replace: false,
			expectedEnvVars: []envvar.EnvVarInput{
				envVarInput("KEY1", "new_value1"),
				envVarInput("KEY2", "old_value2"),
				envVarInput("KEY3", "new_value3"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeServiceRepoClient{}
			repo := NewRepo(fakeClient)

			fakeDeployClient := &fakes.FakeDeployRepoClient{}
			deployRepo := deploy.NewRepo(fakeDeployClient)

			fakeClient.RetrieveServiceWithResponseReturns(&client.RetrieveServiceResponse{
				JSON200: &client.Service{Id: serviceId, OwnerId: ownerId},
				HTTPResponse: &http.Response{
					StatusCode: 200,
				},
			}, nil)

			fakeDeployClient.RetrieveServiceWithResponseReturns(&client.RetrieveServiceResponse{
				JSON200: &client.Service{Id: serviceId, OwnerId: ownerId},
				HTTPResponse: &http.Response{
					StatusCode: 200,
				},
			}, nil)

			if !tt.replace {
				fakeClient.GetEnvVarsForServiceWithResponseReturns(&client.GetEnvVarsForServiceResponse{
					JSON200: pointers.From(envVarsWithCursor(existingEnvVars)),
					HTTPResponse: &http.Response{
						StatusCode: 200,
					},
				}, nil)
			}

			fakeClient.UpdateEnvVarsForServiceWithResponseReturns(&client.UpdateEnvVarsForServiceResponse{
				HTTPResponse: &http.Response{
					StatusCode: 200,
				},
				Body: []byte(sensitiveInfo),
			}, nil)

			fakeDeployClient.CreateDeployWithResponseReturns(&client.CreateDeployResponse{
				JSON201: &client.Deploy{Id: deployId},
				HTTPResponse: &http.Response{
					StatusCode: 201,
				},
			}, nil)

			request := mcp.CallToolRequest{}
			request.Params.Arguments = map[string]interface{}{
				"serviceId": serviceId,
				"replace":   tt.replace,
				"envVars":   envVarInputsAsParams(newEnvVars),
			}

			tool := updateEnvVars(repo, deployRepo)
			result, err := tool.Handler(createTestContext(t, ownerId), request)

			assert.NoError(t, err)
			assert.NotNil(t, result)

			for _, content := range result.Content {
				if textContent, ok := content.(mcp.TextContent); ok {
					assert.Contains(t, textContent.Text, expectedResponseIncludes)
					assert.Contains(t, textContent.Text, deployId)
					// Verify that we don't include sensitive info
					assert.NotContains(t, textContent.Text, sensitiveInfo)
				}
			}

			_, _, updateEnvVarInput, _ := fakeClient.UpdateEnvVarsForServiceWithResponseArgsForCall(0)
			if tt.replace {
				assert.Equal(t, 0, fakeClient.GetEnvVarsForServiceWithResponseCallCount())
				assert.Equal(t, tt.expectedEnvVars, updateEnvVarInput)
			} else {
				// Verify that when we're not replacing, we get the existing env vars and set a
				// a merged list of env vars
				assert.Equal(t, 1, fakeClient.GetEnvVarsForServiceWithResponseCallCount())
				assert.ElementsMatch(t, tt.expectedEnvVars, updateEnvVarInput)
			}

		})
	}
}

func TestUpdateEnvVarsToolWorkspaceMismatch(t *testing.T) {
	fakeClient := &fakes.FakeServiceRepoClient{}
	repo := NewRepo(fakeClient)

	fakeDeployClient := &fakes.FakeDeployRepoClient{}
	deployRepo := deploy.NewRepo(fakeDeployClient)

	// The service belongs to a different workspace than the session's.
	fakeClient.RetrieveServiceWithResponseReturns(&client.RetrieveServiceResponse{
		JSON200: &client.Service{Id: "srv-123456", OwnerId: "own-123456"},
		HTTPResponse: &http.Response{
			StatusCode: 200,
		},
	}, nil)

	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]interface{}{
		"serviceId": "srv-123456",
		"replace":   true,
		"envVars":   envVarInputsAsParams([]envvar.EnvVarInput{envVarInput("KEY1", "value1")}),
	}

	tool := updateEnvVars(repo, deployRepo)
	result, err := tool.Handler(createTestContext(t, "own-other"), request)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
	// The env vars must not be mutated when the workspace check fails.
	assert.Equal(t, 0, fakeClient.UpdateEnvVarsForServiceWithResponseCallCount())
	assert.Equal(t, 0, fakeDeployClient.CreateDeployWithResponseCallCount())
}

func envVarInput(key, value string) envvar.EnvVarInput {
	var input envvar.EnvVarInput
	input.FromEnvVarKeyValue(envvar.EnvVarKeyValue{
		Key:   key,
		Value: value,
	})
	return input
}

func envVarsWithCursor(envVars []*client.EnvVar) []client.EnvVarWithCursor {
	envVarsWithCursor := make([]client.EnvVarWithCursor, 0, len(envVars))
	for i, envVar := range envVars {
		envVarsWithCursor = append(envVarsWithCursor, client.EnvVarWithCursor{
			EnvVar: *envVar,
			Cursor: client.Cursor(fmt.Sprintf("%d", i)),
		})
	}
	return envVarsWithCursor
}

func envVarInputsAsParams(envVars []envvar.EnvVarInput) []interface{} {
	envVarsAsParams := make([]interface{}, 0, len(envVars))
	for _, envVar := range envVars {
		kv, _ := envVar.AsEnvVarKeyValue()
		envVarsAsParams = append(envVarsAsParams, map[string]interface{}{
			"key":   kv.Key,
			"value": kv.Value,
		})
	}
	return envVarsAsParams
}

func TestCreateWebServiceTool(t *testing.T) {
	ownerId := "own-123456"
	serviceName := "test-web-service"
	runtime := "node"
	buildCommand := "npm install"
	startCommand := "npm start"

	tests := []struct {
		name         string
		plan         *string
		expectedPlan *client.Plan
	}{
		{
			name:         "Create web service with no plan defaults to free",
			plan:         nil,
			expectedPlan: pointers.From(client.Plan("free")),
		},
		{
			name:         "Create web service with free plan",
			plan:         pointers.From("free"),
			expectedPlan: pointers.From(client.Plan("free")),
		},
		{
			name:         "Create web service with starter plan",
			plan:         pointers.From("starter"),
			expectedPlan: pointers.From(client.PlanStarter),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeServiceRepoClient{}
			repo := NewRepo(fakeClient)

			fakeClient.CreateServiceWithResponseReturns(&client.CreateServiceResponse{
				JSON201: &client.ServiceAndDeploy{
					Service: &client.Service{
						Id:   "srv-web-123",
						Name: serviceName,
						Type: client.WebService,
					},
				},
				HTTPResponse: &http.Response{
					StatusCode: 201,
				},
			}, nil)

			ctx := createTestContext(t, ownerId)

			args := map[string]any{
				"name":         serviceName,
				"runtime":      runtime,
				"buildCommand": buildCommand,
				"startCommand": startCommand,
			}
			if tt.plan != nil {
				args["plan"] = *tt.plan
			}
			request := mcp.CallToolRequest{}
			request.Params.Arguments = args

			tool := createWebService(repo)
			result, err := tool.Handler(ctx, request)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.False(t, result.IsError, "expected no error but got: %v", result.Content)

			assert.Equal(t, 1, fakeClient.CreateServiceWithResponseCallCount())
			_, requestBody, _ := fakeClient.CreateServiceWithResponseArgsForCall(0)
			assert.Equal(t, serviceName, requestBody.Name)
			assert.Equal(t, ownerId, requestBody.OwnerId)
			assert.Equal(t, client.WebService, requestBody.Type)

			webServiceDetails, err := requestBody.ServiceDetails.AsWebServiceDetailsPOST()
			assert.NoError(t, err)
			assert.Equal(t, client.ServiceRuntime(runtime), webServiceDetails.Runtime)
			assert.Equal(t, tt.expectedPlan, webServiceDetails.Plan)
		})
	}
}

func TestCreateServiceRuntimeSchema(t *testing.T) {
	for _, tool := range []mcp.Tool{createWebService(nil).Tool, createCronJob(nil).Tool} {
		t.Run(tool.Name, func(t *testing.T) {
			for _, param := range []string{"buildCommand", "startCommand", "dockerfilePath", "dockerContext", "dockerCommand"} {
				assert.NotContains(t, tool.InputSchema.Required, param)
			}
			for param, wantDefault := range map[string]string{
				"dockerfilePath": "./Dockerfile",
				"dockerContext":  ".",
				"dockerCommand":  "",
			} {
				property, ok := tool.InputSchema.Properties[param].(map[string]any)
				require.True(t, ok, "missing property %s", param)
				assert.Equal(t, wantDefault, property["default"], "default for %s", param)
			}
		})
	}
}

func TestCreateServiceRuntimeDetails(t *testing.T) {
	tests := []struct {
		name        string
		runtime     string
		params      map[string]any
		wantDetails string
	}{
		{
			name:        "native commands",
			runtime:     "node",
			params:      map[string]any{"buildCommand": "npm install", "startCommand": "npm start"},
			wantDetails: `{"buildCommand":"npm install","startCommand":"npm start"}`,
		},
		{
			name:        "Docker without overrides",
			runtime:     "docker",
			wantDetails: `{"dockerfilePath":"./Dockerfile","dockerContext":".","dockerCommand":""}`,
		},
		{
			name:        "Dockerfile path only",
			runtime:     "docker",
			params:      map[string]any{"dockerfilePath": "deploy/Dockerfile"},
			wantDetails: `{"dockerfilePath":"deploy/Dockerfile","dockerContext":".","dockerCommand":""}`,
		},
		{
			name:        "Docker context only",
			runtime:     "docker",
			params:      map[string]any{"dockerContext": "app"},
			wantDetails: `{"dockerfilePath":"./Dockerfile","dockerContext":"app","dockerCommand":""}`,
		},
		{
			name:        "Docker command only",
			runtime:     "docker",
			params:      map[string]any{"dockerCommand": "./run"},
			wantDetails: `{"dockerfilePath":"./Dockerfile","dockerContext":".","dockerCommand":"./run"}`,
		},
		{
			name:        "all Docker overrides",
			runtime:     "docker",
			params:      map[string]any{"dockerfilePath": "deploy/Dockerfile", "dockerContext": "app", "dockerCommand": "./run"},
			wantDetails: `{"dockerfilePath":"deploy/Dockerfile","dockerContext":"app","dockerCommand":"./run"}`,
		},
	}

	for name, buildRequest := range map[string]func(context.Context, mcp.CallToolRequest) (*client.CreateServiceJSONRequestBody, error){
		"web service": createValidatedWebServiceRequest,
		"cron job":    createValidatedCronJobRequest,
	} {
		t.Run(name, func(t *testing.T) {
			ctx := createTestContext(t, "own-123")
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					args := map[string]any{"name": "test-service", "runtime": tt.runtime}
					if name == "cron job" {
						args["schedule"] = "0 * * * *"
					}
					for key, value := range tt.params {
						args[key] = value
					}
					request := mcp.CallToolRequest{}
					request.Params.Arguments = args

					body, err := buildRequest(ctx, request)
					require.NoError(t, err)
					data, err := json.Marshal(body.ServiceDetails)
					require.NoError(t, err)
					var decoded struct {
						Runtime            string          `json:"runtime"`
						EnvSpecificDetails json.RawMessage `json:"envSpecificDetails"`
					}
					require.NoError(t, json.Unmarshal(data, &decoded))
					assert.Equal(t, tt.runtime, decoded.Runtime)
					assert.JSONEq(t, tt.wantDetails, string(decoded.EnvSpecificDetails))
				})
			}
		})
	}
}

func TestCreateServiceRuntimeValidation(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]any
		wantError string
	}{
		{
			name:      "missing build command",
			args:      map[string]any{"runtime": "node", "startCommand": "npm start"},
			wantError: "required parameter not present: buildCommand",
		},
		{
			name:      "missing start command",
			args:      map[string]any{"runtime": "node", "buildCommand": "npm install"},
			wantError: "required parameter not present: startCommand",
		},
	}

	for name, buildRequest := range map[string]func(context.Context, mcp.CallToolRequest) (*client.CreateServiceJSONRequestBody, error){
		"web service": createValidatedWebServiceRequest,
		"cron job":    createValidatedCronJobRequest,
	} {
		t.Run(name, func(t *testing.T) {
			ctx := createTestContext(t, "own-123")
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					args := map[string]any{"name": "test-service"}
					if name == "cron job" {
						args["schedule"] = "0 * * * *"
					}
					for key, value := range tt.args {
						args[key] = value
					}
					request := mcp.CallToolRequest{}
					request.Params.Arguments = args

					_, err := buildRequest(ctx, request)
					require.EqualError(t, err, tt.wantError)
				})
			}
		})
	}
}

func TestCreateCronJobToolRejectsInvalidSchedule(t *testing.T) {
	ctx := createTestContext(t, "own-123")

	for _, schedule := range []string{"every day", "99 99 * * *", "0 0 * *", "*/0 * * * *"} {
		t.Run(schedule, func(t *testing.T) {
			fakeClient := &fakes.FakeServiceRepoClient{}
			request := mcp.CallToolRequest{}
			request.Params.Arguments = map[string]any{
				"name":         "test-service",
				"runtime":      "node",
				"buildCommand": "npm install",
				"startCommand": "npm start",
				"schedule":     schedule,
			}

			result, err := createCronJob(NewRepo(fakeClient)).Handler(ctx, request)

			require.NoError(t, err)
			require.True(t, result.IsError)
			assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "invalid schedule expression")
			assert.Zero(t, fakeClient.CreateServiceWithResponseCallCount())
		})
	}
}

func TestCreateCronJobTool(t *testing.T) {
	ownerId := "own-123456"
	cronJobName := "test-cron-job"
	schedule := "0 0 * * *"
	runtime := "node"
	buildCommand := "npm install"
	startCommand := "node scripts/cleanup.js"
	repo := "https://github.com/test/repo.git"
	branch := "main"
	plan := "starter"
	region := "oregon"
	autoDeploy := "yes"
	envVars := []interface{}{
		map[string]interface{}{
			"key":   "NODE_ENV",
			"value": "production",
		},
	}

	tests := []struct {
		name                 string
		params               map[string]interface{}
		expectedServiceType  client.ServiceType
		expectedResponseCode int
		expectError          bool
		validateRequestBody  func(*testing.T, client.CreateServiceJSONRequestBody)
	}{
		{
			name: "Create cron job with all required params",
			params: map[string]interface{}{
				"name":         cronJobName,
				"schedule":     schedule,
				"runtime":      runtime,
				"buildCommand": buildCommand,
				"startCommand": startCommand,
			},
			expectedServiceType:  client.CronJob,
			expectedResponseCode: 201,
			expectError:          false,
			validateRequestBody: func(t *testing.T, body client.CreateServiceJSONRequestBody) {
				assert.Equal(t, cronJobName, body.Name)
				assert.Equal(t, ownerId, body.OwnerId)
				assert.Equal(t, client.CronJob, body.Type)

				cronJobDetails, err := body.ServiceDetails.AsCronJobDetailsPOST()
				assert.NoError(t, err)
				assert.Equal(t, client.ServiceRuntime(runtime), cronJobDetails.Runtime)
				assert.Equal(t, schedule, cronJobDetails.Schedule)
				assert.NotNil(t, cronJobDetails.EnvSpecificDetails)

				envDetails, err := cronJobDetails.EnvSpecificDetails.AsNativeEnvironmentDetails()
				assert.NoError(t, err)
				assert.Equal(t, buildCommand, envDetails.BuildCommand)
				assert.Equal(t, startCommand, envDetails.StartCommand)
			},
		},
		{
			name: "Create cron job with all optional params",
			params: map[string]interface{}{
				"name":         cronJobName,
				"schedule":     schedule,
				"runtime":      runtime,
				"buildCommand": buildCommand,
				"startCommand": startCommand,
				"repo":         repo,
				"branch":       branch,
				"plan":         plan,
				"region":       region,
				"autoDeploy":   autoDeploy,
				"envVars":      envVars,
			},
			expectedServiceType:  client.CronJob,
			expectedResponseCode: 201,
			expectError:          false,
			validateRequestBody: func(t *testing.T, body client.CreateServiceJSONRequestBody) {
				assert.Equal(t, cronJobName, body.Name)
				assert.Equal(t, ownerId, body.OwnerId)
				assert.Equal(t, client.CronJob, body.Type)
				assert.NotNil(t, body.Repo)
				assert.Equal(t, repo, *body.Repo)
				assert.NotNil(t, body.Branch)
				assert.Equal(t, branch, *body.Branch)
				assert.NotNil(t, body.AutoDeploy)
				assert.Equal(t, client.AutoDeploy(autoDeploy), *body.AutoDeploy)
				assert.NotNil(t, body.EnvVars)

				cronJobDetails, err := body.ServiceDetails.AsCronJobDetailsPOST()
				assert.NoError(t, err)
				assert.Equal(t, client.ServiceRuntime(runtime), cronJobDetails.Runtime)
				assert.Equal(t, schedule, cronJobDetails.Schedule)
				assert.NotNil(t, cronJobDetails.Plan)
				assert.Equal(t, client.PaidPlanStarter, *cronJobDetails.Plan)
				assert.NotNil(t, cronJobDetails.Region)
				assert.Equal(t, client.Region(region), *cronJobDetails.Region)
			},
		},
		{
			name: "Create cron job with different schedule - every 15 minutes",
			params: map[string]interface{}{
				"name":         cronJobName,
				"schedule":     "*/15 * * * *",
				"runtime":      "python",
				"buildCommand": "pip install -r requirements.txt",
				"startCommand": "python scripts/process.py",
			},
			expectedServiceType:  client.CronJob,
			expectedResponseCode: 201,
			expectError:          false,
			validateRequestBody: func(t *testing.T, body client.CreateServiceJSONRequestBody) {
				cronJobDetails, err := body.ServiceDetails.AsCronJobDetailsPOST()
				assert.NoError(t, err)
				assert.Equal(t, "*/15 * * * *", cronJobDetails.Schedule)
				assert.Equal(t, client.ServiceRuntime("python"), cronJobDetails.Runtime)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeServiceRepoClient{}
			repo := NewRepo(fakeClient)

			// Mock the CreateService response
			fakeClient.CreateServiceWithResponseReturns(&client.CreateServiceResponse{
				JSON201: &client.ServiceAndDeploy{
					Service: &client.Service{
						Id:   "srv-cron-123",
						Name: cronJobName,
						Type: client.CronJob,
					},
				},
				HTTPResponse: &http.Response{
					StatusCode: tt.expectedResponseCode,
				},
			}, nil)

			// Create a test context with a session
			ctx := createTestContext(t, ownerId)

			// Build the request
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.params

			// Call the tool
			tool := createCronJob(repo)
			result, err := tool.Handler(ctx, request)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, result)

			// Verify CreateService was called
			assert.Equal(t, 1, fakeClient.CreateServiceWithResponseCallCount())

			// Get the request body and validate it
			_, requestBody, _ := fakeClient.CreateServiceWithResponseArgsForCall(0)
			tt.validateRequestBody(t, requestBody)

			// Verify the response contains the service ID
			for _, content := range result.Content {
				if textContent, ok := content.(mcp.TextContent); ok {
					assert.Contains(t, textContent.Text, "srv-cron-123")
					assert.Contains(t, textContent.Text, cronJobName)
				}
			}
		})
	}
}

// createTestContext creates a test context with a session that has the given workspace ID
func createTestContext(t *testing.T, workspaceID string) context.Context {
	t.Helper()
	t.Setenv("RENDER_CONFIG_PATH", filepath.Join(t.TempDir(), "mcp-server.yaml"))
	ctx := session.ContextWithStdioSession(context.Background())
	sess := session.FromContext(ctx)
	sess.SetWorkspace(ctx, workspaceID)
	return ctx
}

func TestMergeEnvVars(t *testing.T) {
	tests := []struct {
		name          string
		oldEnvVars    []*client.EnvVar
		newEnvVars    []envvar.EnvVarInput
		expected      []envvar.EnvVarInput
		expectedError string
	}{
		{
			name:       "Empty old env vars, non-empty new env vars",
			oldEnvVars: []*client.EnvVar{},
			newEnvVars: []envvar.EnvVarInput{
				envVarInput("KEY1", "value1"),
				envVarInput("KEY2", "value2"),
			},
			expected: []envvar.EnvVarInput{
				envVarInput("KEY1", "value1"),
				envVarInput("KEY2", "value2"),
			},
		},
		{
			name: "Non-empty old env vars, empty new env vars",
			oldEnvVars: []*client.EnvVar{
				{Key: "KEY1", Value: "value1"},
				{Key: "KEY2", Value: "value2"},
			},
			newEnvVars: []envvar.EnvVarInput{},
			expected: []envvar.EnvVarInput{
				envVarInput("KEY1", "value1"),
				envVarInput("KEY2", "value2"),
			},
		},
		{
			name: "No overlapping keys",
			oldEnvVars: []*client.EnvVar{
				{Key: "KEY1", Value: "value1"},
				{Key: "KEY2", Value: "value2"},
			},
			newEnvVars: []envvar.EnvVarInput{
				envVarInput("KEY3", "value3"),
				envVarInput("KEY4", "value4"),
			},
			expected: []envvar.EnvVarInput{
				envVarInput("KEY1", "value1"),
				envVarInput("KEY2", "value2"),
				envVarInput("KEY3", "value3"),
				envVarInput("KEY4", "value4"),
			},
		},
		{
			name: "Some overlapping keys",
			oldEnvVars: []*client.EnvVar{
				{Key: "KEY1", Value: "old_value1"},
				{Key: "KEY2", Value: "old_value2"},
				{Key: "KEY3", Value: "value3"},
			},
			newEnvVars: []envvar.EnvVarInput{
				envVarInput("KEY1", "new_value1"),
				envVarInput("KEY2", "new_value2"),
				envVarInput("KEY4", "value4"),
			},
			expected: []envvar.EnvVarInput{
				envVarInput("KEY1", "new_value1"),
				envVarInput("KEY2", "new_value2"),
				envVarInput("KEY3", "value3"),
				envVarInput("KEY4", "value4"),
			},
		},
		{
			name: "All overlapping keys",
			oldEnvVars: []*client.EnvVar{
				{Key: "KEY1", Value: "old_value1"},
				{Key: "KEY2", Value: "old_value2"},
			},
			newEnvVars: []envvar.EnvVarInput{
				envVarInput("KEY1", "new_value1"),
				envVarInput("KEY2", "new_value2"),
			},
			expected: []envvar.EnvVarInput{
				envVarInput("KEY1", "new_value1"),
				envVarInput("KEY2", "new_value2"),
			},
		},
		{
			name:       "Empty both old and new env vars",
			oldEnvVars: []*client.EnvVar{},
			newEnvVars: []envvar.EnvVarInput{},
			expected:   []envvar.EnvVarInput{},
		},
		{
			name: "Case sensitivity in keys",
			oldEnvVars: []*client.EnvVar{
				{Key: "key", Value: "value"},
				{Key: "KEY", Value: "VALUE"},
			},
			newEnvVars: []envvar.EnvVarInput{
				envVarInput("Key", "newValue"),
			},
			expected: []envvar.EnvVarInput{
				envVarInput("KEY", "VALUE"),
				envVarInput("Key", "newValue"),
				envVarInput("key", "value"),
			},
		},
		{
			name: "Special characters in keys",
			oldEnvVars: []*client.EnvVar{
				{Key: "DATABASE_URL", Value: "postgres://user:pass@localhost:5432/db"},
			},
			newEnvVars: []envvar.EnvVarInput{
				envVarInput("API_KEY", "abcd1234"),
			},
			expected: []envvar.EnvVarInput{
				envVarInput("API_KEY", "abcd1234"),
				envVarInput("DATABASE_URL", "postgres://user:pass@localhost:5432/db"),
			},
		},
		{
			name: "Empty values",
			oldEnvVars: []*client.EnvVar{
				{Key: "EMPTY_KEY", Value: ""},
			},
			newEnvVars: []envvar.EnvVarInput{
				envVarInput("ANOTHER_EMPTY", ""),
			},
			expected: []envvar.EnvVarInput{
				envVarInput("ANOTHER_EMPTY", ""),
				envVarInput("EMPTY_KEY", ""),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mergedEnvVars, err := mergeEnvVars(tt.oldEnvVars, tt.newEnvVars)

			assert.NoError(t, err, "Expected no error but got one")
			assert.ElementsMatch(t, tt.expected, mergedEnvVars, "Environment variables don't match expected values")
		})
	}
}

// specBasedPlanPattern matches the spec-based compute plan names introduced by
// the plan rename (e.g. "0.5c-512mb", "12c-96g").
var specBasedPlanPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?c-[0-9]+(mb|g)$`)

// planEnumFromTool reads the advertised enum for a tool's "plan" parameter.
func planEnumFromTool(t *testing.T, tool server.ServerTool) []string {
	t.Helper()

	planProp, ok := tool.Tool.InputSchema.Properties["plan"].(map[string]any)
	require.True(t, ok, "tool %q has no plan property", tool.Tool.Name)

	values, ok := planProp["enum"].([]string)
	require.True(t, ok, "tool %q plan property has no string enum", tool.Tool.Name)

	return values
}

// TestServiceToolPlanEnumsAreAccepted pins both service-creating tools' plan
// parameters to the generated enum. Advertising a plan validate.ServicePlan
// rejects is an error the MCP client only discovers by calling the tool, and
// requiring a spec-based name catches a schema that has been pinned to a
// hardcoded list and so no longer picks up newly added plans.
func TestServiceToolPlanEnumsAreAccepted(t *testing.T) {
	repo := NewRepo(&fakes.FakeServiceRepoClient{})

	for _, tool := range []server.ServerTool{createWebService(repo), createCronJob(repo)} {
		t.Run(tool.Tool.Name, func(t *testing.T) {
			plans := planEnumFromTool(t, tool)

			for _, plan := range plans {
				_, err := validate.ServicePlan(plan)
				assert.NoError(t, err, "advertised plan %q is rejected by validate.ServicePlan", plan)
			}

			assert.True(t, slices.ContainsFunc(plans, specBasedPlanPattern.MatchString),
				"no spec-based plan name advertised, got %v", plans)
		})
	}
}

const (
	testOwnerId   = "own-123456"
	testCronJobId = "crn-123456"
)

func newCronJobTestClient(t *testing.T, service client.Service) (*fakes.FakeServiceRepoClient, *Repo) {
	t.Helper()
	fakeClient := &fakes.FakeServiceRepoClient{}
	fakeClient.RetrieveServiceWithResponseReturns(&client.RetrieveServiceResponse{
		JSON200:      &service,
		HTTPResponse: &http.Response{StatusCode: http.StatusOK},
	}, nil)
	fakeClient.RunCronJobWithResponseReturns(&client.RunCronJobResponse{
		JSON200:      &client.CronJobRun{Id: "crnrun-new", Status: client.CronJobRunStatusPending},
		HTTPResponse: &http.Response{StatusCode: http.StatusOK},
	}, nil)
	fakeClient.CancelCronJobRunWithResponseReturns(&client.CancelCronJobRunResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusNoContent},
	}, nil)
	// By default no run is in progress, and the triggered run shows up on the
	// first poll.
	stubRunEvents(fakeClient,
		eventsReply{resp: cronRunEvents(t)},
		eventsReply{resp: cronRunEvents(t, cronRunEvent{runId: "crnrun-new"})},
	)

	repo := NewRepo(fakeClient)
	repo.runRecordedTimeout = 200 * time.Millisecond
	repo.runRecordedPollInterval = time.Millisecond
	return fakeClient, repo
}

type eventsReply struct {
	resp *client.ListEventsResponse
	err  error
}

// stubRunEvents answers the nth ListEvents call with replies[n], repeating
// the last reply once they run out.
func stubRunEvents(fakeClient *fakes.FakeServiceRepoClient, replies ...eventsReply) {
	var mu sync.Mutex
	call := 0
	fakeClient.ListEventsWithResponseStub = func(context.Context, client.ServiceIdParam, *client.ListEventsParams, ...client.RequestEditorFn) (*client.ListEventsResponse, error) {
		mu.Lock()
		defer mu.Unlock()
		reply := replies[min(call, len(replies)-1)]
		call++
		return reply.resp, reply.err
	}
}

func cronJob() client.Service {
	return client.Service{
		Id:        testCronJobId,
		OwnerId:   testOwnerId,
		Type:      client.CronJob,
		Suspended: client.ServiceSuspendedNotSuspended,
	}
}

type cronRunEvent struct {
	ended bool
	runId string
	at    time.Time
}

// cronRunEvents builds a list_events response from events given newest first,
// matching the API's order.
func cronRunEvents(t *testing.T, events ...cronRunEvent) *client.ListEventsResponse {
	t.Helper()
	body := make([]client.ServiceEventWithCursor, 0, len(events))
	for i, e := range events {
		var details eventsclient.ServiceEventDetails
		eventType := eventtypes.ServiceEventTypeCronJobRunStarted
		if e.ended {
			eventType = eventtypes.ServiceEventTypeCronJobRunEnded
			require.NoError(t, details.FromCronJobRunEndedEvent(eventsclient.CronJobRunEndedEvent{
				CronJobRunId: e.runId,
				Status:       eventstatuses.CronJobRunStatusSuccessful,
			}))
		} else {
			require.NoError(t, details.FromCronJobRunStartedEvent(eventsclient.CronJobRunStartedEvent{
				CronJobRunId: e.runId,
			}))
		}
		body = append(body, client.ServiceEventWithCursor{
			Cursor: client.Cursor(fmt.Sprintf("%d", i)),
			Event: eventsclient.ServiceEvent{
				Id:        fmt.Sprintf("evt-%d", i),
				ServiceId: testCronJobId,
				Timestamp: e.at,
				Type:      eventType,
				Details:   details,
			},
		})
	}
	return &client.ListEventsResponse{
		JSON200:      &body,
		HTTPResponse: &http.Response{StatusCode: http.StatusOK},
	}
}

func callCronJobTool(t *testing.T, tool server.ServerTool, workspace string, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	request := mcp.CallToolRequest{}
	request.Params.Arguments = args
	result, err := tool.Handler(createTestContext(t, workspace), request)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	return result, text.Text
}

func TestRunCronJobTool(t *testing.T) {
	startedAt := time.Date(2026, 11, 13, 17, 0, 0, 0, time.UTC)

	tests := []struct {
		name            string
		events          []cronRunEvent
		eventsErr       error
		cancelActiveRun bool
		wantRun         bool
		wantText        []string
	}{
		{
			name:     "no recent runs",
			wantRun:  true,
			wantText: []string{`{"run":{"id":"crnrun-new","status":"pending"}}`},
		},
		{
			name: "latest run has ended",
			events: []cronRunEvent{
				{ended: true, runId: "crnrun-1"},
				{runId: "crnrun-1"},
			},
			wantRun:  true,
			wantText: []string{`{"run":{"id":"crnrun-new","status":"pending"}}`},
		},
		{
			name: "refuses while a run is in progress",
			events: []cronRunEvent{
				{runId: "crnrun-2", at: startedAt},
				{ended: true, runId: "crnrun-1"},
				{runId: "crnrun-1"},
			},
			wantRun:  false,
			wantText: []string{"crnrun-2", "2026-11-13T17:00:00Z", "cancelActiveRun"},
		},
		{
			name: "cancels an in-progress run when asked and reports it",
			events: []cronRunEvent{
				{runId: "crnrun-2", at: startedAt},
			},
			cancelActiveRun: true,
			wantRun:         true,
			wantText:        []string{`"canceledRun":{"id":"crnrun-2","startedAt":"2026-11-13T17:00:00Z"}`},
		},
		{
			name:      "refuses when it cannot check for an in-progress run",
			eventsErr: errors.New("connection reset"),
			wantRun:   false,
			wantText:  []string{"connection reset", "cancelActiveRun"},
		},
		{
			name:            "runs anyway when the check fails but canceling was approved",
			eventsErr:       errors.New("connection reset"),
			cancelActiveRun: true,
			wantRun:         true,
			wantText:        []string{`{"run":{"id":"crnrun-new","status":"pending"}}`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient, repo := newCronJobTestClient(t, cronJob())
			// The first call is the in-progress check; later calls are the
			// wait for the new run, which shows up right away.
			recorded := append([]cronRunEvent{{runId: "crnrun-new"}}, tt.events...)
			stubRunEvents(fakeClient,
				eventsReply{resp: cronRunEvents(t, tt.events...), err: tt.eventsErr},
				eventsReply{resp: cronRunEvents(t, recorded...)},
			)

			result, text := callCronJobTool(t, runCronJob(repo), testOwnerId, map[string]any{
				"serviceId":       testCronJobId,
				"cancelActiveRun": tt.cancelActiveRun,
			})

			assert.Equal(t, !tt.wantRun, result.IsError, text)
			for _, want := range tt.wantText {
				assert.Contains(t, text, want)
			}
			if !tt.wantRun {
				assert.Equal(t, 0, fakeClient.RunCronJobWithResponseCallCount())
				return
			}
			require.Equal(t, 1, fakeClient.RunCronJobWithResponseCallCount())
			_, gotId, _ := fakeClient.RunCronJobWithResponseArgsForCall(0)
			assert.Equal(t, testCronJobId, gotId)
			assert.NotContains(t, text, "warning")
		})
	}
}

// The API returns from a trigger before the run's started event exists. Until
// it does, the in-progress check cannot see the run, so run_cron_job waits for
// it and warns the model if it never shows up.
func TestRunCronJobToolWaitsForRunToBeRecorded(t *testing.T) {
	noRuns := eventsReply{resp: cronRunEvents(t)}

	tests := []struct {
		name        string
		polls       []eventsReply
		wantWarning bool
	}{
		{
			name: "started event appears after a few polls",
			polls: []eventsReply{
				noRuns,
				noRuns,
				{resp: cronRunEvents(t, cronRunEvent{runId: "crnrun-new"})},
			},
		},
		{
			name: "only an ended event appears because the run finished fast",
			polls: []eventsReply{
				{resp: cronRunEvents(t, cronRunEvent{ended: true, runId: "crnrun-new"})},
			},
		},
		{
			name: "a failed poll is retried",
			polls: []eventsReply{
				{err: errors.New("connection reset")},
				{resp: cronRunEvents(t, cronRunEvent{runId: "crnrun-new"})},
			},
		},
		{
			name: "another run's event does not count",
			polls: []eventsReply{
				{resp: cronRunEvents(t, cronRunEvent{runId: "crnrun-other"})},
			},
			wantWarning: true,
		},
		{
			name:        "never appears",
			polls:       []eventsReply{noRuns},
			wantWarning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient, repo := newCronJobTestClient(t, cronJob())
			stubRunEvents(fakeClient, append([]eventsReply{noRuns}, tt.polls...)...)

			result, text := callCronJobTool(t, runCronJob(repo), testOwnerId, map[string]any{"serviceId": testCronJobId})

			// The run was triggered either way, so this is never an error.
			require.False(t, result.IsError, text)
			assert.Contains(t, text, `"run":{"id":"crnrun-new","status":"pending"}`)
			if !tt.wantWarning {
				assert.NotContains(t, text, "warning")
				// One in-progress check, then one poll per reply until found.
				assert.Equal(t, 1+len(tt.polls), fakeClient.ListEventsWithResponseCallCount())
				return
			}
			assert.Contains(t, text, `"warning":"Run crnrun-new was triggered but has not been recorded`)
			assert.Contains(t, text, "would cancel it")
		})
	}
}

// The in-progress check relies on the API accepting several event types
// joined by commas in its one `type` param, and on a lookback long enough to
// cover the 12-hour run limit. Pin both so a change shows up here instead of
// as a silently missed active run.
func TestRunCronJobToolEventQuery(t *testing.T) {
	fakeClient, repo := newCronJobTestClient(t, cronJob())

	before := time.Now().UTC()
	result, text := callCronJobTool(t, runCronJob(repo), testOwnerId, map[string]any{"serviceId": testCronJobId})
	require.False(t, result.IsError, text)

	// Call 0 is the in-progress check and call 1 the wait for the new run;
	// both use the same query.
	require.Equal(t, 2, fakeClient.ListEventsWithResponseCallCount())
	_, gotId, params, _ := fakeClient.ListEventsWithResponseArgsForCall(0)
	assert.Equal(t, testCronJobId, gotId)

	req, err := client.NewListEventsRequest("https://api.render.com/v1", gotId, params)
	require.NoError(t, err)
	assert.Equal(t, []string{"cron_job_run_started,cron_job_run_ended"}, req.URL.Query()["type"])

	require.NotNil(t, params.StartTime)
	assert.True(t, params.StartTime.Before(before.Add(-12*time.Hour)),
		"lookback %s must exceed the 12-hour run limit", before.Sub(*params.StartTime))
}

// Every guard must reject before anything is triggered or canceled.
func TestCronJobToolsRejectBeforeCallingAPI(t *testing.T) {
	webService := cronJob()
	webService.Type = client.WebService
	suspended := cronJob()
	suspended.Suspended = client.ServiceSuspendedSuspended

	tests := []struct {
		name      string
		service   client.Service
		workspace string
		wantText  string
	}{
		{name: "not a cron job", service: webService, workspace: testOwnerId, wantText: "not a cron job"},
		{name: "suspended", service: suspended, workspace: testOwnerId, wantText: "suspended"},
		{name: "another workspace", service: cronJob(), workspace: "own-other"},
	}

	for _, tt := range tests {
		for _, tool := range []func(*Repo) server.ServerTool{runCronJob, cancelCronJobRun} {
			fakeClient, repo := newCronJobTestClient(t, tt.service)
			st := tool(repo)
			t.Run(st.Tool.Name+"/"+tt.name, func(t *testing.T) {
				result, text := callCronJobTool(t, st, tt.workspace, map[string]any{
					"serviceId":       testCronJobId,
					"cancelActiveRun": true,
				})

				assert.True(t, result.IsError)
				assert.Contains(t, text, tt.wantText)
				assert.Equal(t, 0, fakeClient.RunCronJobWithResponseCallCount())
				assert.Equal(t, 0, fakeClient.CancelCronJobRunWithResponseCallCount())
			})
		}
	}
}

func TestCancelCronJobRunTool(t *testing.T) {
	t.Run("cancels the run", func(t *testing.T) {
		fakeClient, repo := newCronJobTestClient(t, cronJob())

		result, text := callCronJobTool(t, cancelCronJobRun(repo), testOwnerId, map[string]any{"serviceId": testCronJobId})

		require.False(t, result.IsError, text)
		assert.JSONEq(t, `{"serviceId":"crn-123456","canceled":true}`, text)
		require.Equal(t, 1, fakeClient.CancelCronJobRunWithResponseCallCount())
		_, gotId, _ := fakeClient.CancelCronJobRunWithResponseArgsForCall(0)
		assert.Equal(t, testCronJobId, gotId)
	})

	t.Run("surfaces an API error", func(t *testing.T) {
		fakeClient, repo := newCronJobTestClient(t, cronJob())
		fakeClient.CancelCronJobRunWithResponseReturns(&client.CancelCronJobRunResponse{
			Body:         []byte(`{"message":"boom"}`),
			HTTPResponse: &http.Response{StatusCode: http.StatusInternalServerError},
		}, nil)

		result, text := callCronJobTool(t, cancelCronJobRun(repo), testOwnerId, map[string]any{"serviceId": testCronJobId})

		assert.True(t, result.IsError)
		assert.Contains(t, text, "boom")
	})
}
