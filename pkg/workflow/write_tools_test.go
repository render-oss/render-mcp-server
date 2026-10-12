package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	workflowclient "github.com/render-oss/render-mcp-server/pkg/client/workflows"
	"github.com/render-oss/render-mcp-server/pkg/fakes"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/stretchr/testify/require"
)

func writeContext(t *testing.T) context.Context {
	t.Helper()
	t.Setenv("RENDER_CONFIG_PATH", filepath.Join(t.TempDir(), "mcp-server.yaml"))
	ctx := session.ContextWithStdioSession(t.Context())
	require.NoError(t, session.FromContext(ctx).SetWorkspace(ctx, "tea-a"))
	return ctx
}
func createArgs() map[string]any {
	return map[string]any{"name": "sample", "repo": "https://github.com/example/workflow", "runtime": "python", "buildCommand": "pip install -r requirements.txt", "runCommand": "python main.py"}
}
func writeResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}
func callWrite(t *testing.T, ctx context.Context, tool server.ServerTool, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := WithLimits([]server.ServerTool{tool})[0].Handler(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: tool.Tool.Name, Arguments: args}})
	require.NoError(t, err)
	return result
}

func TestCreateWorkflowRequestEncoding(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(fmt.Sprint(full), func(t *testing.T) {
			ctx := writeContext(t)
			calls := 0
			args := createArgs()
			if full {
				args["branch"] = "release"
				args["rootDir"] = "jobs"
				args["region"] = "frankfurt"
				args["autoDeploy"] = "checksPass"
				args["envVars"] = []any{map[string]any{"key": "MODE", "value": "test"}, map[string]any{"key": "EMPTY", "value": ""}}
			}
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, "POST", r.Method)
				require.Equal(t, "/workflows", r.URL.Path)
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, "tea-a", body["ownerId"])
				require.Equal(t, args["name"], body["name"])
				require.Equal(t, args["runCommand"], body["runCommand"])
				require.NotContains(t, body, "autoDeploy")
				require.NotContains(t, body, "runtime")
				require.NotContains(t, body, "plan")
				build := body["buildConfig"].(map[string]any)
				for _, key := range []string{"repo", "runtime", "buildCommand"} {
					require.Equal(t, args[key], build[key])
				}
				if full {
					require.Equal(t, "checksPass", body["autoDeployTrigger"])
					require.Equal(t, "frankfurt", body["region"])
					require.Equal(t, args["envVars"], body["envVars"])
					require.Equal(t, "release", build["branch"])
					require.Equal(t, "jobs", build["rootDir"])
				} else {
					require.Equal(t, "oregon", body["region"])
					for _, key := range []string{"autoDeployTrigger", "envVars"} {
						require.NotContains(t, body, key)
					}
					require.NotContains(t, build, "branch")
					require.NotContains(t, build, "rootDir")
				}
				w.WriteHeader(201)
				io.WriteString(w, `{"id":"wfl-created","ownerId":"tea-a","name":"sample"}`)
			})
			result := callWrite(t, ctx, createWorkflow(NewRepo(c)), args)
			value := decodeResult[workflowclient.Workflow](t, result)
			require.Equal(t, "wfl-created", value.Id)
			require.Equal(t, 1, calls, "creation must not trigger a second deployment")
		})
	}
}

func TestWorkflowWritesRejectInvalidInputsBeforeClientCalls(t *testing.T) {
	cases := []struct {
		name, key string
		value     any
		remove    bool
	}{
		{"blank repo", "repo", " ", false}, {"docker runtime", "runtime", "docker", false}, {"rust runtime", "runtime", "rust", false},
		{"invalid runtime type", "runtime", 1, false}, {"invalid region", "region", "moon", false}, {"old autodeploy enum", "autoDeploy", "yes", false},
		{"invalid branch", "branch", 3, false}, {"blank root", "rootDir", " ", false}, {"unknown input", "plan", "starter", false},
		{"owner cannot be supplied", "ownerId", "tea-other", false}, {"null env", "envVars", nil, false},
		{"env object not array", "envVars", map[string]any{}, false},
		{"env unknown field", "envVars", []any{map[string]any{"key": "KEY", "value": "secret", "fromDatabase": "db"}}, false},
		{"env generated unsupported", "envVars", []any{map[string]any{"key": "KEY", "generateValue": true}}, false},
		{"env wrong value", "envVars", []any{map[string]any{"key": "KEY", "value": 123}}, false},
		{"env blank key", "envVars", []any{map[string]any{"key": " ", "value": "secret"}}, false},
		{"env duplicate", "envVars", []any{map[string]any{"key": "KEY", "value": "a"}, map[string]any{"key": "KEY", "value": "b"}}, false},
		{"oversize", "buildCommand", strings.Repeat("x", maxWriteRequestBytes), false},
	}
	for _, key := range []string{"name", "repo", "runtime", "buildCommand", "runCommand"} {
		cases = append(cases, struct {
			name, key string
			value     any
			remove    bool
		}{"missing " + key, key, nil, true})
		cases = append(cases, struct {
			name, key string
			value     any
			remove    bool
		}{"blank " + key, key, " ", false})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := writeContext(t)
			fake := &fakes.FakeWorkflowRepoClient{}
			args := createArgs()
			if tc.remove {
				delete(args, tc.key)
			} else {
				args[tc.key] = tc.value
			}
			result := callWrite(t, ctx, createWorkflow(NewRepo(fake)), args)
			require.True(t, result.IsError, resultText(t, result))
			require.Zero(t, fake.CreateWorkflowCallCount())
			require.Zero(t, fake.GetWorkflowCallCount())
			require.NotContains(t, resultText(t, result), "secret")
		})
	}
	for _, args := range []map[string]any{{}, {"workflowId": " "}, {"workflowId": "wfl-a", "commit": " "}, {"workflowId": "wfl-a", "commit": false}, {"workflowId": "wfl-a", "clearCache": true}} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			fake := &fakes.FakeWorkflowRepoClient{}
			result := callWrite(t, writeContext(t), createWorkflowVersion(NewRepo(fake)), args)
			require.True(t, result.IsError)
			require.Zero(t, fake.GetWorkflowCallCount())
			require.Zero(t, fake.CreateWorkflowVersionCallCount())
		})
	}
}

func TestAdvertisedWorkflowValuesAreAccepted(t *testing.T) {
	tool := createWorkflow(NewRepo(nil)).Tool
	for _, field := range []string{"runtime", "region", "autoDeploy"} {
		schema := tool.InputSchema.Properties[field].(map[string]any)
		data, err := json.Marshal(schema["enum"])
		require.NoError(t, err)
		var values []string
		require.NoError(t, json.Unmarshal(data, &values))
		require.NotEmpty(t, values)
		for _, value := range values {
			args := createArgs()
			args[field] = value
			_, err := createWorkflowInput(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
			require.NoError(t, err)
		}
	}
	args := createArgs()
	args["envVars"] = []any{}
	body, err := createWorkflowInput(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
	require.NoError(t, err)
	data, err := json.Marshal(body)
	require.NoError(t, err)
	require.Contains(t, string(data), `"envVars":[]`)
}

func TestWriteRepoChecksWorkspaceBeforeMutation(t *testing.T) {
	ctx := writeContext(t)
	fake := &fakes.FakeWorkflowRepoClient{}
	_, err := NewRepo(fake).Create(ctx, client.CreateWorkflowJSONRequestBody{OwnerId: "tea-other"})
	require.Error(t, err)
	require.Zero(t, fake.CreateWorkflowCallCount())
	for _, body := range []string{`{"id":"wfl-a","ownerId":"tea-other"}`, `{"id":"wfl-wrong","ownerId":"tea-a"}`, `null`} {
		fake := &fakes.FakeWorkflowRepoClient{}
		fake.GetWorkflowReturns(writeResponse(200, body), nil)
		_, err := NewRepo(fake).CreateVersion(ctx, client.CreateWorkflowVersionJSONRequestBody{WorkflowId: "wfl-a"})
		require.Error(t, err)
		require.Equal(t, 1, fake.GetWorkflowCallCount())
		require.Zero(t, fake.CreateWorkflowVersionCallCount())
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	fake = &fakes.FakeWorkflowRepoClient{}
	_, err = NewRepo(fake).Create(canceled, client.CreateWorkflowJSONRequestBody{OwnerId: "tea-a"})
	require.Error(t, err)
	require.Zero(t, fake.CreateWorkflowCallCount())
}

func TestCreateVersionRequestsAndAcceptedResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		commit bool
	}{
		{"backend created", 201, `{"id":"wfv-a","workflowId":"wfl-a","status":"building"}`, false},
		{"chosen commit", 201, `{"id":"wfv-a","workflowId":"wfl-a","status":"created"}`, true},
		{"documented accepted empty", 202, "", false},
		{"accepted version", 202, `{"id":"wfv-a","workflowId":"wfl-a","status":"registering"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := writeContext(t)
			var calls []string
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				if r.Method == "GET" {
					require.Equal(t, "/workflows/wfl-a", r.URL.Path)
					io.WriteString(w, `{"id":"wfl-a","ownerId":"tea-a"}`)
					return
				}
				require.Equal(t, "/workflowversions", r.URL.Path)
				require.Equal(t, []string{"GET /workflows/wfl-a", "POST /workflowversions"}, calls)
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, "wfl-a", body["workflowId"])
				if tc.commit {
					require.Equal(t, "abc123", body["commit"])
				} else {
					require.NotContains(t, body, "commit")
				}
				require.NotContains(t, body, "clearCache")
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			args := map[string]any{"workflowId": "wfl-a"}
			if tc.commit {
				args["commit"] = "abc123"
			}
			value := decodeResult[VersionCreation](t, callWrite(t, ctx, createWorkflowVersion(NewRepo(c)), args))
			require.True(t, value.Accepted)
			require.Equal(t, "wfl-a", value.WorkflowID)
			require.NotEmpty(t, value.NextStep)
			if tc.body == "" {
				require.Nil(t, value.Version)
			} else {
				require.Equal(t, "wfv-a", value.Version.Id)
				require.NotEqual(t, workflowclient.Ready, value.Version.Status)
			}
			require.Len(t, calls, 2)
		})
	}
}

func TestWriteFailuresAreBoundedRedactedAndNeverRetried(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		err    error
	}{
		{"unauthorized", 401, `secret-env-value`, nil}, {"forbidden", 403, `secret-env-value`, nil},
		{"rate limited", 429, `secret-env-value`, nil}, {"server failure", 500, `secret-env-value`, nil},
		{"wrong success status", 200, `secret-env-value`, nil}, {"empty created", 201, "", nil},
		{"malformed", 201, `secret-env-value`, nil}, {"null", 201, `null`, nil},
		{"wrong identity", 201, `{"id":"wfv-other","workflowId":"wfl-other","ownerId":"tea-other"}`, nil},
		{"oversized", 201, strings.Repeat("x", maxResponseBytes+1), nil},
		{"lost response", 0, "", errors.New("transport leaked secret-env-value")},
		{"canceled", 0, "", context.Canceled}, {"deadline", 0, "", context.DeadlineExceeded},
	}
	for _, tc := range cases {
		for _, version := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/version=%t", tc.name, version), func(t *testing.T) {
				ctx := writeContext(t)
				fake := &fakes.FakeWorkflowRepoClient{}
				var response *http.Response
				if tc.status != 0 {
					response = writeResponse(tc.status, tc.body)
				}
				var result *mcp.CallToolResult
				if version {
					fake.GetWorkflowReturns(writeResponse(200, `{"id":"wfl-a","ownerId":"tea-a"}`), nil)
					fake.CreateWorkflowVersionReturns(response, tc.err)
					result = callWrite(t, ctx, createWorkflowVersion(NewRepo(fake)), map[string]any{"workflowId": "wfl-a"})
					require.Equal(t, 1, fake.CreateWorkflowVersionCallCount())
					require.Contains(t, resultText(t, result), "list_workflow_versions")
				} else {
					fake.CreateWorkflowReturns(response, tc.err)
					result = callWrite(t, ctx, createWorkflow(NewRepo(fake)), createArgs())
					require.Equal(t, 1, fake.CreateWorkflowCallCount())
					require.Contains(t, resultText(t, result), "list_workflows")
				}
				require.True(t, result.IsError)
				require.NotContains(t, resultText(t, result), "secret-env-value")
				require.Contains(t, resultText(t, result), "do not automatically repeat")
			})
		}
	}
}

func TestWritesRetainDeadlineWithoutReplaying(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := writeContext(t)
		fake := &fakes.FakeWorkflowRepoClient{}
		fake.CreateWorkflowStub = func(ctx context.Context, _ client.CreateWorkflowJSONRequestBody, _ ...client.RequestEditorFn) (*http.Response, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		started := time.Now()
		result := callWrite(t, ctx, createWorkflow(NewRepo(fake)), createArgs())
		require.True(t, result.IsError)
		require.Equal(t, requestTimeout, time.Since(started))
		require.Equal(t, 1, fake.CreateWorkflowCallCount())
		require.Contains(t, resultText(t, result), "outcome unknown")
	})
}
