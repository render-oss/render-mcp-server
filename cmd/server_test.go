package cmd

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/render-oss/render-mcp-server/pkg/oauth"
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

func TestNewHTTPMux_OAuthDisabledIsPassthrough(t *testing.T) {
	mcp, called := recordingHandler()
	mux := newHTTPMux(mcp, oauth.Config{}, "")

	// /mcp reaches the MCP handler with no challenge — unchanged from pre-OAuth.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	require.True(t, *called)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Header().Get("WWW-Authenticate"))

	// Metadata is not advertised when disabled.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestNewHTTPMux_OAuthEnabled(t *testing.T) {
	cfg := oauth.Config{
		Enabled:                true,
		AuthorizationServerURL: "https://as.example.com",
		CanonicalResourceURI:   "https://mcp.example.com/mcp",
		APIKeyPassthrough:      true,
	}
	mcp, called := recordingHandler()
	mux := newHTTPMux(mcp, cfg, "")

	// Path-aware RFC 9728 metadata is served.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), cfg.CanonicalResourceURI)

	// /mcp without credentials is challenged and the MCP handler is not reached.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Header().Get("WWW-Authenticate"), "resource_metadata=")
	require.False(t, *called)
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

func TestEveryToolHasExpectedReadOnlyClassification(t *testing.T) {
	tools := buildTools(nil)
	readOnlyNames := []string{
		"get_deploy", "get_key_value", "get_metrics", "get_postgres", "get_selected_workspace",
		"get_service", "list_deploys", "list_key_value", "list_log_label_values", "list_logs",
		"list_postgres_instances", "list_services", "list_workspaces", "query_render_postgres",
	}
	mutatingNames := []string{
		"create_cron_job", "create_key_value", "create_postgres", "create_static_site", "create_web_service",
		"select_workspace", "trigger_deploy", "update_environment_variables",
	}

	actualReadOnly := make([]string, 0, len(readOnlyNames))
	actualMutating := make([]string, 0, len(mutatingNames))
	for _, tool := range tools {
		require.NotNil(t, tool.Tool.Annotations.ReadOnlyHint, "%s is unclassified", tool.Tool.Name)
		if *tool.Tool.Annotations.ReadOnlyHint {
			actualReadOnly = append(actualReadOnly, tool.Tool.Name)
		} else {
			actualMutating = append(actualMutating, tool.Tool.Name)
		}
	}
	sort.Strings(actualReadOnly)
	sort.Strings(actualMutating)
	sort.Strings(readOnlyNames)
	sort.Strings(mutatingNames)
	require.Equal(t, readOnlyNames, actualReadOnly)
	require.Equal(t, mutatingNames, actualMutating)
}

func TestNewHTTPMux_OpenAIChallenge(t *testing.T) {
	mcp, _ := recordingHandler()

	// Served only when the token is configured.
	mux := newHTTPMux(mcp, oauth.Config{}, "verify-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/openai-apps-challenge", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "verify-token", rec.Body.String())

	// Absent token: the route is not registered.
	mux = newHTTPMux(mcp, oauth.Config{}, "")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/openai-apps-challenge", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
}
