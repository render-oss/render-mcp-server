package logging

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHTTPAccessLogMiddlewareDelegatesFlusher(t *testing.T) {
	ctx := ContextWithLogger(t.Context(), New(io.Discard, slog.LevelDebug))

	handler := HTTPAccessLogMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f, ok := w.(http.Flusher)
		require.True(t, ok, "wrapped ResponseWriter must implement http.Flusher")
		f.Flush()
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	handler.ServeHTTP(rec, req)
	require.True(t, rec.Flushed)
}
