package cmd

import (
	"context"
	"encoding/json"
	"maps"
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

const (
	oauthTestResource     = "https://mcp.example.test/mcp"
	oauthTestServiceToken = "test-introspection-service-token"
)

type oauthCase struct {
	name, token, introspection                               string
	passthrough                                              bool
	introspectionStatus, apiStatus, wantStatus, wantAPICalls int
	wantToolError                                            bool
}

// Exercise a Workflows handler behind the same OAuth mux and request-header
// helpers used by the HTTP server, rather than testing an auth stand-in.
func TestWorkflowsThroughSharedOAuth(t *testing.T) {
	t.Setenv("RENDER_CONFIG_PATH", filepath.Join(t.TempDir(), "mcp-server.yaml"))
	active := `{"active":true,"aud":["` + oauthTestResource + `"],"exp":4102444800}`
	tools := []struct {
		name string
		args map[string]any
		// lookups counts API reads a successful call makes beyond the
		// workspace check and the Workflows request itself.
		lookups int
	}{
		{"list_workflows", map[string]any{}, 0},
		{"create_workflow", map[string]any{"name": "sample", "repo": "https://github.com/example/workflow", "runtime": "python", "buildCommand": "pip install render-sdk", "runCommand": "python main.py"}, 0},
		{"create_workflow_version", map[string]any{"workflowId": "wfl-oauth-test"}, 1},
	}
	cases := []oauthCase{
		{"active matching audience", "active", active, true, 200, 200, 200, 2, false},
		{"wrong audience", "wrong-audience", `{"active":true,"aud":["https://other.example/mcp"],"exp":4102444800}`, true, 200, 200, 401, 0, false},
		{"expired active token", "expired", `{"active":true,"aud":["` + oauthTestResource + `"],"exp":1}`, true, 200, 200, 401, 0, false},
		{"revoked OAuth never falls through", "revoked", `{"active":false,"render_token_kind":"oauth_access"}`, true, 200, 200, 401, 0, false},
		{"API key passes through", "api-key", `{"active":false}`, true, 200, 200, 200, 2, false},
		{"unknown token rejected by API", "unknown", `{"active":false}`, true, 200, 401, 200, 1, true},
		{"strict mode rejects inactive", "inactive", `{"active":false}`, false, 200, 200, 401, 0, false},
		{"missing token", "", `{"active":true,"aud":["` + oauthTestResource + `"]}`, true, 200, 200, 401, 0, false},
		{"introspection outage fails closed", "outage", "", true, 503, 200, 503, 0, false},
		{"workspace permission denied", "denied", active, true, 200, 403, 200, 1, true},
	}
	for _, tool := range tools {
		t.Run(tool.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					var introspections, apiCalls, workflowCalls atomic.Int32
					as := newIntrospectionServer(t, tc, &introspections)
					api := newOAuthWorkflowsAPI(t, tc, &apiCalls, &workflowCalls)
					mux := newOAuthMux(t, as.URL, api, tc.passthrough)

					sessionID := initializeHTTPSession(t, withBearer(mux, "initialize-token"))
					before := introspections.Load()
					args := map[string]any{"workspaceId": "tea-a"}
					maps.Copy(args, tool.args)
					rec := postMCP(t, withBearer(mux, tc.token), sessionID, string(mcp.MethodToolsCall),
						mcp.CallToolParams{Name: tool.name, Arguments: args})

					require.Equal(t, tc.wantStatus, rec.Code)
					wantCalls := tc.wantAPICalls
					if wantCalls == 2 {
						wantCalls += tool.lookups
					}
					require.EqualValues(t, wantCalls, apiCalls.Load())
					require.NotContains(t, rec.Body.String(), oauthTestServiceToken)
					if tc.wantStatus == 200 {
						var response struct{ Result mcp.CallToolResult }
						require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
						require.Equal(t, tc.wantToolError, response.Result.IsError)
						require.Equal(t, !tc.wantToolError, workflowCalls.Load() == 1)
						if !tc.wantToolError {
							require.Contains(t, rec.Body.String(), "wfl-oauth-test")
						}
					} else {
						require.Zero(t, workflowCalls.Load(), "rejected authorization must not query Workflows")
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

func newIntrospectionServer(t *testing.T, tc oauthCase, introspections *atomic.Int32) *httptest.Server {
	t.Helper()
	as := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		introspections.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/oauth/introspect" ||
			r.Header.Get("Authorization") != "Bearer "+oauthTestServiceToken {
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
			_, _ = w.Write([]byte(`{"active":true,"aud":["` + oauthTestResource + `"],"exp":4102444800}`))
			return
		}
		if token != tc.token {
			t.Errorf("wrong user token sent for introspection")
		}
		w.WriteHeader(tc.introspectionStatus)
		_, _ = w.Write([]byte(tc.introspection))
	}))
	t.Cleanup(as.Close)
	return as
}

// newOAuthWorkflowsAPI counts every API call, and separately the Workflows
// list or write a tool call makes once authorization succeeds.
func newOAuthWorkflowsAPI(t *testing.T, tc oauthCase, apiCalls, workflowCalls *atomic.Int32) *httptest.Server {
	t.Helper()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+tc.token ||
			r.Header.Get(client.APIAuthHeader) != oauthTestServiceToken {
			t.Errorf("shared API client did not forward the expected user and server credentials")
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/owners/tea-a":
			w.WriteHeader(tc.apiStatus)
			if tc.apiStatus == 200 {
				_, _ = w.Write([]byte(`{"id":"tea-a"}`))
			} else {
				_, _ = w.Write([]byte(`{"message":"rejected"}`))
			}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/workflows":
			workflowCalls.Add(1)
			if r.URL.Query().Get("ownerId") != "tea-a" {
				t.Errorf("wrong workspace filter")
			}
			_, _ = w.Write([]byte(`[{"workflow":{"id":"wfl-oauth-test","ownerId":"tea-a"},"cursor":"next"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/workflows/wfl-oauth-test":
			_, _ = w.Write([]byte(`{"id":"wfl-oauth-test","ownerId":"tea-a"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/workflows":
			workflowCalls.Add(1)
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["ownerId"] != "tea-a" {
				t.Error("creation did not use the authorized workspace")
				w.WriteHeader(400)
				return
			}
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"id":"wfl-oauth-test","ownerId":"tea-a"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/workflowversions":
			workflowCalls.Add(1)
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"id":"wfv-oauth-test","workflowId":"wfl-oauth-test","status":"building"}`))
		default:
			t.Errorf("unexpected API request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(api.Close)
	return api
}

// newOAuthMux builds the HTTP server's mux with OAuth pointed at the test
// authorization server and a client that forwards headers like the real one.
func newOAuthMux(t *testing.T, authServerURL string, api *httptest.Server, passthrough bool) http.Handler {
	t.Helper()
	t.Setenv("OAUTH_ENABLED", "true")
	t.Setenv("OAUTH_AUTHORIZATION_SERVER_URL", authServerURL)
	t.Setenv("OAUTH_CANONICAL_RESOURCE_URI", oauthTestResource)
	t.Setenv("OAUTH_API_KEY_PASSTHROUGH", strconv.FormatBool(passthrough))
	t.Setenv(oauth.AuthTokenEnv, oauthTestServiceToken)
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
	return newHTTPMux(transport, cfg, "")
}

func withBearer(h http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		h.ServeHTTP(w, r)
	})
}
