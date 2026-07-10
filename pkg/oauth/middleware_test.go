package oauth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/render-oss/render-mcp-server/pkg/authn"
	"github.com/stretchr/testify/require"
)

const testResource = "https://mcp.example.com/mcp"

func testConfig() Config {
	return Config{
		Enabled:                true,
		AuthorizationServerURL: "https://api.example.com",
		CanonicalResourceURI:   testResource,
		APIKeyPassthrough:      true,
	}
}

// nextRecorder is a downstream handler that records whether it ran and what
// the middleware attached to the request context.
type nextRecorder struct {
	called        bool
	token         string
	introspection IntrospectionResponse
	hasResult     bool
}

func (n *nextRecorder) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.called = true
		n.token = authn.APITokenFromContext(r.Context())
		n.introspection, n.hasResult = IntrospectionFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
}

// serveThrough runs one request through Middleware backed by a fake
// authorization server that returns introspectionBody.
func serveThrough(t *testing.T, cfg Config, introspectionBody string, mutate func(*http.Request)) (*httptest.ResponseRecorder, *nextRecorder) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if introspectionBody == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(introspectionBody))
	}))
	t.Cleanup(srv.Close)

	next := &nextRecorder{}
	handler := Middleware(cfg, NewIntrospector(srv.URL, "", time.Minute))(next.handler())
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer the-token")
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, next
}

// activeBody is activeJSON (introspect_test.go) anchored at the current time.
func activeBody(aud string) string {
	return activeJSON(time.Now(), aud)
}

func activeBodyWithExp(aud string, exp time.Time) string {
	return fmt.Sprintf(`{"active":true,"aud":%q,"exp":%d}`, aud, exp.Unix())
}

func TestMiddleware_DisabledIsIdentity(t *testing.T) {
	next := &nextRecorder{}
	handler := Middleware(Config{}, nil)(next.handler())
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil) // no Authorization at all
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.True(t, next.called)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Header().Get("WWW-Authenticate"))
}

func TestMiddleware(t *testing.T) {
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Minute)

	cases := []struct {
		name           string
		passthroughOff bool
		introspection  string // fake AS response body; "" makes it answer 500
		authHeader     string // Authorization value; "" omits the header
		wantStatus     int
		wantNextCalled bool
		wantToken      string // asserted only when wantNextCalled
		wantHasResult  bool   // introspection result attached to context?
		wantSubject    string // asserted when non-empty
		challengeHas   []string
		challengeLacks []string
		wantRetryAfter bool
	}{
		{
			name:           "active token, matching audience, proceeds with result",
			introspection:  activeBody(testResource),
			authHeader:     "Bearer the-token",
			wantStatus:     http.StatusOK,
			wantNextCalled: true,
			wantToken:      "the-token",
			wantHasResult:  true,
			wantSubject:    "user:1",
		},
		{
			name:           "bearer scheme is case-insensitive",
			introspection:  activeBody(testResource),
			authHeader:     "bearer the-token",
			wantStatus:     http.StatusOK,
			wantNextCalled: true,
			wantToken:      "the-token",
			wantHasResult:  true,
		},
		{
			name:           "equivalent audience spelling matches",
			introspection:  activeBody("HTTPS://MCP.Example.com:443/mcp"),
			authHeader:     "Bearer the-token",
			wantStatus:     http.StatusOK,
			wantNextCalled: true,
			wantToken:      "the-token",
			wantHasResult:  true,
		},
		{
			name:           "audience array containing the resource matches",
			introspection:  fmt.Sprintf(`{"active":true,"aud":["https://other.example.com",%q],"exp":%d}`, testResource, future.Unix()),
			authHeader:     "Bearer the-token",
			wantStatus:     http.StatusOK,
			wantNextCalled: true,
			wantToken:      "the-token",
			wantHasResult:  true,
		},
		{
			name:           "inactive token passes through as an API key",
			introspection:  `{"active": false}`,
			authHeader:     "Bearer the-token",
			wantStatus:     http.StatusOK,
			wantNextCalled: true,
			wantToken:      "the-token",
			wantHasResult:  false,
		},
		{
			// Some clients send API keys without the Bearer scheme; pkg/authn
			// accepts that, so enabling OAuth must not break them.
			name:           "bare API key passes through",
			introspection:  `{"active": false}`,
			authHeader:     "rnd_some_api_key",
			wantStatus:     http.StatusOK,
			wantNextCalled: true,
			wantToken:      "rnd_some_api_key",
			wantHasResult:  false,
		},
		{
			// A dead OAuth token (expired/revoked) must be challenged even with
			// passthrough on, so the client gets the invalid_token signal to
			// refresh instead of an opaque downstream error.
			name:          "inactive OAuth token is challenged despite passthrough",
			introspection: `{"active": false, "render_token_kind": "oauth_access"}`,
			authHeader:    "Bearer the-token",
			wantStatus:    http.StatusUnauthorized,
			challengeHas:  []string{`error="invalid_token"`},
		},
		{
			// RFC 6750 §3.1: no error attribute when no credentials were sent.
			name:           "missing credentials are challenged without an error code",
			introspection:  `{"active": false}`,
			authHeader:     "",
			wantStatus:     http.StatusUnauthorized,
			challengeHas:   []string{`resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource/mcp"`},
			challengeLacks: []string{"error="},
		},
		{
			name:          "audience mismatch is rejected",
			introspection: activeBody("https://other.example.com/mcp"),
			authHeader:    "Bearer the-token",
			wantStatus:    http.StatusUnauthorized,
			challengeHas:  []string{`error="invalid_token"`, "audience"},
		},
		{
			name:          "missing audience is rejected",
			introspection: fmt.Sprintf(`{"active":true,"exp":%d}`, future.Unix()),
			authHeader:    "Bearer the-token",
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "expired active token is rejected",
			introspection: activeBodyWithExp(testResource, past),
			authHeader:    "Bearer the-token",
			wantStatus:    http.StatusUnauthorized,
			challengeHas:  []string{"expired"},
		},
		{
			name:           "bare API key rejected when passthrough disabled",
			passthroughOff: true,
			introspection:  `{"active": false}`,
			authHeader:     "rnd_some_api_key",
			wantStatus:     http.StatusUnauthorized,
		},
		{
			name:           "inactive token rejected when passthrough disabled",
			passthroughOff: true,
			introspection:  `{"active": false}`,
			authHeader:     "Bearer the-token",
			wantStatus:     http.StatusUnauthorized,
			challengeHas:   []string{`error="invalid_token"`},
		},
		{
			// 503, not 401: the client's token may be perfectly valid, so no
			// challenge is issued.
			name:           "introspection failure is 503 with no challenge",
			introspection:  "",
			authHeader:     "Bearer the-token",
			wantStatus:     http.StatusServiceUnavailable,
			wantRetryAfter: true,
			challengeLacks: []string{"Bearer"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.APIKeyPassthrough = !tc.passthroughOff

			rec, next := serveThrough(t, cfg, tc.introspection, func(r *http.Request) {
				if tc.authHeader == "" {
					r.Header.Del("Authorization")
				} else {
					r.Header.Set("Authorization", tc.authHeader)
				}
			})

			require.Equal(t, tc.wantStatus, rec.Code)
			require.Equal(t, tc.wantNextCalled, next.called)
			if tc.wantNextCalled {
				require.Equal(t, tc.wantToken, next.token)
				require.Equal(t, tc.wantHasResult, next.hasResult)
				if tc.wantSubject != "" {
					require.Equal(t, tc.wantSubject, next.introspection.Subject)
				}
			}
			challenge := rec.Header().Get("WWW-Authenticate")
			for _, want := range tc.challengeHas {
				require.Contains(t, challenge, want)
			}
			for _, absent := range tc.challengeLacks {
				require.NotContains(t, challenge, absent)
			}
			if tc.wantRetryAfter {
				require.NotEmpty(t, rec.Header().Get("Retry-After"))
			}
		})
	}
}
