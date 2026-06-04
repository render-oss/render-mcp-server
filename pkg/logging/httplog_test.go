package logging

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHTTPMiddlewareDelegatesFlusher(t *testing.T) {
	t.Setenv("LOGGING", "1")

	handler := HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f, ok := w.(http.Flusher)
		require.True(t, ok, "wrapped ResponseWriter must implement http.Flusher")
		f.Flush()
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp", nil))
	require.True(t, rec.Flushed)
}
