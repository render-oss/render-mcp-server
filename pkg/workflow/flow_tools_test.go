package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/fakes"
	"github.com/stretchr/testify/require"
)

type flowFixture struct {
	owner, runStatus, runResults string
	flows                        string
	steps                        string
	startStatus                  int
	startBody                    string
	writes                       atomic.Int32
	lastStart                    atomic.Value
}

func newFlowFixture() *flowFixture {
	return &flowFixture{
		owner: "tea-a", runStatus: "succeeded", runResults: `[12345678901234567890,"é"]`,
		flows:       `[{"flow":{"id":"flw-a","name":"hello","workflowId":"wfl-a","workflowVersionId":"wfv-a"},"cursor":"next"}]`,
		steps:       `{"steps":[{"seq":0,"stepKey":"k","type":"detached","status":"succeeded","childTaskRunId":"trn-c","startedAt":"2026-10-30T00:00:00Z"}],"cursor":"next","hasMore":true}`,
		startStatus: http.StatusAccepted, startBody: `{"id":"frn-new","flowId":"flw-a","status":"pending"}`,
	}
}

func (f *flowFixture) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /workflows/wfl-a":
			fmt.Fprintf(w, `{"id":"wfl-a","ownerId":%q,"slug":"my-wf"}`, f.owner)
		case "GET /flows/flw-a":
			fmt.Fprint(w, `{"id":"flw-a","name":"hello","workflowId":"wfl-a"}`)
		case "GET /flows":
			require.Equal(t, "tea-a", r.URL.Query().Get("ownerId"))
			fmt.Fprint(w, f.flows)
		case "GET /flow-runs":
			require.Equal(t, "tea-a", r.URL.Query().Get("ownerId"))
			fmt.Fprintf(w, `[{"flowRun":{"id":"frn-a","flowId":"flw-a","status":%q},"cursor":"next"}]`, f.runStatus)
		case "GET /flow-runs/frn-a":
			fmt.Fprintf(w, `{"id":"frn-a","flowId":"flw-a","status":%q,"input":["secret"],"results":%s,"attempts":[{"attempt":0,"status":%q,"error":%q}],"error":"boom"}`, f.runStatus, f.runResults, f.runStatus, strings.Repeat("x", maxErrorBytes+10))
		case "GET /flow-runs/frn-a/steps":
			fmt.Fprint(w, f.steps)
		case "GET /flow-runs/frn-a/steps/2":
			fmt.Fprint(w, `{"seq":2,"stepKey":"k","type":"inline","status":"succeeded","startedAt":"2026-10-30T00:00:00Z","input":{"password":"secret"},"result":{"n":12345678901234567890}}`)
		case "POST /flow-runs":
			f.writes.Add(1)
			b, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			f.lastStart.Store(string(b))
			w.WriteHeader(f.startStatus)
			fmt.Fprint(w, f.startBody)
		case "DELETE /flow-runs/frn-a":
			f.writes.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestFlowLookupsEnforceWorkspace(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"get_flow", map[string]any{"flowId": "flw-a"}},
		{"get_flow_run", map[string]any{"flowRunId": "frn-a"}},
		{"get_flow_run_results", map[string]any{"flowRunId": "frn-a"}},
		{"list_flow_run_steps", map[string]any{"flowRunId": "frn-a"}},
		{"get_flow_run_step", map[string]any{"flowRunId": "frn-a", "seq": float64(2)}},
		{"list_flows", map[string]any{}},
		{"start_flow_run", map[string]any{"flow": "flw-a"}},
		{"cancel_flow_run", map[string]any{"flowRunId": "frn-a"}},
	} {
		for _, owner := range []string{"tea-a", "tea-other"} {
			t.Run(tc.name+"/"+owner, func(t *testing.T) {
				f := newFlowFixture()
				f.owner = owner
				if tc.name == "cancel_flow_run" {
					f.runStatus = "running"
				}
				result := invoke(t, fixtureClient(t, f.handler(t)), tc.name, tc.args)
				if tc.name == "list_flows" && owner != "tea-a" {
					got := decodeResult[struct{ Items []FlowSummary }](t, result)
					require.Empty(t, got.Items)
					return
				}
				require.Equal(t, owner != "tea-a", result.IsError, resultText(t, result))
				if owner != "tea-a" {
					require.Zero(t, f.writes.Load(), "a write reached the API for another workspace")
				}
			})
		}
	}
}

func TestListFlowsAddsSlug(t *testing.T) {
	f := newFlowFixture()
	got := decodeResult[struct {
		Items      []map[string]any
		NextCursor *string
	}](t, invoke(t, fixtureClient(t, f.handler(t)), "list_flows", map[string]any{}))
	require.Len(t, got.Items, 1)
	require.Equal(t, "my-wf/hello", got.Items[0]["slug"])
	require.Equal(t, "next", *got.NextCursor)
}

func TestListFlowsSurvivesWorkflowLookupFailure(t *testing.T) {
	for _, failure := range []string{"missing", "rate limited", "wrong workspace", "wrong identity"} {
		t.Run(failure, func(t *testing.T) {
			var lookups atomic.Int32
			f := newFlowFixture()
			f.flows = `[{"flow":{"id":"flw-bad1","name":"bad1","workflowId":"wfl-bad"},"cursor":"c1"},{"flow":{"id":"flw-a","name":"hello","workflowId":"wfl-a"},"cursor":"c2"},{"flow":{"id":"flw-bad2","name":"bad2","workflowId":"wfl-bad"},"cursor":"last"}]`
			base := f.handler(t)
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/workflows/wfl-bad" {
					base(w, r)
					return
				}
				lookups.Add(1)
				switch failure {
				case "missing":
					w.WriteHeader(http.StatusNotFound)
				case "rate limited":
					w.WriteHeader(http.StatusTooManyRequests)
				case "wrong workspace":
					fmt.Fprint(w, `{"id":"wfl-bad","ownerId":"tea-other","slug":"private"}`)
				case "wrong identity":
					fmt.Fprint(w, `{"id":"wfl-other","ownerId":"tea-a","slug":"wrong"}`)
				}
			})
			got := decodeResult[struct {
				Items      []FlowSummary
				NextCursor *string
			}](t, invoke(t, c, "list_flows", map[string]any{}))
			require.Len(t, got.Items, 1)
			require.Equal(t, "flw-a", got.Items[0].Id)
			require.Equal(t, "my-wf/hello", *got.Items[0].Slug)
			require.Equal(t, "last", *got.NextCursor)
			require.EqualValues(t, 1, lookups.Load())
		})
	}
}

func TestListFlowsPropagatesContextErrors(t *testing.T) {
	for _, contextErr := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(contextErr.Error(), func(t *testing.T) {
			fake := &fakes.FakeWorkflowRepoClient{}
			fake.ListFlowsWithResponseReturns(parse(client.ParseListFlowsResponse, writeResponse(200, `[{"flow":{"id":"flw-a","workflowId":"wfl-a"},"cursor":"next"}]`), nil))
			fake.GetWorkflowWithResponseReturns(nil, contextErr)
			_, _, err := NewRepo(fake).listFlowSummaries(writeContext(t), &client.ListFlowsParams{})
			require.ErrorIs(t, err, contextErr)
		})
	}
}

func TestFlowRunActionsLookUpRunDirectly(t *testing.T) {
	for _, name := range []string{"list_flow_run_steps", "get_flow_run_step", "cancel_flow_run"} {
		t.Run(name, func(t *testing.T) {
			f := newFlowFixture()
			f.runStatus = "running"
			base := f.handler(t)
			var detailCalls, listCalls atomic.Int32
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/flow-runs/frn-a" {
					detailCalls.Add(1)
				}
				if r.URL.Path == "/flow-runs" {
					listCalls.Add(1)
				}
				base(w, r)
			})
			args := map[string]any{"flowRunId": "frn-a"}
			if name == "get_flow_run_step" {
				args["seq"] = float64(2)
			}
			result := invoke(t, c, name, args)
			require.False(t, result.IsError, resultText(t, result))
			require.EqualValues(t, 1, detailCalls.Load())
			require.Zero(t, listCalls.Load())
		})
	}
}

func TestFlowRunActionsSurviveOversizedResults(t *testing.T) {
	for _, name := range []string{"list_flow_run_steps", "get_flow_run_step", "cancel_flow_run"} {
		t.Run(name, func(t *testing.T) {
			f := newFlowFixture()
			f.runStatus = "running"
			f.runResults = `["` + strings.Repeat("x", maxResponseBytes) + `"]`
			base := f.handler(t)
			var detailCalls, listCalls atomic.Int32
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/flow-runs/frn-a" {
					detailCalls.Add(1)
				}
				if r.URL.Path == "/flow-runs" {
					listCalls.Add(1)
					require.Equal(t, "tea-a", r.URL.Query().Get("ownerId"))
					require.Equal(t, "100", r.URL.Query().Get("limit"))
					if r.URL.Query().Get("cursor") == "" {
						fmt.Fprint(w, `[{"flowRun":{"id":"frn-other","flowId":"flw-a","status":"succeeded"},"cursor":"next"}]`)
						return
					}
					require.Equal(t, "next", r.URL.Query().Get("cursor"))
				}
				base(w, r)
			})
			args := map[string]any{"flowRunId": "frn-a"}
			if name == "get_flow_run_step" {
				args["seq"] = float64(2)
			}
			result := invoke(t, c, name, args)
			require.False(t, result.IsError, resultText(t, result))
			require.EqualValues(t, 1, detailCalls.Load())
			require.EqualValues(t, 2, listCalls.Load())
			if name == "cancel_flow_run" {
				require.EqualValues(t, 1, f.writes.Load())
			} else {
				require.Zero(t, f.writes.Load())
			}
			result = invoke(t, c, "get_flow_run_results", map[string]any{"flowRunId": "frn-a"})
			require.True(t, result.IsError)
			require.Contains(t, resultText(t, result), "exceeds 16 MiB")
		})
	}
}

func TestFlowRunActionLookupStops(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cursors []string
		want    string
	}{
		{"not found", []string{""}, "not found"},
		{"empty page", nil, "not found"},
		{"cursor cycle", []string{"a", "b", "a"}, "cursor"},
		{"missing flow identity", []string{"target"}, "invalid flow run identity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/flow-runs/frn-a" {
					fmt.Fprintf(w, `{"id":"frn-a","flowId":"flw-a","results":["%s"]}`, strings.Repeat("x", maxResponseBytes))
					return
				}
				require.Equal(t, "GET /flow-runs", r.Method+" "+r.URL.Path)
				i := int(calls.Add(1)) - 1
				if len(tc.cursors) == 0 {
					fmt.Fprint(w, `[]`)
					return
				}
				require.Less(t, i, len(tc.cursors))
				if tc.cursors[i] == "target" {
					fmt.Fprint(w, `[{"flowRun":{"id":"frn-a","status":"running"},"cursor":"end"}]`)
					return
				}
				fmt.Fprintf(w, `[{"flowRun":{"id":"frn-other","flowId":"flw-a","status":"running"},"cursor":%q}]`, tc.cursors[i])
			})
			result := invoke(t, c, "cancel_flow_run", map[string]any{"flowRunId": "frn-a"})
			require.True(t, result.IsError)
			require.Contains(t, resultText(t, result), tc.want)
			require.EqualValues(t, max(1, len(tc.cursors)), calls.Load())
		})
	}
}

func TestFlowRunActionLookupStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(writeContext(t))
	defer cancel()
	fake := &fakes.FakeWorkflowRepoClient{}
	fake.GetFlowRunWithResponseReturns(nil, errResponseTooLarge)
	fake.ListFlowRunsWithResponseCalls(func(context.Context, *client.ListFlowRunsParams, ...client.RequestEditorFn) (*client.ListFlowRunsResponse, error) {
		cancel()
		return parse(client.ParseListFlowRunsResponse, writeResponse(200, `[{"flowRun":{"id":"frn-other","flowId":"flw-a"},"cursor":"next"}]`), nil)
	})
	_, err := NewRepo(fake).getFlowRunSummary(ctx, "frn-a")
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, fake.ListFlowRunsWithResponseCallCount())
	require.Equal(t, 1, fake.GetFlowRunWithResponseCallCount())
}

func TestFlowRunActionLookupDoesNotScanOnOtherErrors(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET /flow-runs/frn-a", r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})
	result := invoke(t, c, "cancel_flow_run", map[string]any{"flowRunId": "frn-a"})
	require.True(t, result.IsError)
	require.Contains(t, resultText(t, result), "HTTP 404")
}

func TestListFlowRunsFilters(t *testing.T) {
	var query atomic.Value
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/flow-runs", r.URL.Path)
		query.Store(r.URL.Query())
		fmt.Fprint(w, `[{"flowRun":{"id":"frn-a","flowId":"flw-a","status":"failed"},"cursor":"next"}]`)
	})
	result := invoke(t, c, "list_flow_runs", map[string]any{"workflowId": "wfl-a", "flowSlug": "my-wf/hello", "status": "failed"})
	require.False(t, result.IsError, resultText(t, result))
	q := query.Load().(url.Values)
	require.Equal(t, []string{"tea-a"}, q["ownerId"])
	require.Equal(t, []string{"wfl-a"}, q["workflowId"])
	require.Equal(t, []string{"my-wf/hello"}, q["flowSlug"])
	require.Equal(t, []string{"failed"}, q["status"])

	for _, s := range flowRunStatuses(Options{}) {
		result := invoke(t, c, "list_flow_runs", map[string]any{"status": s})
		require.False(t, result.IsError, s)
	}
	calls := 0
	c = fixtureClient(t, func(http.ResponseWriter, *http.Request) { calls++ })
	result = invoke(t, c, "list_flow_runs", map[string]any{"status": "done"})
	require.True(t, result.IsError)
	require.Contains(t, resultText(t, result), "status must be one of")
	require.Zero(t, calls)
}

func TestFlowRunOmitsInputAndBoundsErrors(t *testing.T) {
	f := newFlowFixture()
	text := resultText(t, invoke(t, fixtureClient(t, f.handler(t)), "get_flow_run", map[string]any{"flowRunId": "frn-a"}))
	require.NotContains(t, text, "secret")
	require.NotContains(t, text, "12345678901234567890")
	run := decodeResult[FlowRun](t, invoke(t, fixtureClient(t, f.handler(t)), "get_flow_run", map[string]any{"flowRunId": "frn-a"}))
	require.Equal(t, "boom", *run.Error)
	require.Len(t, run.Attempts, 1)
	require.True(t, run.Attempts[0].ErrorTruncated)
	require.Len(t, *run.Attempts[0].Error, maxErrorBytes)
}

func TestFlowRunResultsPreserveJSON(t *testing.T) {
	f := newFlowFixture()
	p := decodeResult[flowResultPage](t, invoke(t, fixtureClient(t, f.handler(t)), "get_flow_run_results", map[string]any{"flowRunId": "frn-a"}))
	require.True(t, p.Ready)
	require.Equal(t, "frn-a", p.FlowRunID)
	require.Equal(t, `[12345678901234567890,"é"]`, p.Content)
	require.Nil(t, p.NextOffset)

	f.runStatus = "running"
	p = decodeResult[flowResultPage](t, invoke(t, fixtureClient(t, f.handler(t)), "get_flow_run_results", map[string]any{"flowRunId": "frn-a"}))
	require.False(t, p.Ready)
	require.Empty(t, p.Content)
}

func TestFlowRunSteps(t *testing.T) {
	f := newFlowFixture()
	c := fixtureClient(t, f.handler(t))
	steps := decodeResult[struct {
		Items      []map[string]any
		NextCursor *string
	}](t, invoke(t, c, "list_flow_run_steps", map[string]any{"flowRunId": "frn-a"}))
	require.Len(t, steps.Items, 1)
	require.Equal(t, "trn-c", steps.Items[0]["childTaskRunId"])
	require.Equal(t, "next", *steps.NextCursor)

	text := resultText(t, invoke(t, c, "get_flow_run_step", map[string]any{"flowRunId": "frn-a", "seq": float64(2)}))
	require.NotContains(t, text, "secret")
	require.Contains(t, text, `"result":{"n":12345678901234567890}`)
	require.NotContains(t, text, `"attempts"`)

	result := invoke(t, c, "get_flow_run_step", map[string]any{"flowRunId": "frn-a"})
	require.True(t, result.IsError)
	require.Contains(t, resultText(t, result), "list_flow_run_steps")
}

func TestGetFlowRunStepPreservesResult(t *testing.T) {
	for _, tc := range []struct {
		name, status, result string
	}{
		{"null", "succeeded", "null"},
		{"large integer", "succeeded", `{"n":12345678901234567890}`},
		{"false", "succeeded", "false"},
		{"zero", "succeeded", "0"},
		{"running", "running", ""},
		{"failed", "failed", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFlowFixture()
			base := f.handler(t)
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/flow-runs/frn-a/steps/2" {
					base(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				result := ""
				if tc.result != "" {
					result = `,"result":` + tc.result
				}
				fmt.Fprintf(w, `{"seq":2,"stepKey":"k","type":"inline","status":%q,"input":{"password":"secret"}%s}`, tc.status, result)
			})
			step := decodeResult[map[string]json.RawMessage](t, invoke(t, c, "get_flow_run_step", map[string]any{"flowRunId": "frn-a", "seq": float64(2)}))
			require.NotContains(t, step, "input")
			if tc.result == "" {
				require.NotContains(t, step, "result")
			} else {
				require.Contains(t, step, "result")
				require.Equal(t, tc.result, string(step["result"]))
			}
		})
	}
}

func TestFlowRunStepsPagination(t *testing.T) {
	f := newFlowFixture()
	base := f.handler(t)
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/flow-runs/frn-a/steps" {
			base(w, r)
			return
		}
		require.Equal(t, "1", r.URL.Query().Get("limit"))
		switch r.URL.Query().Get("cursor") {
		case "":
			fmt.Fprint(w, f.steps)
		case "next":
			fmt.Fprint(w, `{"steps":[],"hasMore":false}`)
		default:
			t.Errorf("unexpected cursor: %s", r.URL.Query().Get("cursor"))
		}
	})
	args := map[string]any{"flowRunId": "frn-a", "limit": float64(1)}
	first := decodeResult[struct {
		Items      []map[string]any
		NextCursor *string
	}](t, invoke(t, c, "list_flow_run_steps", args))
	require.Len(t, first.Items, 1)
	require.NotNil(t, first.NextCursor)
	args["cursor"] = *first.NextCursor
	last := decodeResult[struct {
		Items      []map[string]any
		NextCursor *string
	}](t, invoke(t, c, "list_flow_run_steps", args))
	require.NotNil(t, last.Items)
	require.Empty(t, last.Items)
	require.Nil(t, last.NextCursor)
}

func TestGetFlowRunStepIdentityAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name        string
		key         *string
		responseKey string
		responseSeq int
		wantError   bool
	}{
		{"keyed", new("key /:é"), "key /:é", 2, false},
		{"unkeyed", nil, "", 2, false},
		{"wrong key", new("wanted"), "other", 2, true},
		{"wrong sequence", nil, "", 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFlowFixture()
			base := f.handler(t)
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path != "/flow-runs/frn-a/steps/2" {
					base(w, r)
					return
				}
				if tc.key == nil {
					require.NotContains(t, r.URL.Query(), "stepKey")
				} else {
					require.Equal(t, *tc.key, r.URL.Query().Get("stepKey"))
				}
				fmt.Fprintf(w, `{"seq":%d,"stepKey":%q,"type":"inline","status":"failed","input":{"password":"secret"},"error":%q}`, tc.responseSeq, tc.responseKey, strings.Repeat("x", maxErrorBytes+10))
			})
			args := map[string]any{"flowRunId": "frn-a", "seq": float64(2)}
			if tc.key != nil {
				args["stepKey"] = *tc.key
			}
			result := invoke(t, c, "get_flow_run_step", args)
			require.Equal(t, tc.wantError, result.IsError, resultText(t, result))
			if tc.wantError {
				require.Contains(t, resultText(t, result), "invalid step identity")
				return
			}
			require.NotContains(t, resultText(t, result), "secret")
			step := decodeResult[Step](t, result)
			require.True(t, step.ErrorTruncated)
			require.Len(t, *step.Error, maxErrorBytes)
		})
	}
}

func TestStartFlowRunRequestEncoding(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"no input", map[string]any{}, `{"flow":"flw-a","input":[]}`},
		{"positional", map[string]any{"input": []any{1.0, "a"}}, `{"flow":"flw-a","input":[1,"a"]}`},
		{"named", map[string]any{"input": map[string]any{"n": 1.0}, "idempotencyKey": "key-1"}, `{"flow":"flw-a","input":{"n":1},"idempotencyKey":"key-1"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFlowFixture()
			tc.args["flow"] = "flw-a"
			run := decodeResult[map[string]any](t, invoke(t, fixtureClient(t, f.handler(t)), "start_flow_run", tc.args))
			require.Equal(t, "frn-new", run["id"])
			require.EqualValues(t, 1, f.writes.Load())
			require.JSONEq(t, tc.want, f.lastStart.Load().(string))
		})
	}
}

func TestStartFlowRunRejectsBeforeWriting(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"missing flow", map[string]any{}, "flow"},
		{"string input", map[string]any{"flow": "flw-a", "input": "x"}, "input must be"},
		{"blank key", map[string]any{"flow": "flw-a", "idempotencyKey": " "}, "idempotencyKey"},
		{"slug", map[string]any{"flow": "my-wf/hello"}, "flow ID"},
		{"versioned slug", map[string]any{"flow": "my-wf/hello:v1"}, "flow ID"},
		{"wrong resource", map[string]any{"flow": "tsk-a"}, "flow ID"},
		{"empty ID", map[string]any{"flow": "flw-"}, "flow ID"},
		{"ID with version", map[string]any{"flow": "flw-a:v1"}, "flow ID"},
		{"oversized input", map[string]any{"flow": "flw-a", "input": []any{strings.Repeat("x", maxWriteRequestBytes)}}, "256 KiB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFlowFixture()
			result := invoke(t, fixtureClient(t, f.handler(t)), "start_flow_run", tc.args)
			require.True(t, result.IsError)
			require.Contains(t, resultText(t, result), tc.want)
			require.Zero(t, f.writes.Load())
		})
	}
}

func TestStartFlowRunFailureNamesIdempotencyKey(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{http.StatusInternalServerError, `{"message":"secret input"}`}, {http.StatusAccepted, ``}} {
		f := newFlowFixture()
		f.startStatus, f.startBody = tc.status, tc.body
		result := invoke(t, fixtureClient(t, f.handler(t)), "start_flow_run", map[string]any{"flow": "flw-a", "idempotencyKey": "key-1"})
		require.True(t, result.IsError)
		require.Contains(t, resultText(t, result), "same idempotencyKey")
		require.NotContains(t, resultText(t, result), "secret")
		require.EqualValues(t, 1, f.writes.Load(), "writes are never retried")
	}
}

func TestCancelFlowRun(t *testing.T) {
	f := newFlowFixture()
	f.runStatus = "running"
	got := decodeResult[FlowRunCancellation](t, invoke(t, fixtureClient(t, f.handler(t)), "cancel_flow_run", map[string]any{"flowRunId": "frn-a"}))
	require.True(t, got.Requested)
	require.Contains(t, got.NextStep, "get_flow_run")
	require.EqualValues(t, 1, f.writes.Load())

	for _, status := range []string{"succeeded", "failed", "canceled"} {
		f := newFlowFixture()
		f.runStatus = status
		result := invoke(t, fixtureClient(t, f.handler(t)), "cancel_flow_run", map[string]any{"flowRunId": "frn-a"})
		require.True(t, result.IsError, status)
		require.Contains(t, resultText(t, result), "already "+status)
		require.Zero(t, f.writes.Load())
	}
}

func TestStartFlowRunChecksWorkspaceBeforeMutation(t *testing.T) {
	ctx := writeContext(t)
	fake := &fakes.FakeWorkflowRepoClient{}
	fake.GetFlowWithResponseReturns(parse(client.ParseGetFlowResponse, writeResponse(200, `{"id":"flw-a","name":"hello","workflowId":"wfl-a"}`), nil))
	fake.GetWorkflowWithResponseReturns(parse(client.ParseGetWorkflowResponse, writeResponse(200, `{"id":"wfl-a","ownerId":"tea-other"}`), nil))
	_, err := NewRepo(fake).StartFlowRun(ctx, client.StartFlowRunJSONRequestBody{Flow: "flw-a"})
	require.Error(t, err)
	require.Zero(t, fake.StartFlowRunWithResponseCallCount())
	require.Zero(t, fake.ListFlowsWithResponseCallCount())
	_, id, _ := fake.GetFlowWithResponseArgsForCall(0)
	require.Equal(t, "flw-a", id)
}

func TestDurability21SurfaceIsGated(t *testing.T) {
	tools := func(opts Options) map[string]mcp.Tool {
		byName := map[string]mcp.Tool{}
		for _, tool := range Tools(nil, opts) {
			byName[tool.Tool.Name] = tool.Tool
		}
		return byName
	}
	statusEnum := func(tool mcp.Tool) []string {
		data, err := json.Marshal(tool.InputSchema.Properties["status"].(map[string]any)["enum"])
		require.NoError(t, err)
		var values []string
		require.NoError(t, json.Unmarshal(data, &values))
		return values
	}
	for _, enabled := range []bool{false, true} {
		got := tools(Options{Durability21: enabled})
		require.Equal(t, enabled, slices.Contains(statusEnum(got["list_flow_runs"]), "paused"))
		require.Equal(t, enabled, strings.Contains(got["list_flow_run_steps"].Description, "childTaskRunId"))
		require.Equal(t, enabled, strings.Contains(got["get_flow_run_step"].Description, "detached"))
		require.Equal(t, enabled, strings.Contains(got["cancel_flow_run"].Description, "paused"))
	}

	calls := 0
	c := fixtureClient(t, func(http.ResponseWriter, *http.Request) { calls++ })
	for _, tool := range WithLimits(Tools(c, Options{})) {
		if tool.Tool.Name == "list_flow_runs" {
			result, err := tool.Handler(session.ContextWithWorkspace(t.Context(), "tea-a"), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"status": "paused"}}})
			require.NoError(t, err)
			require.True(t, result.IsError)
		}
	}
	require.Zero(t, calls)
}
