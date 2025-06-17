package session

import (
	"context"

	"github.com/render-oss/render-mcp-server/pkg/config"
)

type inMemoryStore struct {
	sessions map[string]*InMemorySession
}

var _ Store = (*inMemoryStore)(nil)

func NewInMemoryStore() Store {
	return &inMemoryStore{
		sessions: make(map[string]*InMemorySession),
	}
}

func (i *inMemoryStore) Get(_ context.Context, sessionID string) (Session, error) {
	if _, ok := i.sessions[sessionID]; !ok {
		i.sessions[sessionID] = &InMemorySession{}
	}
	return i.sessions[sessionID], nil
}

type InMemorySession struct {
	selectedWorkspaceID string
}

var _ Session = (*InMemorySession)(nil)

func (h *InMemorySession) GetWorkspace(_ context.Context) (string, error) {
	if h.selectedWorkspaceID == "" {
		return "", config.ErrNoWorkspace
	}
	return h.selectedWorkspaceID, nil
}

func (h *InMemorySession) SetWorkspace(_ context.Context, s string) error {
	h.selectedWorkspaceID = s
	return nil
}
