package session

import (
	"context"
	"sync"

	"github.com/render-oss/render-mcp-server/pkg/config"
)

// inMemoryStore is a session store backed by an in-memory map, safe for concurrent access.
type inMemoryStore struct {
	mu       sync.Mutex
	sessions map[string]*InMemorySession
}

var _ Store = (*inMemoryStore)(nil)

func NewInMemoryStore() Store {
	return &inMemoryStore{
		sessions: make(map[string]*InMemorySession),
	}
}

func (i *inMemoryStore) Get(_ context.Context, sessionID string) (Session, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	session, ok := i.sessions[sessionID]
	if !ok {
		session = &InMemorySession{}
		i.sessions[sessionID] = session
	}
	return session, nil
}

type InMemorySession struct {
	mu                  sync.RWMutex
	selectedWorkspaceID string
}

var _ Session = (*InMemorySession)(nil)

func (h *InMemorySession) GetWorkspace(_ context.Context) (string, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.selectedWorkspaceID == "" {
		return "", config.ErrNoWorkspace
	}
	return h.selectedWorkspaceID, nil
}

func (h *InMemorySession) SetWorkspace(_ context.Context, s string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.selectedWorkspaceID = s
	return nil
}
