package session_test

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/session"
)

func TestHTTPSession(t *testing.T) {
	sessionStore := session.NewInMemoryStore()
	contextWithHTTPSession := session.ContextWithHTTPSession(sessionStore)
	{
		ctxOne := (&server.MCPServer{}).WithContext(context.Background(), fakeSession{sessionID: "one"})
		ctxOne = contextWithHTTPSession(ctxOne, nil)

		sessionOne := session.FromContext(ctxOne)

		_, err := sessionOne.GetWorkspace()
		if err == nil {
			t.Error("Expected error, got nil")
		}

		if err := sessionOne.SetWorkspace("workspace-one"); err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		workspace, err := sessionOne.GetWorkspace()
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

		workspace, err := sessionOneAgain.GetWorkspace()
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

		_, err := sessionTwo.GetWorkspace()
		if err == nil {
			t.Error("Expected error, got nil")
		}

		if err := sessionTwo.SetWorkspace("workspace-two"); err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
	}

	{

		ctxOneFinal := (&server.MCPServer{}).WithContext(context.Background(), fakeSession{sessionID: "one"})
		ctxOneFinal = contextWithHTTPSession(ctxOneFinal, nil)
		sessionOneFinal := session.FromContext(ctxOneFinal)

		workspace, err := sessionOneFinal.GetWorkspace()
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if workspace != "workspace-one" {
			t.Errorf("Expected workspace-one, got %s", workspace)
		}
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
