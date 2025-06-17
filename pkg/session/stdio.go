package session

import (
	"context"

	"github.com/render-oss/render-mcp-server/pkg/config"
)

func ContextWithStdioSession(ctx context.Context) context.Context {
	return context.WithValue(ctx, sessionCtxKey, &StdioSession{})
}

type StdioSession struct{}

var _ Session = (*StdioSession)(nil)

func (h *StdioSession) GetWorkspace() (string, error) {
	return config.WorkspaceID()
}

func (h *StdioSession) SetWorkspace(s string) error {
	return config.SelectWorkspace(s)
}
