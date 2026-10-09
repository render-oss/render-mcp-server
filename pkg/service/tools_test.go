package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	envvar "github.com/render-oss/render-mcp-server/pkg/client/envvar"
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

func TestCreateCronJobScheduleValidation(t *testing.T) {
	ctx := createTestContext(t, "own-123")

	valid := map[string]any{
		"name":         "test-service",
		"runtime":      "node",
		"buildCommand": "npm install",
		"startCommand": "npm start",
		"schedule":     "*/15 * * * *",
	}
	request := mcp.CallToolRequest{}
	request.Params.Arguments = valid
	_, err := createValidatedCronJobRequest(ctx, request)
	require.NoError(t, err)

	for _, schedule := range []string{"every day", "99 99 * * *", "0 0 * *", "*/0 * * * *"} {
		args := map[string]any{
			"name":         "test-service",
			"runtime":      "node",
			"buildCommand": "npm install",
			"startCommand": "npm start",
			"schedule":     schedule,
		}
		request := mcp.CallToolRequest{}
		request.Params.Arguments = args
		_, err := createValidatedCronJobRequest(ctx, request)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid schedule expression")
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
