package httpcontext

import (
	"context"
	"net/http"
	"testing"
)

func TestFromContext_EmptyContext(t *testing.T) {
	ctx := context.Background()
	hc := FromContext(ctx)

	if hc.UserAgent != "" {
		t.Errorf("expected empty UserAgent, got %q", hc.UserAgent)
	}
	if hc.ForwardedFor != "" {
		t.Errorf("expected empty ForwardedFor, got %q", hc.ForwardedFor)
	}
}

func TestContextWithHTTPContext_RoundTrip(t *testing.T) {
	ctx := context.Background()
	expected := HTTPContext{
		UserAgent:    "TestAgent/1.0",
		ForwardedFor: "10.0.0.1, 192.168.1.1",
	}

	ctx = ContextWithHTTPContext(ctx, expected)
	actual := FromContext(ctx)

	if actual.UserAgent != expected.UserAgent {
		t.Errorf("expected UserAgent %q, got %q", expected.UserAgent, actual.UserAgent)
	}
	if actual.ForwardedFor != expected.ForwardedFor {
		t.Errorf("expected ForwardedFor %q, got %q", expected.ForwardedFor, actual.ForwardedFor)
	}
}

func TestContextWithHTTPRequest_UserAgent(t *testing.T) {
	ctx := context.Background()
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", "Claude-Desktop/1.2.3")
	req.RemoteAddr = "192.168.1.100:54321"

	ctx = ContextWithHTTPRequest(ctx, req)
	hc := FromContext(ctx)

	if hc.UserAgent != "Claude-Desktop/1.2.3" {
		t.Errorf("expected UserAgent %q, got %q", "Claude-Desktop/1.2.3", hc.UserAgent)
	}
}

func TestContextWithHTTPRequest_XFFChainBuilding(t *testing.T) {
	tests := []struct {
		name       string
		existingXFF string
		remoteAddr string
		expectedXFF string
	}{
		{
			name:        "no existing XFF",
			existingXFF: "",
			remoteAddr:  "192.168.1.100:54321",
			expectedXFF: "192.168.1.100",
		},
		{
			name:        "with existing XFF",
			existingXFF: "10.0.0.1",
			remoteAddr:  "192.168.1.100:54321",
			expectedXFF: "10.0.0.1, 192.168.1.100",
		},
		{
			name:        "multiple entries in existing XFF",
			existingXFF: "10.0.0.1, 172.16.0.1",
			remoteAddr:  "192.168.1.100:54321",
			expectedXFF: "10.0.0.1, 172.16.0.1, 192.168.1.100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			req, _ := http.NewRequest("GET", "/", nil)
			if tt.existingXFF != "" {
				req.Header.Set("X-Forwarded-For", tt.existingXFF)
			}
			req.RemoteAddr = tt.remoteAddr

			ctx = ContextWithHTTPRequest(ctx, req)
			hc := FromContext(ctx)

			if hc.ForwardedFor != tt.expectedXFF {
				t.Errorf("expected ForwardedFor %q, got %q", tt.expectedXFF, hc.ForwardedFor)
			}
		})
	}
}

func TestContextWithHTTPRequest_LastEntryDeduplication(t *testing.T) {
	ctx := context.Background()
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.1.100")
	req.RemoteAddr = "192.168.1.100:54321" // Same as last entry in XFF

	ctx = ContextWithHTTPRequest(ctx, req)
	hc := FromContext(ctx)

	// Should not add duplicate
	expected := "10.0.0.1, 192.168.1.100"
	if hc.ForwardedFor != expected {
		t.Errorf("expected ForwardedFor %q (no duplicate), got %q", expected, hc.ForwardedFor)
	}
}

func TestContextWithHTTPRequest_EarlierDuplicatePreserved(t *testing.T) {
	ctx := context.Background()
	req, _ := http.NewRequest("GET", "/", nil)
	// IP appears earlier in chain but not as last entry
	req.Header.Set("X-Forwarded-For", "192.168.1.100, 10.0.0.1")
	req.RemoteAddr = "192.168.1.100:54321"

	ctx = ContextWithHTTPRequest(ctx, req)
	hc := FromContext(ctx)

	// Should add it since it's not the last entry
	expected := "192.168.1.100, 10.0.0.1, 192.168.1.100"
	if hc.ForwardedFor != expected {
		t.Errorf("expected ForwardedFor %q (earlier duplicate preserved), got %q", expected, hc.ForwardedFor)
	}
}

func TestGetClientIP_PortStripping(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		expected   string
	}{
		{
			name:       "IPv4 with port",
			remoteAddr: "192.168.1.100:54321",
			expected:   "192.168.1.100",
		},
		{
			name:       "IPv6 with port",
			remoteAddr: "[::1]:54321",
			expected:   "::1",
		},
		{
			name:       "IPv4 without port",
			remoteAddr: "192.168.1.100",
			expected:   "192.168.1.100",
		},
		{
			name:       "empty string",
			remoteAddr: "",
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getClientIP(tt.remoteAddr)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestBuildXFF(t *testing.T) {
	tests := []struct {
		name        string
		existingXFF string
		remoteAddr  string
		expected    string
	}{
		{
			name:        "empty XFF, valid remoteAddr",
			existingXFF: "",
			remoteAddr:  "192.168.1.100:54321",
			expected:    "192.168.1.100",
		},
		{
			name:        "existing XFF, valid remoteAddr",
			existingXFF: "10.0.0.1",
			remoteAddr:  "192.168.1.100:54321",
			expected:    "10.0.0.1, 192.168.1.100",
		},
		{
			name:        "empty remoteAddr",
			existingXFF: "10.0.0.1",
			remoteAddr:  "",
			expected:    "10.0.0.1",
		},
		{
			name:        "both empty",
			existingXFF: "",
			remoteAddr:  "",
			expected:    "",
		},
		{
			name:        "XFF with spaces",
			existingXFF: "10.0.0.1,  172.16.0.1",
			remoteAddr:  "192.168.1.100:54321",
			expected:    "10.0.0.1,  172.16.0.1, 192.168.1.100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildXFF(tt.existingXFF, tt.remoteAddr)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

