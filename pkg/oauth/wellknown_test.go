package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHandleProtectedResourceMetadata(t *testing.T) {
	cases := []struct {
		name      string
		cfg       Config
		method    string
		want      int
		wantAllow string
	}{
		{"disabled serves 404", Config{}, http.MethodGet, http.StatusNotFound, ""},
		{"GET is served", testConfig(), http.MethodGet, http.StatusOK, ""},
		{"HEAD is allowed", testConfig(), http.MethodHead, http.StatusOK, ""},
		{"POST is rejected", testConfig(), http.MethodPost, http.StatusMethodNotAllowed, "GET, HEAD"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			HandleProtectedResourceMetadata(tc.cfg).ServeHTTP(rec,
				httptest.NewRequest(tc.method, "/.well-known/oauth-protected-resource/mcp", nil))

			require.Equal(t, tc.want, rec.Code)
			if tc.wantAllow != "" {
				require.Equal(t, tc.wantAllow, rec.Header().Get("Allow"))
			}
		})
	}
}

func TestHandleProtectedResourceMetadata_DocumentShape(t *testing.T) {
	rec := httptest.NewRecorder()
	HandleProtectedResourceMetadata(testConfig()).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.NotEmpty(t, rec.Header().Get("Cache-Control"))

	var doc ProtectedResourceMetadata
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	require.Equal(t, testResource, doc.Resource)
	require.Equal(t, []string{"https://api.example.com"}, doc.AuthorizationServers)
	require.Equal(t, []string{"header"}, doc.BearerMethodsSupported)

	// No scope vocabulary is advertised until the authorization server has one.
	require.NotContains(t, rec.Body.String(), "scopes_supported")
}
