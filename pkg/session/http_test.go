package session_test

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/session"
)

func TestHTTPSession(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer s.Close()

	redisStore, err := session.NewRedisStore("redis://" + s.Addr())
	if err != nil {
		t.Fatalf("failed to initialize Redis session store: %v", err)
	}

	tests := []struct {
		name  string
		store session.Store
	}{
		{
			name:  "in-memory",
			store: session.NewInMemoryStore(),
		},
		{
			name:  "redis",
			store: redisStore,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contextWithHTTPSession := session.ContextWithHTTPSession(tt.store)
			{
				ctxOne := (&server.MCPServer{}).WithContext(context.Background(), fakeSession{sessionID: "one"})
				ctxOne = contextWithHTTPSession(ctxOne, nil)

				sessionOne := session.FromContext(ctxOne)

				_, err := sessionOne.GetWorkspace(ctxOne)
				if err == nil {
					t.Error("Expected error, got nil")
				}

				if err := sessionOne.SetWorkspace(ctxOne, "workspace-one"); err != nil {
					t.Errorf("Expected no error, got %v", err)
				}

				workspace, err := sessionOne.GetWorkspace(ctxOne)
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if workspace != "workspace-one" {
					t.Errorf("Expected workspace-one, got %s", workspace)
				}
			}

			{
				ctxOneAgain := (&server.MCPServer{}).WithContext(context.Background(), fakeSession{sessionID: "one"})
				ctxOneAgain = contextWithHTTPSession(ctxOneAgain, nil)
				sessionOneAgain := session.FromContext(ctxOneAgain)

				workspace, err := sessionOneAgain.GetWorkspace(ctxOneAgain)
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if workspace != "workspace-one" {
					t.Errorf("Expected workspace-one, got %s", workspace)
				}
			}

			{
				ctxTwo := (&server.MCPServer{}).WithContext(context.Background(), fakeSession{sessionID: "two"})
				ctxTwo = contextWithHTTPSession(ctxTwo, nil)

				sessionTwo := session.FromContext(ctxTwo)

				_, err := sessionTwo.GetWorkspace(ctxTwo)
				if err == nil {
					t.Error("Expected error, got nil")
				}

				if err := sessionTwo.SetWorkspace(ctxTwo, "workspace-two"); err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}

			{

				ctxOneFinal := (&server.MCPServer{}).WithContext(context.Background(), fakeSession{sessionID: "one"})
				ctxOneFinal = contextWithHTTPSession(ctxOneFinal, nil)
				sessionOneFinal := session.FromContext(ctxOneFinal)

				workspace, err := sessionOneFinal.GetWorkspace(ctxOneFinal)
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if workspace != "workspace-one" {
					t.Errorf("Expected workspace-one, got %s", workspace)
				}
			}
		})
	}
}

type fakeSession struct {
	sessionID           string
	notificationChannel chan mcp.JSONRPCNotification
	initialized         bool
}

func (f fakeSession) SessionID() string {
	return f.sessionID
}

func (f fakeSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return f.notificationChannel
}

func (f fakeSession) Initialize() {
}

func (f fakeSession) Initialized() bool {
	return f.initialized
}
