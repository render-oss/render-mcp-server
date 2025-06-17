package session

import (
	"context"
	"net/http"

	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/config"
)

type sessionCtxKeyType struct{}

var sessionCtxKey sessionCtxKeyType

func ContextWithHTTPSession(ctx context.Context, _ *http.Request) context.Context {
	cs := server.ClientSessionFromContext(ctx)
	if _, ok := inMemoryDataSingleton[cs.SessionID()]; !ok {
		inMemoryDataSingleton[cs.SessionID()] = &HTTPSession{}
	}
	return context.WithValue(ctx, sessionCtxKey, inMemoryDataSingleton[cs.SessionID()])
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

var inMemoryDataSingleton = map[string]*HTTPSession{}
