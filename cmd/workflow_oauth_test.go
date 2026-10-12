package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/authn"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/oauth"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/stretchr/testify/require"
	"path/filepath"
)

// Exercise a Workflows handler behind the same OAuth mux and request-header
// helpers used by the HTTP server, rather than testing an auth stand-in.
func TestWorkflowsThroughSharedOAuth(t *testing.T) {
	t.Setenv("WORKFLOW_WRITES_ENABLED", "true")
	t.Setenv("RENDER_CONFIG_PATH", filepath.Join(t.TempDir(), "mcp-server.yaml"))
	const resource = "https://mcp.example.test/mcp"
	const serviceToken = "test-introspection-service-token"
	for _, toolName := range []string{"list_workflows", "create_workflow", "create_workflow_version"} {
		t.Run(toolName, func(t *testing.T) {
			for _, tc := range []struct {
				name, token, introspection                               string
				passthrough                                              bool
				introspectionStatus, apiStatus, wantStatus, wantAPICalls int
				wantToolError                                            bool
			}{
				{"active matching audience", "active", `{"active":true,"aud":["` + resource + `"],"exp":4102444800}`, true, 200, 200, 200, 2, false},
				{"wrong audience", "wrong-audience", `{"active":true,"aud":["https://other.example/mcp"],"exp":4102444800}`, true, 200, 200, 401, 0, false},
				{"expired active token", "expired", `{"active":true,"aud":["` + resource + `"],"exp":1}`, true, 200, 200, 401, 0, false},
				{"revoked OAuth never falls through", "revoked", `{"active":false,"render_token_kind":"oauth_access"}`, true, 200, 200, 401, 0, false},
				{"API key passes through", "api-key", `{"active":false}`, true, 200, 200, 200, 2, false},
				{"unknown token rejected by API", "unknown", `{"active":false}`, true, 200, 401, 200, 1, true},
				{"strict mode rejects inactive", "inactive", `{"active":false}`, false, 200, 200, 401, 0, false},
				{"missing token", "", `{"active":true,"aud":["` + resource + `"]}`, true, 200, 200, 401, 0, false},
				{"introspection outage fails closed", "outage", "", true, 503, 200, 503, 0, false},
				{"workspace permission denied", "denied", `{"active":true,"aud":["` + resource + `"],"exp":4102444800}`, true, 200, 403, 200, 1, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					var introspections, apiCalls, lists atomic.Int32
					as := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						introspections.Add(1)
						if r.Method != http.MethodPost || r.URL.Path != "/v1/oauth/introspect" ||
							r.Header.Get("Authorization") != "Bearer "+serviceToken {
							t.Errorf("unexpected introspection request method, path, or service credential")
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						if err := r.ParseForm(); err != nil {
							t.Error(err)
							w.WriteHeader(400)
							return
						}
						token := r.Form.Get("token")
						w.Header().Set("Content-Type", "application/json")
						if token == "initialize-token" {
							_, _ = w.Write([]byte(`{"active":true,"aud":["` + resource + `"],"exp":4102444800}`))
							return
						}
						if token != tc.token {
							t.Errorf("wrong user token sent for introspection")
						}
						w.WriteHeader(tc.introspectionStatus)
						_, _ = w.Write([]byte(tc.introspection))
					}))
					defer as.Close()

					t.Setenv("OAUTH_ENABLED", "true")
					t.Setenv("OAUTH_AUTHORIZATION_SERVER_URL", as.URL)
					t.Setenv("OAUTH_CANONICAL_RESOURCE_URI", resource)
					t.Setenv("OAUTH_API_KEY_PASSTHROUGH", strconv.FormatBool(tc.passthrough))
					t.Setenv(oauth.AuthTokenEnv, serviceToken)

					api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						apiCalls.Add(1)
						if r.Header.Get("Authorization") != "Bearer "+tc.token ||
							r.Header.Get(client.APIAuthHeader) != serviceToken {
							t.Errorf("shared API client did not forward the expected user and server credentials")
							w.WriteHeader(401)
							return
						}
						w.Header().Set("Content-Type", "application/json")
						if r.Method == http.MethodGet && r.URL.Path == "/v1/owners/tea-a" {
							w.WriteHeader(tc.apiStatus)
							if tc.apiStatus == 200 {
								_, _ = w.Write([]byte(`{"id":"tea-a"}`))
							} else {
								_, _ = w.Write([]byte(`{"message":"rejected"}`))
							}
							return
						}
						if r.Method == http.MethodGet && r.URL.Path == "/v1/workflows" {
							lists.Add(1)
							if r.URL.Query().Get("ownerId") != "tea-a" {
								t.Errorf("wrong workspace filter")
							}
							_, _ = w.Write([]byte(`[{"workflow":{"id":"wfl-oauth-test","ownerId":"tea-a"},"cursor":"next"}]`))
							return
						}
						if r.Method == http.MethodGet && r.URL.Path == "/v1/workflows/wfl-oauth-test" {
							_, _ = w.Write([]byte(`{"id":"wfl-oauth-test","ownerId":"tea-a"}`))
							return
						}
						if r.Method == http.MethodPost && r.URL.Path == "/v1/workflows" {
							lists.Add(1)
							var body map[string]any
							if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["ownerId"] != "tea-a" {
								t.Error("creation did not use the authorized workspace")
								w.WriteHeader(400)
								return
							}
							w.WriteHeader(201)
							_, _ = w.Write([]byte(`{"id":"wfl-oauth-test","ownerId":"tea-a"}`))
							return
						}
						if r.Method == http.MethodPost && r.URL.Path == "/v1/workflowversions" {
							lists.Add(1)
							w.WriteHeader(201)
							_, _ = w.Write([]byte(`{"id":"wfv-oauth-test","workflowId":"wfl-oauth-test","status":"building"}`))
							return
						}
						t.Errorf("unexpected API request %s %s", r.Method, r.URL.Path)
						w.WriteHeader(500)
					}))
					defer api.Close()
					c, err := client.NewClientWithResponses(api.URL+"/v1", client.WithHTTPClient(api.Client()),
						client.WithRequestEditorFn(func(ctx context.Context, r *http.Request) error {
							r.Header = client.AddHeaders(ctx, r.Header, authn.APITokenFromContext(ctx))
							return nil
						}))
					require.NoError(t, err)
					_, transport := newStreamableHTTPServer(c, session.NewInMemoryStore())
					t.Cleanup(func() {
						ctx, cancel := context.WithTimeout(context.Background(), time.Second)
						defer cancel()
						require.NoError(t, transport.Shutdown(ctx))
					})
					cfg, err := oauth.FromEnv()
					require.NoError(t, err)
					mux := newHTTPMux(transport, cfg, "")
					withToken := func(token string) http.Handler {
						return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							if token != "" {
								r.Header.Set("Authorization", "Bearer "+token)
							}
							mux.ServeHTTP(w, r)
						})
					}
					sessionID := initializeHTTPSession(t, withToken("initialize-token"))
					before := introspections.Load()
					args := map[string]any{"workspaceId": "tea-a"}
					if toolName == "create_workflow" {
						args["name"], args["repo"], args["runtime"] = "sample", "https://github.com/example/workflow", "python"
						args["buildCommand"], args["runCommand"] = "pip install render-sdk", "python main.py"
					} else if toolName == "create_workflow_version" {
						args["workflowId"] = "wfl-oauth-test"
					}
					rec := postMCP(t, withToken(tc.token), sessionID, string(mcp.MethodToolsCall),
						mcp.CallToolParams{Name: toolName, Arguments: args})
					require.Equal(t, tc.wantStatus, rec.Code)
					wantCalls := tc.wantAPICalls
					if toolName == "create_workflow_version" && wantCalls == 2 {
						wantCalls++
					}
					require.EqualValues(t, wantCalls, apiCalls.Load())
					require.NotContains(t, rec.Body.String(), serviceToken)
					if tc.wantStatus == 200 {
						var response struct{ Result mcp.CallToolResult }
						require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
						require.Equal(t, tc.wantToolError, response.Result.IsError)
						require.Equal(t, !tc.wantToolError, lists.Load() == 1)
						if !tc.wantToolError {
							require.Contains(t, rec.Body.String(), "wfl-oauth-test")
						}
					} else {
						require.Zero(t, lists.Load(), "rejected authorization must not query Workflows")
						if tc.wantStatus == 401 {
							require.Contains(t, rec.Header().Get("WWW-Authenticate"), "resource_metadata=")
						}
						if tc.wantStatus == 503 {
							require.Equal(t, "5", rec.Header().Get("Retry-After"))
							require.NotContains(t, rec.Header().Get("WWW-Authenticate"), "invalid_token")
						}
					}
					if tc.token == "" {
						require.Equal(t, before, introspections.Load())
					} else {
						require.Equal(t, before+1, introspections.Load())
					}
					require.False(t, strings.Contains(rec.Body.String(), "Bearer "+tc.token) && tc.token != "")
				})
			}
		})
	}
}
