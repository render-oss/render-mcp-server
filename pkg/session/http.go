package session

import (
	"context"
	"net/http"

	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/config"
)

type sessionCtxKeyType struct{}

var sessionCtxKey sessionCtxKeyType

func ContextWithHTTPSession(store Store) func(ctx context.Context, _ *http.Request) context.Context {
	return func(ctx context.Context, _ *http.Request) context.Context {
		return context.WithValue(ctx, sessionCtxKey, store.Get(server.ClientSessionFromContext(ctx).SessionID()))
	}
}

type HTTPSession struct {
	selectedWorkspaceID string
}

var _ Session = (*HTTPSession)(nil)

func (h *HTTPSession) GetWorkspace() (string, error) {
	if h.selectedWorkspaceID == "" {
		return "", config.ErrNoWorkspace
	}
	return h.selectedWorkspaceID, nil
}

func (h *HTTPSession) SetWorkspace(s string) error {
	h.selectedWorkspaceID = s
	return nil
}

type Store interface {
	Get(sessionID string) *HTTPSession
}

type inMemoryStore struct {
	sessions map[string]*HTTPSession
}

var _ Store = (*inMemoryStore)(nil)

func NewInMemoryStore() Store {
	return &inMemoryStore{
		sessions: make(map[string]*HTTPSession),
	}
}

func (i *inMemoryStore) Get(sessionID string) *HTTPSession {
	if _, ok := i.sessions[sessionID]; !ok {
		i.sessions[sessionID] = &HTTPSession{}
	}
	return i.sessions[sessionID]
}
