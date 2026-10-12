package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	workflowclient "github.com/render-oss/render-mcp-server/pkg/client/workflows"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/stretchr/testify/require"
)

func fixtureClient(t *testing.T, h http.HandlerFunc) *client.ClientWithResponses {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	c, e := client.NewClientWithResponses(s.URL, client.WithHTTPClient(s.Client()))
	require.NoError(t, e)
	return c
}
func invoke(t *testing.T, c *client.ClientWithResponses, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	return invokeContext(t, session.ContextWithWorkspace(t.Context(), "tea-a"), c, name, args)
}
func invokeContext(t *testing.T, ctx context.Context, c *client.ClientWithResponses, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	for _, tool := range WithLimits(Tools(c)) {
		if tool.Tool.Name == name {
			v, e := tool.Handler(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: name, Arguments: args}})
			require.NoError(t, e)
			return v
		}
	}
	t.Fatalf("missing tool %s", name)
	return nil
}
func resultText(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	require.Len(t, r.Content, 1)
	return r.Content[0].(mcp.TextContent).Text
}
func decodeResult[T any](t *testing.T, r *mcp.CallToolResult) T {
	t.Helper()
	require.False(t, r.IsError, resultText(t, r))
	var v T
	require.NoError(t, json.Unmarshal([]byte(resultText(t, r)), &v))
	return v
}

func TestListRequests(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
		args             map[string]any
		query            map[string]string
	}{
		{"list_workflows", "/workflows", `[{"workflow":{"id":"wfl-a","ownerId":"tea-a"},"cursor":"next"}]`, map[string]any{"name": "example", "environmentId": "env-a"}, map[string]string{"name": "example", "environmentId": "env-a"}},
		{"list_workflow_versions", "/workflowversions", `[{"workflowVersion":{"id":"wfv-a","workflowId":"wfl-a"},"cursor":"next"}]`, map[string]any{"workflowId": "wfl-a"}, map[string]string{"workflowID": "wfl-a"}},
		{"list_workflow_tasks", "/tasks", `[{"task":{"id":"tsk-a","workflowId":"wfl-a"},"cursor":"next"}]`, map[string]any{"workflowId": "wfl-a", "workflowVersionId": "wfv-a", "taskSlug": "example/task"}, map[string]string{"workflowId": "wfl-a", "workflowVersionId": "wfv-a", "taskSlug": "example/task"}},
		{"list_workflow_runs", "/task-runs", `[{"taskRun":{"id":"trn-a","taskId":"tsk-a","status":"failed"},"cursor":"next"}]`, map[string]any{"workflowId": "wfl-a", "workflowVersionId": "wfv-a", "taskSlug": "example/task", "rootTaskRunId": "trn-root"}, map[string]string{"workflowId": "wfl-a", "workflowVersionId": "wfv-a", "taskSlug": "example/task", "rootTaskRunId": "trn-root"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := 0
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				count++
				require.Equal(t, "GET", r.Method)
				require.Equal(t, tc.path, r.URL.Path)
				q := r.URL.Query()
				require.Equal(t, "tea-a", q.Get("ownerId"))
				for k, v := range tc.query {
					require.Equal(t, v, q.Get(k))
				}
				require.Equal(t, "2", q.Get("limit"))
				if count == 1 {
					require.Equal(t, "", q.Get("cursor"))
					fmt.Fprint(w, tc.body)
				} else {
					require.Equal(t, "next", q.Get("cursor"))
					fmt.Fprint(w, "null")
				}
			})
			tc.args["limit"] = float64(2)
			first := decodeResult[struct {
				Items      []json.RawMessage
				NextCursor *string
			}](t, invoke(t, c, tc.name, tc.args))
			require.Len(t, first.Items, 1)
			require.Equal(t, "next", *first.NextCursor)
			tc.args["cursor"] = *first.NextCursor
			second := invoke(t, c, tc.name, tc.args)
			require.False(t, second.IsError)
			require.JSONEq(t, `{"items":[],"nextCursor":null}`, resultText(t, second))
			require.Equal(t, 2, count)
		})
	}
}

func TestExactLookupsEnforceWorkspace(t *testing.T) {
	for _, tool := range []struct{ name, key, id, path string }{
		{"get_workflow", "workflowId", "wfl-a", "/workflows/wfl-a"},
		{"get_workflow_version", "workflowVersionId", "wfv-a", "/workflowversions/wfv-a"},
		{"get_workflow_task", "taskId", "tsk-a", "/tasks/tsk-a"},
		{"get_workflow_run", "taskRunId", "trn-a", "/task-runs/trn-a"},
		{"get_workflow_run_results", "taskRunId", "trn-a", "/task-runs/trn-a"},
	} {
		for _, owner := range []string{"tea-a", "tea-other", ""} {
			t.Run(tool.name+"/"+owner, func(t *testing.T) {
				c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, "GET", r.Method)
					switch r.URL.Path {
					case "/workflows/wfl-a":
						fmt.Fprintf(w, `{"id":"wfl-a","ownerId":%q}`, owner)
					case "/workflowversions/wfv-a":
						fmt.Fprint(w, `{"id":"wfv-a","workflowId":"wfl-a"}`)
					case "/tasks/tsk-a":
						fmt.Fprint(w, `{"id":"tsk-a","workflowId":"wfl-a","workflowVersionId":"wfv-a"}`)
					case "/task-runs/trn-a":
						fmt.Fprint(w, `{"id":"trn-a","taskId":"tsk-a","status":"failed","error":"PRIVATE_TASK_ERROR","input":["PRIVATE_INPUT"],"results":["PRIVATE_RESULT"],"attempts":[{"attempt":0,"status":"failed","error":"PRIVATE_ATTEMPT","results":[]}]}`)
					default:
						t.Errorf("unexpected path %s", r.URL.Path)
						w.WriteHeader(404)
					}
				})
				result := invoke(t, c, tool.name, map[string]any{tool.key: tool.id})
				require.Equal(t, owner != "tea-a", result.IsError)
				if owner != "tea-a" {
					require.NotContains(t, resultText(t, result), "PRIVATE")
				}
				if owner == "tea-a" && tool.name == "get_workflow_run" {
					text := resultText(t, result)
					require.Contains(t, text, "PRIVATE_TASK_ERROR")
					require.Contains(t, text, "PRIVATE_ATTEMPT")
					require.NotContains(t, text, "PRIVATE_INPUT")
					require.NotContains(t, text, "PRIVATE_RESULT")
					require.NotContains(t, text, `"results"`)
				}
			})
		}
	}
}

func TestInvalidArgumentsNeverReachAPI(t *testing.T) {
	var calls atomic.Int32
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) })
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"get_workflow", nil}, {"get_workflow", map[string]any{"workflowId": "  "}}, {"get_workflow", map[string]any{"workflowId": 1}},
		{"list_workflows", map[string]any{"limit": 1.5}}, {"list_workflows", map[string]any{"limit": 0.0}}, {"list_workflows", map[string]any{"limit": 101.0}}, {"list_workflows", map[string]any{"limit": math.Inf(1)}}, {"list_workflows", map[string]any{"limit": "20"}},
		{"list_workflow_runs", map[string]any{"status": "failed"}},
		{"list_workflow_tasks", map[string]any{"taskSlug": []string{"a"}}},
		{"get_workflow_run_results", map[string]any{"taskRunId": "trn-a", "offset": -1.0}},
		{"get_workflow_run_results", map[string]any{"taskRunId": "trn-a", "attempt": 0.5}},
		{"get_workflow_run_results", map[string]any{"taskRunId": "trn-a", "limit": 3.0}},
	} {
		r := invoke(t, c, tc.name, tc.args)
		require.True(t, r.IsError, tc.name)
	}
	require.Zero(t, calls.Load())
}

func TestBoundedResponsesAndSafeErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"unauthorized", "SENSITIVE", 401}, {"forbidden", "SENSITIVE", 403}, {"missing", "SENSITIVE", 404}, {"rate limited", "SENSITIVE", 429}, {"unavailable", "SENSITIVE", 503},
		{"malformed", "{SENSITIVE", 200}, {"trailing", `[] SENSITIVE`, 200}, {"oversized", strings.Repeat("x", maxResponseBytes+1), 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) })
			v := invoke(t, c, "list_workflows", nil)
			require.True(t, v.IsError)
			require.NotContains(t, resultText(t, v), "SENSITIVE")
		})
	}
}

func TestResultPagingPreservesJSON(t *testing.T) {
	raw := `[ {"large":9007199254740993,"text":"` + strings.Repeat("水🍃", 12000) + `","array":[[1,2],[]]} ]`
	var run runDetails
	require.NoError(t, json.Unmarshal([]byte(`{"id":"trn-a","taskId":"tsk-a","status":"completed","results":`+raw+`,"attempts":[{"attempt":2,"status":"failed","error":"expected","results":[9007199254740993]}]}`), &run))
	require.Len(t, run.Attempts, 1)
	require.Equal(t, 2, run.Attempts[0].Attempt)
	var content strings.Builder
	offset := 0
	hash := ""
	pages := 0
	for {
		p, e := pageResults(&run, nil, offset, 1024, hash)
		require.NoError(t, e)
		require.True(t, p.Ready)
		require.True(t, utf8.ValidString(p.Content))
		require.LessOrEqual(t, len(p.Content), 1024)
		content.WriteString(p.Content)
		pages++
		hash = p.SHA256
		if p.NextOffset == nil {
			break
		}
		offset = *p.NextOffset
	}
	require.Greater(t, pages, 2)
	require.JSONEq(t, raw, content.String())
	require.Contains(t, content.String(), "9007199254740993")
	attempt := 2
	p, e := pageResults(&run, &attempt, 0, 1024, "")
	require.NoError(t, e)
	require.Equal(t, `[9007199254740993]`, p.Content)
	require.Equal(t, workflowclient.Failed, p.Status)
	attempt = 0
	_, e = pageResults(&run, &attempt, 0, 1024, "")
	require.ErrorContains(t, e, "attempt not found")
	_, e = pageResults(&run, nil, 1, 1024, "")
	require.ErrorContains(t, e, "sha256")
	_, e = pageResults(&run, nil, 1, 1024, "changed")
	require.ErrorContains(t, e, "changed")
	_, e = pageResults(&run, nil, len(raw)+1, 1024, hash)
	require.ErrorContains(t, e, "offset")
	first, e := pageResults(&run, nil, 0, 1024, "")
	require.NoError(t, e)
	pos := strings.Index(content.String(), "水") + 1
	_, e = pageResults(&run, nil, pos, 1024, first.SHA256)
	require.ErrorContains(t, e, "UTF-8")
}

func TestNonterminalAndEmptyResults(t *testing.T) {
	for _, status := range []workflowclient.TaskRunStatus{workflowclient.Pending, workflowclient.Running, workflowclient.Paused, "future_status"} {
		run := &runDetails{Run: Run{TaskRun: workflowclient.TaskRun{Id: "trn-a", Status: status}}, Results: json.RawMessage(`["partial"]`)}
		p, e := pageResults(run, nil, 0, 1024, "")
		require.NoError(t, e)
		require.False(t, p.Ready)
		require.Empty(t, p.Content)
		require.Nil(t, p.NextOffset)
	}
	for _, raw := range []string{"null", "[]", `[[]]`, `[null]`} {
		run := &runDetails{Run: Run{TaskRun: workflowclient.TaskRun{Status: workflowclient.Canceled}}, Results: json.RawMessage(raw)}
		p, e := pageResults(run, nil, 0, 1024, "")
		require.NoError(t, e)
		require.True(t, p.Ready)
		require.Equal(t, raw, p.Content)
	}
}

func TestErrorTruncationAndOutputLimit(t *testing.T) {
	s := strings.Repeat("水", 5000)
	r := &runDetails{Run: Run{Error: &s, Attempts: []Attempt{{Error: &s}}}}
	v := runMetadata(r)
	require.True(t, v.ErrorTruncated)
	require.True(t, v.Attempts[0].ErrorTruncated)
	require.LessOrEqual(t, len(*v.Error), maxErrorBytes)
	require.True(t, utf8.ValidString(*v.Error))
	tool := readTool("large", "test", func(context.Context, mcp.CallToolRequest) (any, error) {
		return strings.Repeat("x", maxToolOutputBytes), nil
	})
	result, e := tool.Handler(t.Context(), mcp.CallToolRequest{})
	require.NoError(t, e)
	require.True(t, result.IsError)
	require.Contains(t, resultText(t, result), "256 KiB")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }
func TestDeadlineAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, e := client.NewClientWithResponses("https://api.example.test", client.WithHTTPClient(roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })))
		require.NoError(t, e)
		start := time.Now()
		v := invoke(t, c, "list_workflows", nil)
		require.True(t, v.IsError)
		require.Contains(t, resultText(t, v), "deadline exceeded")
		require.Equal(t, requestTimeout, time.Since(start))
		ctx, cancel := context.WithCancel(session.ContextWithWorkspace(t.Context(), "tea-a"))
		cancel()
		v = invokeContext(t, ctx, c, "list_workflows", nil)
		require.True(t, v.IsError)
		require.Contains(t, resultText(t, v), "canceled")
	})
}

func TestToolAnnotationsAndNames(t *testing.T) {
	tools := Tools(nil)
	require.Len(t, tools, 11)
	counts := make(map[string]int)
	for _, v := range tools {
		counts[v.Tool.Name]++
		write := v.Tool.Name == "create_workflow" || v.Tool.Name == "create_workflow_version"
		require.Equal(t, !write, *v.Tool.Annotations.ReadOnlyHint, v.Tool.Name)
		require.Equal(t, v.Tool.Name == "create_workflow_version", *v.Tool.Annotations.DestructiveHint, v.Tool.Name)
		require.Equal(t, !write, *v.Tool.Annotations.IdempotentHint, v.Tool.Name)
		require.Equal(t, write, *v.Tool.Annotations.OpenWorldHint, v.Tool.Name)
	}
	for _, name := range []string{
		"list_workflows", "get_workflow", "list_workflow_versions", "get_workflow_version",
		"list_workflow_tasks", "get_workflow_task", "list_workflow_runs", "get_workflow_run",
		"get_workflow_run_results", "create_workflow", "create_workflow_version",
	} {
		require.Equal(t, 1, counts[name], name)
	}
}

// Compile-time checks keep the adapter on generated API methods.
var _ workflowRepoClient = (*client.ClientWithResponses)(nil)
