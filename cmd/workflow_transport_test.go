package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/stretchr/testify/require"
)

type workflowDoer func(*http.Request) (*http.Response, error)

func (f workflowDoer) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestWorkflowsDeadlineIncludesWorkspaceResolution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, e := client.NewClientWithResponses("https://api.example.test", client.WithHTTPClient(workflowDoer(func(r *http.Request) (*http.Response, error) {
			require.Equal(t, "/owners/tea-a", r.URL.Path)
			<-r.Context().Done()
			return nil, r.Context().Err()
		})))
		require.NoError(t, e)
		for _, tool := range buildWorkspaceScopedTools(c) {
			if tool.Tool.Name == "list_workflows" {
				start := time.Now()
				result, e := tool.Handler(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"workspaceId": "tea-a"}}})
				require.NoError(t, e)
				require.True(t, result.IsError)
				require.Equal(t, 30*time.Second, time.Since(start))
				return
			}
		}
		t.Fatal("Workflows tool was not registered")
	})
}

func TestWorkflowsTransportCancellation(t *testing.T) {
	for _, kind := range []string{"stdio", "http"} {
		t.Run(kind, func(t *testing.T) {
			started := make(chan struct{})
			canceled := make(chan struct{})
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/owners/tea-a":
					io.WriteString(w, `{"id":"tea-a"}`)
				case "/workflows":
					close(started)
					<-r.Context().Done()
					close(canceled)
				default:
					t.Errorf("unexpected API path %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer api.Close()
			c, e := client.NewClientWithResponses(api.URL, client.WithHTTPClient(api.Client()))
			require.NoError(t, e)
			call := mcp.JSONRPCRequest{JSONRPC: "2.0", ID: mcp.NewRequestId(2), Method: "tools/call", Params: mcp.CallToolParams{Name: "list_workflows", Arguments: map[string]any{"workspaceId": "tea-a"}}}
			if kind == "http" {
				_, transport := newStreamableHTTPServer(c, session.NewInMemoryStore())
				defer transport.Shutdown(t.Context())
				sid := initializeHTTPSession(t, transport)
				srv := httptest.NewServer(transport)
				defer srv.Close()
				b, e := json.Marshal(call)
				require.NoError(t, e)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				req, e := http.NewRequestWithContext(ctx, "POST", srv.URL, bytes.NewReader(b))
				require.NoError(t, e)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json, text/event-stream")
				req.Header.Set("Mcp-Session-Id", sid)
				req.Header.Set("MCP-Protocol-Version", "2025-03-26")
				done := make(chan struct{})
				go func() {
					defer close(done)
					r, _ := srv.Client().Do(req)
					if r != nil {
						io.Copy(io.Discard, r.Body)
						r.Body.Close()
					}
				}()
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Fatal("API not reached")
				}
				cancel()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("HTTP client did not cancel")
				}
			} else {
				t.Setenv("RENDER_API_KEY", "test-token")
				_, transport := newStdioServer(c)
				inR, inW := io.Pipe()
				outR, outW := io.Pipe()
				done := make(chan struct{})
				go func() { defer close(done); transport.Listen(t.Context(), inR, outW); outW.Close() }()
				defer func() { inW.Close(); outR.Close(); <-done }()
				enc := json.NewEncoder(inW)
				dec := json.NewDecoder(outR)
				require.NoError(t, enc.Encode(mcp.JSONRPCRequest{JSONRPC: "2.0", ID: mcp.NewRequestId(1), Method: "initialize", Params: mcp.InitializeParams{ProtocolVersion: "2025-03-26", Capabilities: mcp.ClientCapabilities{}, ClientInfo: mcp.Implementation{Name: "test", Version: "1"}}}))
				var initialized json.RawMessage
				require.NoError(t, dec.Decode(&initialized))
				go io.Copy(io.Discard, outR)
				require.NoError(t, enc.Encode(call))
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Fatal("API not reached")
				}
				require.NoError(t, enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": 2, "reason": "test"}}))
			}
			select {
			case <-canceled:
			case <-time.After(3 * time.Second):
				t.Fatal("upstream API request was not canceled")
			}
		})
	}
}
