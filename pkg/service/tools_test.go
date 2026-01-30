package service

import (
	"context"
	"fmt"
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

func TestUpdateEnvVarsTool(t *testing.T) {
	serviceId := "srv-123456"
	existingEnvVars := []*client.EnvVar{
		{Key: "KEY1", Value: "old_value1"},
		{Key: "KEY2", Value: "old_value2"},
	}
	newEnvVars := []client.EnvVarInput{
		envVarInput("KEY1", "new_value1"),
		envVarInput("KEY3", "new_value3"),
	}
	expectedResponseIncludes := "Environment variables updated. A new deploy has been triggered to pick up the changes."
	expectedDeployResponse := "create deploy response"
	sensitiveInfo := "sensitive information"

	tests := []struct {
		name            string
		replace         bool
		expectedEnvVars []client.EnvVarInput
	}{
		{
			name:            "Replace existing env vars, does not include sensitive info",
			replace:         true,
			expectedEnvVars: newEnvVars,
		},
		{
			name:    "Merge with existing env vars, does not include sensitive info",
			replace: false,
			expectedEnvVars: []client.EnvVarInput{
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

			fakeClient.RetrieveServiceWithResponseReturns(&client.RetrieveServiceResponse{
				JSON200: &client.Service{},
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

			fakeClient.CreateDeployWithResponseReturns(&client.CreateDeployResponse{
				HTTPResponse: &http.Response{
					StatusCode: 201,
				},
				Body: []byte(expectedDeployResponse),
			}, nil)

			request := mcp.CallToolRequest{}
			request.Params.Arguments = map[string]interface{}{
				"serviceId": serviceId,
				"replace":   tt.replace,
				"envVars":   envVarInputsAsParams(newEnvVars),
			}

			tool := updateEnvVars(repo)
			result, err := tool.Handler(context.Background(), request)

			assert.NoError(t, err)
			assert.NotNil(t, result)

			for _, content := range result.Content {
				if textContent, ok := content.(mcp.TextContent); ok {
					assert.Contains(t, textContent.Text, expectedResponseIncludes)
					assert.Contains(t, textContent.Text, expectedDeployResponse)
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

func envVarInput(key, value string) client.EnvVarInput {
	var input client.EnvVarInput
	input.FromEnvVarKeyValue(client.EnvVarKeyValue{
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

func envVarInputsAsParams(envVars []client.EnvVarInput) []interface{} {
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
		plan         string
		expectedPlan *client.PaidPlan
	}{
		{
			name:         "Create web service with free plan",
			plan:         "free",
			expectedPlan: pointers.From(client.PaidPlan("free")),
		},
		{
			name:         "Create web service with starter plan",
			plan:         "starter",
			expectedPlan: pointers.From(client.PaidPlanStarter),
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

			ctx := createTestContext(ownerId)

			request := mcp.CallToolRequest{}
			request.Params.Arguments = map[string]any{
				"name":         serviceName,
				"runtime":      runtime,
				"buildCommand": buildCommand,
				"startCommand": startCommand,
				"plan":         tt.plan,
			}

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
			ctx := createTestContext(ownerId)

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
func createTestContext(workspaceID string) context.Context {
	ctx := session.ContextWithStdioSession(context.Background())
	sess := session.FromContext(ctx)
	sess.SetWorkspace(ctx, workspaceID)
	return ctx
}

func TestMergeEnvVars(t *testing.T) {
	tests := []struct {
		name          string
		oldEnvVars    []*client.EnvVar
		newEnvVars    []client.EnvVarInput
		expected      []client.EnvVarInput
		expectedError string
	}{
		{
			name:       "Empty old env vars, non-empty new env vars",
			oldEnvVars: []*client.EnvVar{},
			newEnvVars: []client.EnvVarInput{
				envVarInput("KEY1", "value1"),
				envVarInput("KEY2", "value2"),
			},
			expected: []client.EnvVarInput{
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
			newEnvVars: []client.EnvVarInput{},
			expected: []client.EnvVarInput{
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
			newEnvVars: []client.EnvVarInput{
				envVarInput("KEY3", "value3"),
				envVarInput("KEY4", "value4"),
			},
			expected: []client.EnvVarInput{
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
			newEnvVars: []client.EnvVarInput{
				envVarInput("KEY1", "new_value1"),
				envVarInput("KEY2", "new_value2"),
				envVarInput("KEY4", "value4"),
			},
			expected: []client.EnvVarInput{
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
			newEnvVars: []client.EnvVarInput{
				envVarInput("KEY1", "new_value1"),
				envVarInput("KEY2", "new_value2"),
			},
			expected: []client.EnvVarInput{
				envVarInput("KEY1", "new_value1"),
				envVarInput("KEY2", "new_value2"),
			},
		},
		{
			name:       "Empty both old and new env vars",
			oldEnvVars: []*client.EnvVar{},
			newEnvVars: []client.EnvVarInput{},
			expected:   []client.EnvVarInput{},
		},
		{
			name: "Case sensitivity in keys",
			oldEnvVars: []*client.EnvVar{
				{Key: "key", Value: "value"},
				{Key: "KEY", Value: "VALUE"},
			},
			newEnvVars: []client.EnvVarInput{
				envVarInput("Key", "newValue"),
			},
			expected: []client.EnvVarInput{
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
			newEnvVars: []client.EnvVarInput{
				envVarInput("API_KEY", "abcd1234"),
			},
			expected: []client.EnvVarInput{
				envVarInput("API_KEY", "abcd1234"),
				envVarInput("DATABASE_URL", "postgres://user:pass@localhost:5432/db"),
			},
		},
		{
			name: "Empty values",
			oldEnvVars: []*client.EnvVar{
				{Key: "EMPTY_KEY", Value: ""},
			},
			newEnvVars: []client.EnvVarInput{
				envVarInput("ANOTHER_EMPTY", ""),
			},
			expected: []client.EnvVarInput{
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
