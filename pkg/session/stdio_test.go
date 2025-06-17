package session_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/session"
)

func TestStdioSession(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("RENDER_CONFIG_PATH", filepath.Join(tempDir, "mcp-server.yaml"))

	ctxOne := (&server.MCPServer{}).WithContext(context.Background(), fakeSession{sessionID: "one"})
	ctxOne = session.ContextWithStdioSession(ctxOne)

	sessionOne := session.FromContext(ctxOne)

	_, err := sessionOne.GetWorkspace()
	if err == nil {
		t.Error("Expected error, got nil")
	}

	if err := sessionOne.SetWorkspace("workspace-one"); err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	ctxOneAgain := (&server.MCPServer{}).WithContext(context.Background(), fakeSession{sessionID: "one"})
	ctxOneAgain = session.ContextWithStdioSession(ctxOneAgain)
	sessionOneAgain := session.FromContext(ctxOneAgain)

	workspace, err := sessionOneAgain.GetWorkspace()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if workspace != "workspace-one" {
		t.Errorf("Expected workspace-one, got %s", workspace)
	}

	if err := sessionOneAgain.SetWorkspace("workspace-two"); err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	workspace, err = sessionOne.GetWorkspace()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if workspace != "workspace-two" {
		t.Errorf("Expected workspace-two, got %s", workspace)
	}
}
