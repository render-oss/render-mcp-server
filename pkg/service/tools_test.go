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
	"github.com/stretchr/testify/assert"
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
