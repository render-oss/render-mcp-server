package httpcontext_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/render-oss/render-mcp-server/pkg/cfg"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/httpcontext"
)

// TestEndToEndHeaderPassthrough verifies the complete flow from incoming HTTP request
// to outgoing API headers.
func TestEndToEndHeaderPassthrough(t *testing.T) {
	// Simulate an incoming MCP HTTP request
	incomingReq, _ := http.NewRequest("POST", "/mcp", nil)
	incomingReq.Header.Set("User-Agent", "Claude-Desktop/1.2.3")
	incomingReq.Header.Set("X-Forwarded-For", "10.0.0.1, 172.16.0.1")
	incomingReq.RemoteAddr = "192.168.1.100:54321"

	// Extract HTTP context (this happens in MultiHTTPContextFunc chain)
	ctx := context.Background()
	ctx = httpcontext.ContextWithHTTPRequest(ctx, incomingReq)

	// Simulate outgoing API request header creation
	outgoingHeaders := make(http.Header)
	outgoingHeaders = client.AddHeaders(ctx, outgoingHeaders, "test-token")

	// Verify User-Agent is combined
	userAgent := outgoingHeaders.Get("User-Agent")
	expectedUAPrefix := "render-mcp-server/"
	expectedUASuffix := "Claude-Desktop/1.2.3"

	if !strings.HasPrefix(userAgent, expectedUAPrefix) {
		t.Errorf("User-Agent should start with %q, got %q", expectedUAPrefix, userAgent)
	}
	if !strings.HasSuffix(userAgent, expectedUASuffix) {
		t.Errorf("User-Agent should end with %q, got %q", expectedUASuffix, userAgent)
	}

	// Verify X-Forwarded-For chain is built correctly
	xff := outgoingHeaders.Get("X-Forwarded-For")
	expectedXFF := "10.0.0.1, 172.16.0.1, 192.168.1.100"
	if xff != expectedXFF {
		t.Errorf("X-Forwarded-For should be %q, got %q", expectedXFF, xff)
	}

	// Verify authorization header is set
	auth := outgoingHeaders.Get("Authorization")
	if auth != "Bearer test-token" {
		t.Errorf("Authorization should be %q, got %q", "Bearer test-token", auth)
	}

	t.Logf("User-Agent: %s", userAgent)
	t.Logf("X-Forwarded-For: %s", xff)
	t.Logf("Authorization: %s", auth)
}

// TestEndToEndWithoutHTTPContext verifies graceful degradation when no HTTP context
// is available (STDIO mode).
func TestEndToEndWithoutHTTPContext(t *testing.T) {
	// No HTTP context set - simulates STDIO mode
	ctx := context.Background()

	outgoingHeaders := make(http.Header)
	outgoingHeaders = client.AddHeaders(ctx, outgoingHeaders, "test-token")

	// Verify User-Agent contains only server info (no client UA appended)
	userAgent := outgoingHeaders.Get("User-Agent")
	if !strings.HasPrefix(userAgent, "render-mcp-server/") {
		t.Errorf("User-Agent should start with render-mcp-server/, got %q", userAgent)
	}
	// Should NOT contain extra space at the end (no client UA)
	if strings.HasSuffix(userAgent, " ") {
		t.Errorf("User-Agent should not have trailing space, got %q", userAgent)
	}

	// Verify X-Forwarded-For is NOT set
	xff := outgoingHeaders.Get("X-Forwarded-For")
	if xff != "" {
		t.Errorf("X-Forwarded-For should be empty in STDIO mode, got %q", xff)
	}

	t.Logf("User-Agent (STDIO mode): %s", userAgent)
}

// TestUserAgentFormat verifies the exact User-Agent format
func TestUserAgentFormat(t *testing.T) {
	header := make(http.Header)
	header = cfg.AddUserAgent(header, "TestClient/2.0")

	ua := header.Get("User-Agent")

	// Should be: render-mcp-server/VERSION (OS) TestClient/2.0
	if !strings.Contains(ua, "render-mcp-server/") {
		t.Errorf("User-Agent missing server identifier: %s", ua)
	}
	if !strings.Contains(ua, "(") || !strings.Contains(ua, ")") {
		t.Errorf("User-Agent missing OS info in parentheses: %s", ua)
	}
	if !strings.HasSuffix(ua, "TestClient/2.0") {
		t.Errorf("User-Agent should end with client UA, got: %s", ua)
	}

	t.Logf("Full User-Agent: %s", ua)
}
