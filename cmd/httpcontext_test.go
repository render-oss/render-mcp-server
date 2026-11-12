package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/httpcontext"
	"github.com/render-oss/render-mcp-server/pkg/logging"
	"github.com/render-oss/render-mcp-server/pkg/oauth"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/stretchr/testify/require"
)

func TestHTTPMetadataBeforeAuthentication(t *testing.T) {
	var observed httpcontext.HTTPContext
	previousTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		observed = httpcontext.FromContext(req.Context())
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	cfg := oauth.Config{
		Enabled:                true,
		AuthorizationServerURL: "https://as.example.com",
		CanonicalResourceURI:   "https://mcp.example.com/mcp",
	}
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	mux := newHTTPMux(next, cfg, "")
	handler := withMiddleware(mux, logging.RequestLoggerMiddleware, httpcontext.Middleware)
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer synthetic-api-token")
	req.Header.Set("Rndr-Id", "authentication-request")
	req.Header.Set("User-Agent", "context-test-client")
	req.Header.Set("X-Forwarded-For", "192.0.2.1")
	req.RemoteAddr = "192.0.2.2:1234"

	handler.ServeHTTP(httptest.NewRecorder(), req)

	require.Equal(t, httpcontext.HTTPContext{
		RequestID:    "authentication-request",
		UserAgent:    "context-test-client",
		ForwardedFor: "192.0.2.1, 192.0.2.2",
	}, observed)
}

func TestHTTPRequestIDReachesMCPHandler(t *testing.T) {
	apiClient, _ := newTestRenderClient(t)
	mcpServer, httpTransport := newStreamableHTTPServer(apiClient, session.NewInMemoryStore())
	t.Cleanup(func() { require.NoError(t, httpTransport.Shutdown(t.Context())) })
	mcpServer.AddTool(mcp.NewTool("context_probe"), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText(httpcontext.FromContext(ctx).RequestID), nil
	})
	mux := newHTTPMux(httpTransport, oauth.Config{Enabled: false}, "")
	handler := withMiddleware(mux, logging.RequestLoggerMiddleware, httpcontext.Middleware)
	sessionID := initializeHTTPSession(t, handler)
	requestHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Rndr-Id", "request-id")
		handler.ServeHTTP(w, r)
	})

	result := callHTTPTool(t, requestHandler, sessionID, "context_probe", nil)

	require.False(t, result.IsError)
	require.Equal(t, "request-id", toolResultText(t, result))
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
