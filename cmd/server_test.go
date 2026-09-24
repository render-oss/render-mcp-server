package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/oauth"
	"github.com/render-oss/render-mcp-server/pkg/session"
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

		requests := []mcp.JSONRPCRequest{
			{
				JSONRPC: mcp.JSONRPC_VERSION,
				ID:      mcp.NewRequestId(1),
				Method:  string(mcp.MethodInitialize),
				Params: mcp.InitializeParams{
					ProtocolVersion: givenVersion,
					Capabilities:    mcp.ClientCapabilities{},
					ClientInfo:      mcp.Implementation{Name: "test", Version: "1"},
				},
			},
			{
				JSONRPC: mcp.JSONRPC_VERSION,
				ID:      mcp.NewRequestId(2),
				Method:  string(mcp.MethodToolsCall),
				Params:  mcp.CallToolParams{Name: "get_selected_workspace"},
			},
		}
		var input, output bytes.Buffer
		encoder := json.NewEncoder(&input)
		for _, request := range requests {
			require.NoError(t, encoder.Encode(request))
		}

		require.NoError(t, stdioTransport.Listen(t.Context(), &input, &output))

		decoder := json.NewDecoder(&output)
		var initialized struct {
			ID     int
			Result mcp.InitializeResult
			Error  *mcp.JSONRPCErrorDetails
		}
		require.NoError(t, decoder.Decode(&initialized))
		require.Equal(t, 1, initialized.ID)
		require.Nil(t, initialized.Error)
		require.Equal(t, wantVersion, initialized.Result.ProtocolVersion)
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
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))

		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Contains(t, rec.Header().Get("WWW-Authenticate"), "resource_metadata=")
		require.False(t, *called)
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
	ctx := t.Context()
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

// fakeRenderAPI is a stub that tracks the workspace IDs that the mcp server presents to the render API when listing
// services associated with the workspace ID. This stub is not intended for concurrent use.
type fakeRenderAPI struct {
	workspaceIDs      []string
	servicesCallCount int
	serviceOwnerID    string
	eventQueries      []url.Values
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
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", sessionID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
