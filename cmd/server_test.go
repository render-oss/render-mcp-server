package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	blueprintclient "github.com/render-oss/render-mcp-server/pkg/client/blueprints"
	"github.com/render-oss/render-mcp-server/pkg/config"
	"github.com/render-oss/render-mcp-server/pkg/logging"
	"github.com/render-oss/render-mcp-server/pkg/oauth"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingHandler stands in for the MCP server and reports whether it was reached.
func recordingHandler() (http.Handler, *bool) {
	called := false
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	return h, &called
}

func TestHTTPWorkspaceSelection(t *testing.T) {
	t.Run("selections are isolated by session ID", func(t *testing.T) {
		httpTransport, renderAPI := newWorkspaceHTTPTestServer(t)

		firstSessionID := initializeHTTPSession(t, httpTransport)
		secondSessionID := initializeHTTPSession(t, httpTransport)
		require.NotEqual(t, firstSessionID, secondSessionID)
		firstSelection := callHTTPTool(t, httpTransport, firstSessionID, "select_workspace", map[string]any{"ownerID": "tea-first"})
		require.False(t, firstSelection.IsError)
		secondSelection := callHTTPTool(t, httpTransport, secondSessionID, "select_workspace", map[string]any{"ownerID": "tea-second"})
		require.False(t, secondSelection.IsError)

		firstResult := callHTTPTool(t, httpTransport, firstSessionID, "list_services", nil)
		require.False(t, firstResult.IsError)
		require.Equal(t, "tea-first", renderAPI.lastWorkspaceID())
		require.Equal(t, 1, renderAPI.servicesCallCount)

		secondResult := callHTTPTool(t, httpTransport, secondSessionID, "list_services", nil)
		require.False(t, secondResult.IsError)
		require.Equal(t, "tea-second", renderAPI.lastWorkspaceID())
		require.Equal(t, 2, renderAPI.servicesCallCount)
	})

	t.Run("explicit workspace overrides apply only to the current request", func(t *testing.T) {
		httpTransport, renderAPI := newWorkspaceHTTPTestServer(t)

		sessionID := initializeHTTPSession(t, httpTransport)
		selection := callHTTPTool(t, httpTransport, sessionID, "select_workspace", map[string]any{"ownerID": "tea-selected"})
		require.False(t, selection.IsError)

		result := callHTTPTool(t, httpTransport, sessionID, "list_services", nil)
		require.False(t, result.IsError)
		require.Equal(t, "tea-selected", renderAPI.lastWorkspaceID())
		require.Equal(t, 1, renderAPI.servicesCallCount)

		override := callHTTPTool(t, httpTransport, sessionID, "list_services", map[string]any{"workspaceId": "tea-explicit"})
		require.False(t, override.IsError)
		require.Equal(t, "tea-explicit", renderAPI.lastWorkspaceID())
		require.Equal(t, 2, renderAPI.servicesCallCount)

		result = callHTTPTool(t, httpTransport, sessionID, "list_services", nil)
		require.False(t, result.IsError)
		require.Equal(t, "tea-selected", renderAPI.lastWorkspaceID())
		require.Equal(t, 3, renderAPI.servicesCallCount)
	})

	t.Run("stored selection survives idle cleanup", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var expiredSessionIDs []string
			apiClient, _ := newTestRenderClient(t)
			mcpServer, httpTransport := newStreamableHTTPServer(apiClient, session.NewInMemoryStore())
			// add a new hook for testing to track the sdk unregistering idle sessions
			mcpServer.GetHooks().AddOnUnregisterSession(func(_ context.Context, clientSession server.ClientSession) {
				expiredSessionIDs = append(expiredSessionIDs, clientSession.SessionID())
			})
			t.Cleanup(func() { require.NoError(t, httpTransport.Shutdown(t.Context())) })

			sessionID := initializeHTTPSession(t, httpTransport)
			selection := callHTTPTool(t, httpTransport, sessionID, "select_workspace", map[string]any{"ownerID": "tea-selected"})
			require.False(t, selection.IsError)

			// Advance the fake clock past idle expiry and a subsequent cleanup sweep.
			synctest.Sleep(2 * httpSessionIdleTTL)
			require.Equal(t, []string{sessionID}, expiredSessionIDs)

			// SDK cleanup leaves the separate workspace store intact, and the ID
			// manager still accepts this ID. Revisit if workspace persistence or
			// session ID validation changes.
			result := callHTTPTool(t, httpTransport, sessionID, "get_selected_workspace", nil)

			require.False(t, result.IsError)
			require.NotEmpty(t, result.Content)
			text, ok := result.Content[0].(mcp.TextContent)
			require.True(t, ok)
			require.Contains(t, text.Text, "tea-selected")
		})
	})
}

// HTTP workspace selections are stored by MCP session ID, which sessionless requests lack.
func TestHTTPRejectsSessionlessToolCalls(t *testing.T) {
	apiClient, _ := newTestRenderClient(t)
	_, httpTransport := newStreamableHTTPServer(apiClient, session.NewInMemoryStore())
	t.Cleanup(func() { require.NoError(t, httpTransport.Shutdown(t.Context())) })

	meta := new(mcp.Meta)
	sessionlessProtocol := mcp.ProtocolVersion20260728
	meta.SetProtocolVersion(sessionlessProtocol)
	meta.SetClientCapabilities(mcp.ClientCapabilities{})
	request := mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(1),
		Method:  string(mcp.MethodToolsCall),
		Params: mcp.CallToolParams{
			Name: "get_selected_workspace",
			Meta: meta,
		},
	}
	body, err := json.Marshal(request)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req = req.WithContext(logging.ContextWithDefaultLogger(req.Context()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Protocol-Version", sessionlessProtocol)
	req.Header.Set("Mcp-Method", "tools/call")
	req.Header.Set("Mcp-Name", "get_selected_workspace")
	rec := httptest.NewRecorder()

	httpTransport.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var response transport.JSONRPCResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Empty(t, response.Result)
	require.NotNil(t, response.Error)

	var protocolErr mcp.UnsupportedProtocolVersionError
	require.ErrorAs(t, response.Error.AsError(), &protocolErr)
	require.Equal(t, mcp.LegacyProtocolVersions(), protocolErr.Supported)
}

func TestStdioProtocolCompatibility(t *testing.T) {
	t.Run("initialize negotiates a session-based version and tools read the configured workspace", func(t *testing.T) {
		t.Setenv("RENDER_API_KEY", "test-token")
		configPath := filepath.Join(t.TempDir(), "mcp-server.yaml")
		require.NoError(t, os.WriteFile(configPath, []byte("version: 1\nworkspace: tea-stdio\n"), 0600))
		t.Setenv("RENDER_CONFIG_PATH", configPath)
		apiClient, _ := newTestRenderClient(t)
		_, stdioTransport := newStdioServer(apiClient)

		// we want to confirm the protocol gets downgraded
		givenVersion := mcp.ProtocolVersion20260728
		wantVersion := mcp.ProtocolVersion20251125
		require.Greater(t, givenVersion, wantVersion)

		// The server handles requests concurrently, so wait for each response
		// before sending the next request, like a real client.
		stdinReader, stdinWriter := io.Pipe()
		stdoutReader, stdoutWriter := io.Pipe()
		listenErr := make(chan error, 1)
		go func() {
			listenErr <- stdioTransport.Listen(logging.ContextWithDefaultLogger(t.Context()), stdinReader, stdoutWriter)
			stdoutWriter.Close()
		}()
		encoder := json.NewEncoder(stdinWriter)
		decoder := json.NewDecoder(stdoutReader)

		require.NoError(t, encoder.Encode(mcp.JSONRPCRequest{
			JSONRPC: mcp.JSONRPC_VERSION,
			ID:      mcp.NewRequestId(1),
			Method:  string(mcp.MethodInitialize),
			Params: mcp.InitializeParams{
				ProtocolVersion: givenVersion,
				Capabilities:    mcp.ClientCapabilities{},
				ClientInfo:      mcp.Implementation{Name: "test", Version: "1"},
			},
		}))
		var initialized struct {
			ID     int
			Result mcp.InitializeResult
			Error  *mcp.JSONRPCErrorDetails
		}
		require.NoError(t, decoder.Decode(&initialized))
		require.Equal(t, 1, initialized.ID)
		require.Nil(t, initialized.Error)
		require.Equal(t, wantVersion, initialized.Result.ProtocolVersion)

		require.NoError(t, encoder.Encode(mcp.JSONRPCRequest{
			JSONRPC: mcp.JSONRPC_VERSION,
			ID:      mcp.NewRequestId(2),
			Method:  string(mcp.MethodToolsCall),
			Params:  mcp.CallToolParams{Name: "get_selected_workspace"},
		}))
		var response struct {
			ID     int
			Result mcp.CallToolResult
			Error  *mcp.JSONRPCErrorDetails
		}
		require.NoError(t, decoder.Decode(&response))
		require.Equal(t, 2, response.ID)
		require.Nil(t, response.Error)
		require.False(t, response.Result.IsError)
		require.NotEmpty(t, response.Result.Content)
		text, ok := response.Result.Content[0].(mcp.TextContent)
		require.True(t, ok)
		require.Contains(t, text.Text, "tea-stdio")

		// Drain any late writes so Listen can return.
		go io.Copy(io.Discard, stdoutReader)
		require.NoError(t, stdinWriter.Close())
		require.NoError(t, <-listenErr)
	})
}

func TestStdioSessionlessWorkspaceSelection(t *testing.T) {
	t.Run("subsequent calls use the selected workspace", func(t *testing.T) {
		t.Setenv("RENDER_API_KEY", "test-token")
		configPath := filepath.Join(t.TempDir(), "mcp-server.yaml")
		require.NoError(t, os.WriteFile(configPath, []byte("version: 1\nworkspace: tea-initial\n"), 0600))
		t.Setenv("RENDER_CONFIG_PATH", configPath)
		apiClient, renderAPI := newTestRenderClient(t)
		mcpClient := newSessionlessStdioTestClient(t, apiClient)

		selection, err := mcpClient.CallTool(t.Context(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{Name: "select_workspace", Arguments: map[string]any{"ownerID": "tea-selected"}},
		})
		require.NoError(t, err)
		require.False(t, selection.IsError)

		services, err := mcpClient.CallTool(t.Context(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{Name: "list_services"},
		})

		require.NoError(t, err)
		require.False(t, services.IsError)
		require.Equal(t, "tea-selected", renderAPI.lastWorkspaceID())
		require.Equal(t, 1, renderAPI.servicesCallCount)
	})

	t.Run("explicit workspace overrides apply only to the current request", func(t *testing.T) {
		t.Setenv("RENDER_API_KEY", "test-token")
		configPath := filepath.Join(t.TempDir(), "mcp-server.yaml")
		require.NoError(t, os.WriteFile(configPath, []byte("version: 1\nworkspace: tea-selected\n"), 0600))
		t.Setenv("RENDER_CONFIG_PATH", configPath)
		apiClient, renderAPI := newTestRenderClient(t)
		mcpClient := newSessionlessStdioTestClient(t, apiClient)

		override, err := mcpClient.CallTool(t.Context(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{Name: "list_services", Arguments: map[string]any{"workspaceId": "tea-explicit"}},
		})
		require.NoError(t, err)
		require.False(t, override.IsError)
		require.Equal(t, "tea-explicit", renderAPI.lastWorkspaceID())
		require.Equal(t, 1, renderAPI.servicesCallCount)

		services, err := mcpClient.CallTool(t.Context(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{Name: "list_services"},
		})

		require.NoError(t, err)
		require.False(t, services.IsError)
		require.Equal(t, "tea-selected", renderAPI.lastWorkspaceID())
		require.Equal(t, 2, renderAPI.servicesCallCount)
	})
}

func TestWithMiddleware_Order(t *testing.T) {
	var calls []string
	recordMiddleware := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, name+":before")
				next.ServeHTTP(w, r)
				calls = append(calls, name+":after")
			})
		}
	}
	handler := withMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls = append(calls, "handler")
	}), recordMiddleware("first"), recordMiddleware("second"))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, []string{"first:before", "second:before", "handler", "second:after", "first:after"}, calls)
}

func TestRequestBodyLimitMiddleware(t *testing.T) {
	const wantLimitBytes = 256
	readBody := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	t.Run("at limit", func(t *testing.T) {
		var output bytes.Buffer
		logger := logging.New(&output, slog.LevelInfo)
		handler := requestBodyLimitMiddleware(wantLimitBytes)(readBody)
		body := strings.Repeat("x", wantLimitBytes)
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
		req = req.WithContext(logging.ContextWithLogger(req.Context(), logger))
		// ensure max bytes is measured, not believed from content length
		req.ContentLength = -1
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Empty(t, output.String())
	})

	t.Run("over limit propagates read error", func(t *testing.T) {
		var output bytes.Buffer
		logger := logging.New(&output, slog.LevelInfo).WithFields(logging.OneOffField("request", "body-limit"))
		handler := requestBodyLimitMiddleware(wantLimitBytes)(readBody)
		body := strings.Repeat("x", wantLimitBytes+64)
		reader := strings.NewReader(body)
		req := httptest.NewRequest(http.MethodPost, "/mcp", reader)
		req = req.WithContext(logging.ContextWithLogger(req.Context(), logger))
		// ensure max bytes is measured, not believed from content length
		req.ContentLength = -1
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		require.Equal(t, http.StatusInternalServerError, rec.Code)
		require.Contains(t, rec.Body.String(), "http: request body too large")
		require.LessOrEqual(t, len(body)-reader.Len(), wantLimitBytes+1)
		var record map[string]any
		require.NoError(t, json.Unmarshal(output.Bytes(), &record))
		require.Equal(t, "HTTP request body limit exceeded", record["msg"])
		require.Equal(t, "WARN", record["level"])
		require.Equal(t, "body-limit", record["request"])
	})

	t.Run("over limit SDK returns bad request", func(t *testing.T) {
		apiClient, _ := newTestRenderClient(t)
		_, httpTransport := newStreamableHTTPServer(apiClient, session.NewInMemoryStore())
		t.Cleanup(func() { require.NoError(t, httpTransport.Shutdown(t.Context())) })
		handler := requestBodyLimitMiddleware(wantLimitBytes)(httpTransport)
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(strings.Repeat("x", wantLimitBytes+1)))
		req = req.WithContext(logging.ContextWithLogger(req.Context(), logging.New(io.Discard, slog.LevelInfo)))
		req.ContentLength = -1
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "read request body error: http: request body too large")
	})
}

func TestNewHTTPMux_OAuth(t *testing.T) {
	t.Run("disabled allows MCP requests without a challenge", func(t *testing.T) {
		mcp, called := recordingHandler()
		mux := newHTTPMux(mcp, oauth.Config{}, "")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))

		require.True(t, *called)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Empty(t, rec.Header().Get("WWW-Authenticate"))
	})

	t.Run("disabled does not advertise metadata", func(t *testing.T) {
		mcp, _ := recordingHandler()
		mux := newHTTPMux(mcp, oauth.Config{}, "")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))

		require.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("enabled advertises path-specific metadata", func(t *testing.T) {
		cfg := oauth.Config{
			Enabled:                true,
			AuthorizationServerURL: "https://as.example.com",
			CanonicalResourceURI:   "https://mcp.example.com/mcp",
			APIKeyPassthrough:      true,
		}
		mcp, _ := recordingHandler()
		mux := newHTTPMux(mcp, cfg, "")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp", nil))

		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), cfg.CanonicalResourceURI)
	})

	t.Run("enabled challenges requests without credentials before reaching MCP", func(t *testing.T) {
		cfg := oauth.Config{
			Enabled:                true,
			AuthorizationServerURL: "https://as.example.com",
			CanonicalResourceURI:   "https://mcp.example.com/mcp",
			APIKeyPassthrough:      true,
		}
		mcp, called := recordingHandler()
		mux := newHTTPMux(mcp, cfg, "")
		body := strings.NewReader(strings.Repeat("x", 1024))
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", body))

		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Contains(t, rec.Header().Get("WWW-Authenticate"), "resource_metadata=")
		require.False(t, *called)
		require.Equal(t, 1024, body.Len(), "authentication must reject the request without reading its body")
	})
}

func TestNewHTTPMux_OpenAIChallenge(t *testing.T) {
	t.Run("configured token is served", func(t *testing.T) {
		mcp, _ := recordingHandler()
		mux := newHTTPMux(mcp, oauth.Config{}, "verify-token")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/openai-apps-challenge", nil))

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "verify-token", rec.Body.String())
	})

	t.Run("route is absent without a token", func(t *testing.T) {
		mcp, _ := recordingHandler()
		mux := newHTTPMux(mcp, oauth.Config{}, "")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/openai-apps-challenge", nil))

		require.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestHTTPQueryPostgresExplainsIPAllowList(t *testing.T) {
	tests := []struct {
		name                    string
		ipAllowList             string
		wantMessage             string
		wantConnectionInfoCalls int
	}{
		{
			name:                    "empty allowlist skips the connection",
			ipAllowList:             `[]`,
			wantMessage:             "blocks all external connections because its IP allowlist is empty",
			wantConnectionInfoCalls: 0,
		},
		{
			name:                    "restricted allowlist explains a failed connection",
			ipAllowList:             `[{"cidrBlock":"203.0.113.0/24","description":"office"}]`,
			wantMessage:             "only accepts external connections from IP addresses on its allowlist",
			wantConnectionInfoCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A port nothing listens on, so the connection is refused.
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			require.NoError(t, listener.Close())

			httpTransport, renderAPI := newWorkspaceHTTPTestServer(t)
			renderAPI.postgres = `{"id":"dpg-test","ipAllowList":` + tt.ipAllowList + `}`
			renderAPI.connectionInfo = `{"externalConnectionString":"postgresql://user:pass@` + listener.Addr().String() + `/db?sslmode=disable&connect_timeout=5"}`

			sessionID := initializeHTTPSession(t, httpTransport)
			result := callHTTPTool(t, httpTransport, sessionID, "query_render_postgres", map[string]any{
				"postgresId": "dpg-test",
				"sql":        "SELECT 1",
			})

			require.True(t, result.IsError)
			text, ok := result.Content[0].(mcp.TextContent)
			require.True(t, ok)
			require.Contains(t, text.Text, tt.wantMessage)
			require.Equal(t, tt.wantConnectionInfoCalls, renderAPI.connectionInfoCallCount)
		})
	}
}

func createdBlueprintWithPlan(ownerID, repoURL string) blueprintclient.CreateBlueprintResponse {
	return blueprintclient.CreateBlueprintResponse{
		Blueprint: blueprintclient.Blueprint{
			Id: "exs-created", OwnerId: ownerID, Name: "my-app",
			Repo: repoURL, Branch: "main", Path: "render.yaml",
			AutoSync: true, Status: blueprintclient.StatusCreated,
		},
		PlanningOutcome: blueprintclient.PlanningOutcomePlanned,
		Sync: &blueprintclient.SyncWithPlan{
			Id: "exe-first", State: blueprintclient.SyncStateCreated,
			Commit:                blueprintclient.CommitRef{Id: "abc123"},
			DashboardUrl:          "https://dashboard.render.com/blueprints/exs-created/syncs/exe-first",
			RequiredManualEnvVars: []blueprintclient.RequiredManualEnvVar{},
		},
		Notices: []blueprintclient.BlueprintNotice{},
	}
}

func plannedBlueprintSync() blueprintclient.CreateBlueprintSyncResponse {
	return blueprintclient.CreateBlueprintSyncResponse{
		PlanningOutcome: blueprintclient.PlanningOutcomePlanned,
		Sync: &blueprintclient.SyncWithPlan{
			Id: "exe-planned", State: blueprintclient.SyncStateCreated,
			Commit:                blueprintclient.CommitRef{Id: "abc123"},
			DashboardUrl:          "https://dashboard.render.com/blueprints/exs-existing/syncs/exe-planned",
			RequiredManualEnvVars: []blueprintclient.RequiredManualEnvVar{},
		},
		Notices: []blueprintclient.BlueprintNotice{},
	}
}

func inSyncBlueprintWithResources(ownerID string) blueprintclient.BlueprintDetail {
	return blueprintclient.BlueprintDetail{
		Id: "exs-existing", OwnerId: ownerID, Name: "my-app",
		Repo: "https://github.com/example/my-app", Branch: "main", Path: "render.yaml",
		AutoSync: true, Status: blueprintclient.StatusInSync,
		Resources: []blueprintclient.ResourceRef{
			{Id: "srv-web", Name: "web", Type: blueprintclient.WebService},
		},
	}
}

func unapprovedBlueprintAndSync(ownerID string) (
	blueprintclient.BlueprintDetail, blueprintclient.ApprovedSync,
) {
	blueprint := blueprintclient.BlueprintDetail{
		Id: "exs-draft", OwnerId: ownerID, Name: "draft",
		Repo: "https://github.com/example/my-app", Branch: "main", Path: "render.yaml",
		Status:    blueprintclient.StatusCreated,
		Resources: []blueprintclient.ResourceRef{},
	}
	sync := blueprintclient.ApprovedSync{
		Id: "exe-queued", State: blueprintclient.SyncStatePending,
		Commit:       blueprintclient.CommitRef{Id: "commit-123"},
		DashboardUrl: "https://dashboard.render.com/blueprint/exs-draft/sync/exe-queued",
	}
	return blueprint, sync
}

func TestBlueprintToolRegistration(t *testing.T) {
	s := newMCPServer(nil)
	for _, name := range []string{"create_blueprint", "get_blueprint", "plan_blueprint", "approve_blueprint", "update_blueprint"} {
		t.Run(name, func(t *testing.T) {
			tool := s.GetTool(name)
			require.NotNil(t, tool)
			workspaceSchema, ok := tool.Tool.InputSchema.Properties["workspaceId"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "string", workspaceSchema["type"])
			require.Contains(t, workspaceSchema["description"], "list_workspaces")
			require.NotContains(t, tool.Tool.InputSchema.Required, "workspaceId")
		})
	}
}

// TestBlueprintToolsOverHTTP exercises the tool happy paths over a fully configured mcp server
func TestBlueprintToolsOverHTTP(t *testing.T) {
	t.Run("create forwards its inputs and returns the Blueprint and first plan", func(t *testing.T) {
		workspaceID := "tea-blueprints"
		repoURL := "https://github.com/example/my-app"
		want := createdBlueprintWithPlan(workspaceID, repoURL)

		ownerLookupReply := jsonReply(t, http.StatusOK, client.Owner{
			Id: workspaceID, Name: "Blueprints", Type: client.OwnerTypeTeam,
		})
		createBlueprintReply := jsonReply(t, http.StatusCreated, want)
		mux := http.NewServeMux()
		mux.HandleFunc("GET /v1/owners/"+workspaceID, ownerLookupReply)
		mux.HandleFunc("POST /v1/blueprints", func(w http.ResponseWriter, r *http.Request) {
			var request client.CreateBlueprintJSONRequestBody
			if !decodeJSONRequest(t, w, r, &request) {
				return
			}
			assert.Equal(t, workspaceID, request.OwnerId)
			assert.Equal(t, repoURL, request.Source.Repo)
			createBlueprintReply(w, r)
		})
		httpTransport := newHTTPTransportWithAPIMux(t, mux)

		sessionID := initializeHTTPSession(t, httpTransport)
		result := callHTTPTool(t, httpTransport, sessionID, "create_blueprint", map[string]any{
			"repoUrl":     repoURL,
			"workspaceId": workspaceID,
		})

		require.False(t, result.IsError, "%+v", result.Content)
		var got blueprintclient.CreateBlueprintResponse
		require.NoError(t, json.Unmarshal([]byte(toolResultText(t, result)), &got))
		assert.Equal(t, want.Blueprint.Id, got.Blueprint.Id)
		assert.Equal(t, want.PlanningOutcome, got.PlanningOutcome)
		require.NotNil(t, got.Sync)
		assert.Equal(t, want.Sync.Id, got.Sync.Id)
	})

	t.Run("get returns owned Blueprint details", func(t *testing.T) {
		const workspaceID = "tea-selected"
		want := inSyncBlueprintWithResources(workspaceID)

		getBlueprintReply := jsonReply(t, http.StatusOK, want)
		mux := http.NewServeMux()
		mux.HandleFunc("GET /v1/blueprints/"+want.Id, getBlueprintReply)
		httpTransport := newHTTPTransportWithAPIMux(t, mux)

		sessionID := initializeHTTPSession(t, httpTransport)
		result := callHTTPTool(t, httpTransport, sessionID, "get_blueprint",
			map[string]any{"blueprintId": want.Id, "workspaceId": workspaceID})

		require.False(t, result.IsError, "%+v", result.Content)
		var got blueprintclient.BlueprintDetail
		require.NoError(t, json.Unmarshal([]byte(toolResultText(t, result)), &got))
		assert.Equal(t, want.Id, got.Id)
		assert.Equal(t, want.OwnerId, got.OwnerId)
		assert.Equal(t, want.Resources, got.Resources)
	})

	t.Run("plan returns a planned sync", func(t *testing.T) {
		const workspaceID = "tea-selected"
		blueprint := inSyncBlueprintWithResources(workspaceID)
		want := plannedBlueprintSync()

		getBlueprintReply := jsonReply(t, http.StatusOK, blueprint)
		planBlueprintReply := jsonReply(t, http.StatusCreated, want)
		mux := http.NewServeMux()
		mux.HandleFunc("GET /v1/blueprints/"+blueprint.Id, getBlueprintReply)
		mux.HandleFunc("POST /v1/blueprints/"+blueprint.Id+"/syncs", planBlueprintReply)
		httpTransport := newHTTPTransportWithAPIMux(t, mux)

		sessionID := initializeHTTPSession(t, httpTransport)
		result := callHTTPTool(t, httpTransport, sessionID, "plan_blueprint",
			map[string]any{"blueprintId": blueprint.Id, "workspaceId": workspaceID})

		require.False(t, result.IsError, "%+v", result.Content)
		var got blueprintclient.CreateBlueprintSyncResponse
		require.NoError(t, json.Unmarshal([]byte(toolResultText(t, result)), &got))
		assert.Equal(t, want.PlanningOutcome, got.PlanningOutcome)
		require.NotNil(t, got.Sync)
		assert.Equal(t, want.Sync.Id, got.Sync.Id)
	})

	t.Run("approve sends manual values and returns a queued sync", func(t *testing.T) {
		const workspaceID = "tea-selected"
		blueprint, want := unapprovedBlueprintAndSync(workspaceID)
		manualValue := "example-value"
		wantManualEnvVars := []blueprintclient.SuppliedManualEnvVar{{
			ServiceType: blueprintclient.ManualEnvVarTargetTypeWeb,
			ServiceName: "web", Key: "TOKEN", Value: &manualValue,
		}}

		getBlueprintReply := jsonReply(t, http.StatusOK, blueprint)
		approveBlueprintReply := jsonReply(t, http.StatusOK, want)
		mux := http.NewServeMux()
		mux.HandleFunc("GET /v1/blueprints/"+blueprint.Id, getBlueprintReply)
		mux.HandleFunc("POST /v1/blueprints/"+blueprint.Id+"/approve",
			func(w http.ResponseWriter, r *http.Request) {
				var request client.ApproveBlueprintJSONRequestBody
				if !decodeJSONRequest(t, w, r, &request) {
					return
				}
				assert.Equal(t, &wantManualEnvVars, request.ManualEnvVars)
				approveBlueprintReply(w, r)
			})
		httpTransport := newHTTPTransportWithAPIMux(t, mux)

		sessionID := initializeHTTPSession(t, httpTransport)
		result := callHTTPTool(t, httpTransport, sessionID, "approve_blueprint", map[string]any{
			"blueprintId": blueprint.Id,
			"workspaceId": workspaceID,
			"manualEnvVars": []any{map[string]any{
				"serviceType": "web", "serviceName": "web", "key": "TOKEN", "value": manualValue,
			}},
		})

		require.False(t, result.IsError, "%+v", result.Content)
		var got blueprintclient.ApprovedSync
		require.NoError(t, json.Unmarshal([]byte(toolResultText(t, result)), &got))
		assert.Equal(t, want.Id, got.Id)
		assert.Equal(t, want.State, got.State)
	})

	t.Run("update changes the branch and returns a plan", func(t *testing.T) {
		const workspaceID = "tea-selected"
		current := inSyncBlueprintWithResources(workspaceID)
		updated := blueprintclient.Blueprint{
			Id: current.Id, OwnerId: current.OwnerId, Name: current.Name,
			Repo: current.Repo, Branch: "preview", Path: current.Path,
			AutoSync: current.AutoSync, Status: current.Status,
		}
		plan := plannedBlueprintSync()

		getBlueprintReply := jsonReply(t, http.StatusOK, current)
		updateBlueprintReply := jsonReply(t, http.StatusOK, updated)
		planReply := jsonReply(t, http.StatusCreated, plan)
		mux := http.NewServeMux()
		mux.HandleFunc("GET /v1/blueprints/"+current.Id, getBlueprintReply)
		mux.HandleFunc("PATCH /v1/blueprints/"+current.Id,
			func(w http.ResponseWriter, r *http.Request) {
				var request client.UpdateBlueprintJSONRequestBody
				if !decodeJSONRequest(t, w, r, &request) {
					return
				}
				assert.Equal(t, &updated.Branch, request.Branch)
				updateBlueprintReply(w, r)
			})
		mux.HandleFunc("POST /v1/blueprints/"+current.Id+"/syncs", planReply)
		httpTransport := newHTTPTransportWithAPIMux(t, mux)

		sessionID := initializeHTTPSession(t, httpTransport)
		result := callHTTPTool(t, httpTransport, sessionID, "update_blueprint", map[string]any{
			"blueprintId": current.Id,
			"workspaceId": workspaceID,
			"branch":      updated.Branch,
		})

		require.False(t, result.IsError, "%+v", result.Content)
		var got struct {
			Blueprint       blueprintclient.Blueprint        `json:"blueprint"`
			PlanningOutcome *blueprintclient.PlanningOutcome `json:"planningOutcome"`
			Sync            *blueprintclient.SyncWithPlan    `json:"sync"`
		}
		require.NoError(t, json.Unmarshal([]byte(toolResultText(t, result)), &got))
		assert.Equal(t, updated.Branch, got.Blueprint.Branch)
		require.NotNil(t, got.PlanningOutcome)
		assert.Equal(t, plan.PlanningOutcome, *got.PlanningOutcome)
		require.NotNil(t, got.Sync)
		assert.Equal(t, plan.Sync.Id, got.Sync.Id)
	})
}

func TestBlueprintWorkspaceResolutionOverHTTP(t *testing.T) {
	t.Run("requires a workspace when none is selected or supplied", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			args map[string]any
		}{
			{
				name: "create_blueprint",
				args: map[string]any{"repoUrl": "https://github.com/example/my-app"},
			},
			{
				name: "get_blueprint",
				args: map[string]any{"blueprintId": "exs-existing"},
			},
			{
				name: "plan_blueprint",
				args: map[string]any{"blueprintId": "exs-existing"},
			},
			{
				name: "approve_blueprint",
				args: map[string]any{"blueprintId": "exs-existing"},
			},
			{
				name: "update_blueprint",
				args: map[string]any{"blueprintId": "exs-existing", "branch": "preview"},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				existing := inSyncBlueprintWithResources("tea-blueprints")
				blueprintReply := jsonReply(t, http.StatusOK, existing)
				mux := http.NewServeMux()
				mux.HandleFunc("GET /v1/blueprints/"+existing.Id, blueprintReply)
				httpTransport := newHTTPTransportWithAPIMux(t, mux)

				sessionID := initializeHTTPSession(t, httpTransport)
				result := callHTTPTool(t, httpTransport, sessionID, tt.name, tt.args)

				require.True(t, result.IsError)
				assert.Contains(t, toolResultText(t, result), config.ErrNoWorkspace.Error())
			})
		}
	})

	t.Run("uses the selected session workspace when omitted", func(t *testing.T) {
		const workspaceID = "tea-selected"
		want := inSyncBlueprintWithResources(workspaceID)
		blueprintReply := jsonReply(t, http.StatusOK, want)

		mux := http.NewServeMux()
		mux.HandleFunc("GET /v1/blueprints/"+want.Id, blueprintReply)
		httpTransport, sessionID := startSelectedWorkspaceSession(t, mux, workspaceID)

		result := callHTTPTool(t, httpTransport, sessionID, "get_blueprint",
			map[string]any{"blueprintId": want.Id})

		require.False(t, result.IsError, "%+v", result.Content)
		var got blueprintclient.BlueprintDetail
		require.NoError(t, json.Unmarshal([]byte(toolResultText(t, result)), &got))
		assert.Equal(t, want.Id, got.Id)
		assert.Equal(t, workspaceID, got.OwnerId)
	})

	t.Run("uses a per-call override without changing the session selection", func(t *testing.T) {
		const selectedWorkspaceID = "tea-selected"
		blueprint, wantSync := unapprovedBlueprintAndSync("tea-other")
		getBlueprintReply := jsonReply(t, http.StatusOK, blueprint)
		approveBlueprintReply := jsonReply(t, http.StatusOK, wantSync)

		mux := http.NewServeMux()
		mux.HandleFunc("GET /v1/blueprints/"+blueprint.Id, getBlueprintReply)
		var approvalCalls atomic.Int32
		mux.HandleFunc("POST /v1/blueprints/"+blueprint.Id+"/approve",
			func(w http.ResponseWriter, r *http.Request) {
				approvalCalls.Add(1)
				approveBlueprintReply(w, r)
			})
		httpTransport, sessionID := startSelectedWorkspaceSession(t, mux, selectedWorkspaceID)

		result := callHTTPTool(t, httpTransport, sessionID, "approve_blueprint", map[string]any{
			"blueprintId": blueprint.Id,
			"workspaceId": blueprint.OwnerId,
		})

		require.False(t, result.IsError, "%+v", result.Content)
		var got blueprintclient.ApprovedSync
		require.NoError(t, json.Unmarshal([]byte(toolResultText(t, result)), &got))
		assert.Equal(t, wantSync.Id, got.Id)
		assert.Equal(t, int32(1), approvalCalls.Load())

		selection := callHTTPTool(t, httpTransport, sessionID, "get_selected_workspace", nil)
		require.False(t, selection.IsError, "%+v", selection.Content)
		assert.Equal(t, "The currently selected workspace is: "+selectedWorkspaceID,
			toolResultText(t, selection))
	})
}

func TestBlueprintWorkspaceAccessOverHTTP(t *testing.T) {
	t.Run("create rejects an inaccessible workspace before the API request", func(t *testing.T) {
		workspaceID := "tea-inaccessible"
		ownerLookupReply := jsonReply(t, http.StatusForbidden, map[string]string{"message": "Forbidden"})

		mux := http.NewServeMux()
		mux.HandleFunc("GET /v1/owners/"+workspaceID, ownerLookupReply)
		httpTransport := newHTTPTransportWithAPIMux(t, mux)

		sessionID := initializeHTTPSession(t, httpTransport)
		result := callHTTPTool(t, httpTransport, sessionID, "create_blueprint", map[string]any{
			"repoUrl":     "https://github.com/example/my-app",
			"workspaceId": workspaceID,
		})

		require.True(t, result.IsError)
		assert.Contains(t, toolResultText(t, result),
			"cannot access workspace tea-inaccessible: forbidden")
	})

	t.Run("tools reject a Blueprint owned by another workspace", func(t *testing.T) {
		const selectedWorkspaceID = "tea-selected"
		foreign, _ := unapprovedBlueprintAndSync("tea-other")

		for _, tt := range []struct {
			name string
			args map[string]any
		}{
			{name: "get_blueprint", args: map[string]any{"blueprintId": foreign.Id}},
			{name: "plan_blueprint", args: map[string]any{"blueprintId": foreign.Id}},
			{name: "approve_blueprint", args: map[string]any{"blueprintId": foreign.Id}},
			{name: "update_blueprint", args: map[string]any{"blueprintId": foreign.Id, "branch": "preview"}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				blueprintReply := jsonReply(t, http.StatusOK, foreign)
				mux := http.NewServeMux()
				mux.HandleFunc("GET /v1/blueprints/"+foreign.Id, blueprintReply)
				httpTransport, sessionID := startSelectedWorkspaceSession(t, mux, selectedWorkspaceID)

				result := callHTTPTool(t, httpTransport, sessionID, tt.name, tt.args)

				require.True(t, result.IsError)
				text := toolResultText(t, result)
				assert.Contains(t, text, "belongs to workspace "+foreign.OwnerId)
				assert.Contains(t, text, "targets workspace "+selectedWorkspaceID)
			})
		}
	})
}

func TestWorkspaceScopedToolsAcceptOptionalWorkspaceID(t *testing.T) {
	tools := buildWorkspaceScopedTools(nil)
	require.NotEmpty(t, tools)

	for _, tool := range tools {
		workspaceSchema, ok := tool.Tool.InputSchema.Properties["workspaceId"].(map[string]interface{})
		require.True(t, ok, "%s does not define workspaceId", tool.Tool.Name)
		require.Equal(t, "string", workspaceSchema["type"], tool.Tool.Name)
		require.Contains(t, workspaceSchema["description"], "list_workspaces", tool.Tool.Name)
		require.NotContains(t, tool.Tool.InputSchema.Required, "workspaceId", tool.Tool.Name)
	}
}

// Exercises list_events over the real HTTP transport, covering tool
// registration, workspace scoping, filter encoding, and event details reaching
// the caller.
func TestListEventsToolOverHTTP(t *testing.T) {
	httpTransport, renderAPI := newWorkspaceHTTPTestServer(t)
	renderAPI.serviceOwnerID = "tea-events"

	sessionID := initializeHTTPSession(t, httpTransport)

	result := callHTTPTool(t, httpTransport, sessionID, "list_events", map[string]any{
		"serviceId":   "srv-123456",
		"workspaceId": "tea-events",
		"eventTypes":  []any{"server_failed", "deploy_ended"},
	})
	require.False(t, result.IsError, "%+v", result.Content)

	require.Len(t, renderAPI.eventQueries, 1)
	query := renderAPI.eventQueries[0]
	require.Equal(t, []string{"server_failed,deploy_ended"}, query["type"])
	require.Equal(t, "20", query.Get("limit"))
	require.NotEmpty(t, query.Get("startTime"))

	content, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, content.Text, "evt-abc")
	require.Contains(t, content.Text, "oomKilled")
	require.Contains(t, content.Text, "512MB")
	require.True(t, strings.HasSuffix(content.Text, "\n\n cursor: evt-abc-cursor"), content.Text)
}

// Paging over HTTP forwards the cursor to the API as a query param.
func TestListEventsToolForwardsCursorOverHTTP(t *testing.T) {
	httpTransport, renderAPI := newWorkspaceHTTPTestServer(t)
	renderAPI.serviceOwnerID = "tea-events"

	sessionID := initializeHTTPSession(t, httpTransport)

	result := callHTTPTool(t, httpTransport, sessionID, "list_events", map[string]any{
		"serviceId":   "srv-123456",
		"workspaceId": "tea-events",
		"cursor":      "evt-abc-cursor",
	})
	require.False(t, result.IsError, "%+v", result.Content)

	require.Len(t, renderAPI.eventQueries, 1)
	require.Equal(t, "evt-abc-cursor", renderAPI.eventQueries[0].Get("cursor"))
}

// A service owned by another workspace must not return events.
func TestListEventsToolRejectsForeignWorkspace(t *testing.T) {
	httpTransport, renderAPI := newWorkspaceHTTPTestServer(t)
	renderAPI.serviceOwnerID = "tea-owner"

	sessionID := initializeHTTPSession(t, httpTransport)

	result := callHTTPTool(t, httpTransport, sessionID, "list_events", map[string]any{
		"serviceId":   "srv-123456",
		"workspaceId": "tea-intruder",
	})
	require.True(t, result.IsError)
	require.Empty(t, renderAPI.eventQueries)
}

func newSessionlessStdioTestClient(t *testing.T, apiClient *client.ClientWithResponses) *mcpclient.Client {
	t.Helper()
	_, stdioTransport := newStdioServer(apiClient)
	clientConn, serverConn := net.Pipe()
	ctx := logging.ContextWithDefaultLogger(t.Context())
	var serverTasks sync.WaitGroup
	serverTasks.Go(func() {
		err := stdioTransport.Listen(ctx, serverConn, serverConn)
		_ = serverConn.Close()
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("stdio server stopped unexpectedly: %v", err)
		}
	})

	mcpClient := mcpclient.NewClient(
		transport.NewIO(clientConn, clientConn, nil),
		mcpclient.WithProtocolVersion(mcp.ProtocolVersion20260728),
	)
	t.Cleanup(func() {
		closeErr := mcpClient.Close()
		serverTasks.Wait()
		require.NoError(t, closeErr)
	})
	require.NoError(t, mcpClient.Start(ctx))
	// For the sessionless protocol, Initialize uses server/discover rather than
	// the initialize handshake. Assert the version so fallback cannot mask a failure.
	initialized, err := mcpClient.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{ClientInfo: mcp.Implementation{Name: "test", Version: "1"}},
	})
	require.NoError(t, err)
	require.Equal(t, mcp.ProtocolVersion20260728, initialized.ProtocolVersion)
	return mcpClient
}

func newWorkspaceHTTPTestServer(t *testing.T) (*server.StreamableHTTPServer, *fakeRenderAPI) {
	t.Helper()
	apiClient, renderAPI := newTestRenderClient(t)
	_, httpTransport := newStreamableHTTPServer(apiClient, session.NewInMemoryStore())
	t.Cleanup(func() { require.NoError(t, httpTransport.Shutdown(t.Context())) })
	return httpTransport, renderAPI
}

func newTestRenderClient(t *testing.T) (*client.ClientWithResponses, *fakeRenderAPI) {
	t.Helper()
	renderAPI := &fakeRenderAPI{}
	apiClient, err := client.NewClientWithResponses(
		"https://api.example.com/v1",
		client.WithHTTPClient(renderAPI),
	)
	require.NoError(t, err)
	return apiClient, renderAPI
}

// fakeRenderAPI is a stub that records the workspace IDs and event filters the mcp server sends to the render API,
// and serves the service and Postgres bodies tests set. This stub is not intended for concurrent use.
type fakeRenderAPI struct {
	workspaceIDs            []string
	servicesCallCount       int
	serviceOwnerID          string
	eventQueries            []url.Values
	postgres                string
	connectionInfo          string
	connectionInfoCallCount int
}

func (f *fakeRenderAPI) Do(r *http.Request) (*http.Response, error) {
	var body string
	switch {
	case strings.HasPrefix(r.URL.Path, "/v1/owners/"):
		// Return a successful workspace lookup so workspace access validation
		// does not prevent these tests from exercising workspace selection.
		id := strings.TrimPrefix(r.URL.Path, "/v1/owners/")
		body = `{"id":"` + id + `","name":"Workspace"}`
	case r.URL.Path == "/v1/services":
		f.servicesCallCount++
		// Record the requested workspace (ownerId) so tests can verify workspace
		// selection. Service contents are irrelevant, so return an empty list.
		f.workspaceIDs = append(f.workspaceIDs, r.URL.Query().Get("ownerId"))
		body = "[]"
	case strings.HasSuffix(r.URL.Path, "/events"):
		// Record the query so tests can verify the filters the tool sent.
		f.eventQueries = append(f.eventQueries, r.URL.Query())
		body = `[{"cursor":"evt-abc-cursor","event":{"id":"evt-abc","serviceId":"srv-123456",` +
			`"type":"server_failed","timestamp":"2026-09-20T00:00:00Z",` +
			`"details":{"reason":{"evicted":false,"oomKilled":{"memoryLimit":"512MB"}}}}}]`
	case strings.HasPrefix(r.URL.Path, "/v1/services/"):
		// Service lookup used by the workspace-ownership check.
		id := strings.TrimPrefix(r.URL.Path, "/v1/services/")
		body = `{"id":"` + id + `","ownerId":"` + f.serviceOwnerID + `","type":"web_service"}`
	case strings.HasPrefix(r.URL.Path, "/v1/postgres/") && strings.HasSuffix(r.URL.Path, "/connection-info"):
		f.connectionInfoCallCount++
		body = f.connectionInfo
	case strings.HasPrefix(r.URL.Path, "/v1/postgres/"):
		body = f.postgres
	default:
		return nil, fmt.Errorf("unexpected API request: %s", r.URL.Path)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func (f *fakeRenderAPI) lastWorkspaceID() string {
	if len(f.workspaceIDs) <= 0 {
		return ""
	}
	return f.workspaceIDs[len(f.workspaceIDs)-1]
}

func jsonReply(t *testing.T, status int, value any) http.HandlerFunc {
	t.Helper()
	body, err := json.Marshal(value)
	require.NoError(t, err)

	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if _, err := w.Write(body); err != nil {
			t.Errorf("write API response: %v", err)
		}
	}
}

// decodeJSONRequest reports malformed fixture requests and writes a 400 response.
func decodeJSONRequest(t *testing.T, w http.ResponseWriter, r *http.Request, request any) bool {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(request); err != nil {
		t.Errorf("decode API request %s %s: %v", r.Method, r.URL.Path, err)
		w.WriteHeader(http.StatusBadRequest)
		return false
	}
	return true
}

// newHTTPTransportWithAPIMux fails on any API request the test did not register.
func newHTTPTransportWithAPIMux(t *testing.T, mux *http.ServeMux) *server.StreamableHTTPServer {
	t.Helper()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API request: %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	})

	renderAPI := httptest.NewServer(mux)
	t.Cleanup(renderAPI.Close)
	apiClient, err := client.NewClientWithResponses(renderAPI.URL + "/v1")
	require.NoError(t, err)
	// use the same constructor as the actual http server
	_, httpTransport := newStreamableHTTPServer(apiClient, session.NewInMemoryStore())
	t.Cleanup(func() { require.NoError(t, httpTransport.Shutdown(t.Context())) })
	return httpTransport
}

func startSelectedWorkspaceSession(
	t *testing.T, mux *http.ServeMux, workspaceID string,
) (*server.StreamableHTTPServer, string) {
	t.Helper()
	mux.HandleFunc("GET /v1/owners/"+workspaceID, jsonReply(t, http.StatusOK, client.Owner{
		Id: workspaceID, Name: "Selected workspace", Type: client.OwnerTypeTeam,
	}))
	httpTransport := newHTTPTransportWithAPIMux(t, mux)
	sessionID := initializeHTTPSession(t, httpTransport)

	selection := callHTTPTool(t, httpTransport, sessionID, "select_workspace",
		map[string]any{"ownerID": workspaceID})
	require.False(t, selection.IsError, "%+v", selection.Content)
	return httpTransport, sessionID
}

func toolResultText(t *testing.T, result mcp.CallToolResult) string {
	t.Helper()
	require.Len(t, result.Content, 1)
	content, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	return content.Text
}

// initializeHTTPSession performs the MCP handshake and returns the ID clients
// send on subsequent HTTP requests.
func initializeHTTPSession(t *testing.T, handler http.Handler) string {
	t.Helper()
	rec := postMCP(t, handler, "", string(mcp.MethodInitialize), mcp.InitializeParams{
		ProtocolVersion: mcp.ProtocolVersion20250326,
		Capabilities:    mcp.ClientCapabilities{},
		ClientInfo:      mcp.Implementation{Name: "test", Version: "1"},
	})
	require.Equal(t, http.StatusOK, rec.Code)
	sessionID := rec.Header().Get("Mcp-Session-Id")
	require.NotEmpty(t, sessionID)
	return sessionID
}

// callHTTPTool checks transport and JSON-RPC success. The caller checks the
// tool result, which may report an error even when the request succeeds.
func callHTTPTool(t *testing.T, handler http.Handler, sessionID, toolName string, arguments map[string]any) mcp.CallToolResult {
	t.Helper()
	rec := postMCP(t, handler, sessionID, string(mcp.MethodToolsCall), mcp.CallToolParams{
		Name:      toolName,
		Arguments: arguments,
	})
	require.Equal(t, http.StatusOK, rec.Code)
	var response struct {
		Result *mcp.CallToolResult
		Error  *mcp.JSONRPCErrorDetails
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Nil(t, response.Error)
	require.NotNil(t, response.Result)
	return *response.Result
}

func postMCP(t *testing.T, handler http.Handler, sessionID, method string, params any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(1),
		Method:  method,
		Params:  params,
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req = req.WithContext(logging.ContextWithDefaultLogger(req.Context()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", sessionID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
