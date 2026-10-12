package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/stretchr/testify/require"
)

func TestWorkflowWritesThroughTransports(t *testing.T) {
	t.Setenv("WORKFLOW_WRITES_ENABLED", "true")
	testWorkflowWritesThroughTransports(t, true)
}

func testWorkflowWritesThroughTransports(t *testing.T, enabled bool) {
	t.Helper()
	for _, kind := range []string{"http", "stdio"} {
		t.Run(fmt.Sprintf("%s/enabled=%t", kind, enabled), func(t *testing.T) {
			t.Setenv("RENDER_CONFIG_PATH", filepath.Join(t.TempDir(), "mcp-server.yaml"))
			t.Setenv("RENDER_API_KEY", "test-token")
			var mu sync.Mutex
			var writes []string
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.Method + " " + r.URL.Path {
				case "GET /owners/tea-a":
					io.WriteString(w, `{"id":"tea-a"}`)
				case "GET /workflows/wfl-new":
					io.WriteString(w, `{"id":"wfl-new","ownerId":"tea-a"}`)
				case "GET /workflows/wfl-other":
					io.WriteString(w, `{"id":"wfl-other","ownerId":"tea-other"}`)
				case "POST /workflows":
					mu.Lock()
					writes = append(writes, "create")
					mu.Unlock()
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["ownerId"] != "tea-a" {
						t.Error("invalid create body")
						w.WriteHeader(400)
						return
					}
					w.WriteHeader(201)
					io.WriteString(w, `{"id":"wfl-new","ownerId":"tea-a"}`)
				case "POST /workflowversions":
					mu.Lock()
					writes = append(writes, "deploy")
					mu.Unlock()
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["workflowId"] != "wfl-new" {
						t.Error("invalid version body")
						w.WriteHeader(400)
						return
					}
					w.WriteHeader(202)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer api.Close()
			c, err := client.NewClientWithResponses(api.URL, client.WithHTTPClient(api.Client()))
			require.NoError(t, err)
			var send func(string, any) json.RawMessage
			if kind == "http" {
				_, transport := newStreamableHTTPServer(c, session.NewInMemoryStore())
				defer transport.Shutdown(t.Context())
				sid := initializeHTTPSession(t, transport)
				send = func(method string, params any) json.RawMessage {
					rec := postMCP(t, transport, sid, method, params)
					require.Equal(t, 200, rec.Code)
					return append(json.RawMessage(nil), rec.Body.Bytes()...)
				}
			} else {
				_, transport := newStdioServer(c)
				inR, inW := io.Pipe()
				outR, outW := io.Pipe()
				done := make(chan error, 1)
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				go func() { done <- transport.Listen(ctx, inR, outW); outW.Close() }()
				defer func() { cancel(); inW.Close(); outR.Close(); <-done }()
				enc := json.NewEncoder(inW)
				dec := json.NewDecoder(outR)
				id := 0
				send = func(method string, params any) json.RawMessage {
					id++
					require.NoError(t, enc.Encode(mcp.JSONRPCRequest{JSONRPC: "2.0", ID: mcp.NewRequestId(id), Method: method, Params: params}))
					var result json.RawMessage
					require.NoError(t, dec.Decode(&result))
					return result
				}
				send("initialize", mcp.InitializeParams{ProtocolVersion: "2025-03-26", Capabilities: mcp.ClientCapabilities{}, ClientInfo: mcp.Implementation{Name: "test", Version: "1"}})
			}
			var catalog struct{ Result struct{ Tools []mcp.Tool } }
			require.NoError(t, json.Unmarshal(send("tools/list", map[string]any{}), &catalog))
			for _, name := range []string{"create_workflow", "create_workflow_version"} {
				present := slices.ContainsFunc(catalog.Result.Tools, func(tool mcp.Tool) bool { return tool.Name == name })
				require.Equal(t, enabled, present, name)
			}
			create := mcp.CallToolParams{Name: "create_workflow", Arguments: map[string]any{"workspaceId": "tea-a", "name": "test", "repo": "https://github.com/example/repo", "runtime": "python", "buildCommand": "pip install render-sdk", "runCommand": "python main.py"}}
			raw := send("tools/call", create)
			var result struct {
				Result mcp.CallToolResult
				Error  *mcp.JSONRPCErrorDetails
			}
			require.NoError(t, json.Unmarshal(raw, &result))
			if !enabled {
				require.True(t, result.Error != nil || result.Result.IsError, "disabled create unexpectedly callable")
				raw = send("tools/call", mcp.CallToolParams{Name: "create_workflow_version", Arguments: map[string]any{"workspaceId": "tea-a", "workflowId": "wfl-new"}})
				result = struct {
					Result mcp.CallToolResult
					Error  *mcp.JSONRPCErrorDetails
				}{}
				require.NoError(t, json.Unmarshal(raw, &result))
				require.True(t, result.Error != nil || result.Result.IsError, "disabled deploy unexpectedly callable")
				mu.Lock()
				require.Empty(t, writes)
				mu.Unlock()
				return
			}
			require.Nil(t, result.Error)
			require.False(t, result.Result.IsError)
			require.Contains(t, string(raw), "wfl-new")
			for _, id := range []string{"wfl-other", "wfl-new"} {
				raw = send("tools/call", mcp.CallToolParams{Name: "create_workflow_version", Arguments: map[string]any{"workspaceId": "tea-a", "workflowId": id}})
				require.NoError(t, json.Unmarshal(raw, &result))
				require.Nil(t, result.Error)
				require.Equal(t, id == "wfl-other", result.Result.IsError)
				if id == "wfl-new" {
					require.Contains(t, string(raw), "accepted")
					require.Contains(t, string(raw), "list_workflow_versions")
					require.False(t, strings.Contains(string(raw), "wfv-"))
				}
			}
			mu.Lock()
			require.Equal(t, []string{"create", "deploy"}, writes)
			mu.Unlock()
		})
	}
}
