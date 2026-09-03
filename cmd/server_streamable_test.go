package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/oauth"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mcpSessionHeader = "Mcp-Session-Id"

type streamLifecycle struct {
	handler http.Handler
	mu      sync.Mutex
	done    map[string]chan struct{}
}

func (s *streamLifecycle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := r.Header.Get("X-Test-Stream")
	if r.Method != http.MethodGet || name == "" {
		s.handler.ServeHTTP(w, r)
		return
	}
	s.mu.Lock()
	done := s.done[name]
	s.mu.Unlock()
	defer close(done)
	s.handler.ServeHTTP(w, r)
}

func (s *streamLifecycle) track(name string) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	done := make(chan struct{})
	s.done[name] = done
	return done
}

func newStreamableTestServer(t *testing.T, heartbeatInterval time.Duration) (*httptest.Server, *streamLifecycle) {
	t.Helper()
	mcpServer := server.NewMCPServer("render-mcp-test", "1.0.0")
	transport := newStreamableHTTPServer(mcpServer, session.NewInMemoryStore(), heartbeatInterval)
	lifecycle := &streamLifecycle{
		handler: newHTTPMux(transport, oauth.Config{}, ""),
		done:    make(map[string]chan struct{}),
	}
	return httptest.NewServer(lifecycle), lifecycle
}

func initializeStreamableSession(t *testing.T, endpoint string) string {
	t.Helper()
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"render-test","version":"1.0.0"}}}`)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer closeResponseBody(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	sessionID := resp.Header.Get(mcpSessionHeader)
	require.NotEmpty(t, sessionID)
	return sessionID
}

func closeResponseBody(t *testing.T, resp *http.Response) {
	t.Helper()
	require.NoError(t, resp.Body.Close())
}

func openStreamableGet(t *testing.T, endpoint, sessionID, name string) (*http.Response, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set(mcpSessionHeader, sessionID)
	req.Header.Set("X-Test-Stream", name)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp, cancel
}

func waitForStreamExit(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream handler did not exit")
	}
}

func TestStreamableHTTPHeartbeatAndReconnect(t *testing.T) {
	require.Equal(t, 20*time.Second, streamableHTTPHeartbeatInterval)
	ts, lifecycle := newStreamableTestServer(t, 5*time.Millisecond)
	defer ts.Close()
	sessionID := initializeStreamableSession(t, ts.URL+"/mcp")

	firstDone := lifecycle.track("first")
	first, cancelFirst := openStreamableGet(t, ts.URL+"/mcp", sessionID, "first")
	require.Equal(t, http.StatusOK, first.StatusCode)

	payload := make(chan []byte, 1)
	readErr := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(first.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				readErr <- err
				return
			}
			if data, ok := strings.CutPrefix(line, "data: "); ok {
				payload <- []byte(strings.TrimSpace(data))
				return
			}
		}
	}()

	var heartbeat []byte
	select {
	case heartbeat = <-payload:
	case err := <-readErr:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not arrive")
	}
	var ping struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
	}
	require.NoError(t, json.Unmarshal(heartbeat, &ping))
	assert.Equal(t, "2.0", ping.JSONRPC)
	assert.Equal(t, "ping", ping.Method)

	cancelFirst()
	closeResponseBody(t, first)
	waitForStreamExit(t, firstDone)

	reconnectDone := lifecycle.track("reconnect")
	reconnected, cancelReconnect := openStreamableGet(t, ts.URL+"/mcp", sessionID, "reconnect")
	require.Equal(t, http.StatusOK, reconnected.StatusCode)
	cancelReconnect()
	closeResponseBody(t, reconnected)
	waitForStreamExit(t, reconnectDone)
}

func TestStreamableHTTPSessionCleanupAfterMCPGo970(t *testing.T) {
	t.Skip("blocked on https://github.com/mark3labs/mcp-go/pull/970")

	ts, lifecycle := newStreamableTestServer(t, 5*time.Millisecond)
	defer ts.Close()
	sessionID := initializeStreamableSession(t, ts.URL+"/mcp")

	streamDone := lifecycle.track("active")
	active, cancelActive := openStreamableGet(t, ts.URL+"/mcp", sessionID, "active")
	require.Equal(t, http.StatusOK, active.StatusCode)
	defer cancelActive()
	defer closeResponseBody(t, active)

	duplicateDone := lifecycle.track("duplicate")
	duplicate, cancelDuplicate := openStreamableGet(t, ts.URL+"/mcp", sessionID, "duplicate")
	cancelDuplicate()
	closeResponseBody(t, duplicate)
	waitForStreamExit(t, duplicateDone)
	require.Equal(t, http.StatusConflict, duplicate.StatusCode)

	deleteReq, err := http.NewRequest(http.MethodDelete, ts.URL+"/mcp", nil)
	require.NoError(t, err)
	deleteReq.Header.Set("Authorization", "Bearer test-api-key")
	deleteReq.Header.Set(mcpSessionHeader, sessionID)
	deleted, err := http.DefaultClient.Do(deleteReq)
	require.NoError(t, err)
	closeResponseBody(t, deleted)
	require.Equal(t, http.StatusOK, deleted.StatusCode)
	waitForStreamExit(t, streamDone)

	staleReq, err := http.NewRequest(http.MethodGet, ts.URL+"/mcp", nil)
	require.NoError(t, err)
	staleReq.Header.Set("Authorization", "Bearer test-api-key")
	staleReq.Header.Set("Accept", "text/event-stream")
	staleReq.Header.Set(mcpSessionHeader, sessionID)
	stale, err := http.DefaultClient.Do(staleReq)
	require.NoError(t, err)
	defer closeResponseBody(t, stale)
	assert.Equal(t, http.StatusNotFound, stale.StatusCode)
}
