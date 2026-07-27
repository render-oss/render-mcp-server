package session

import (
	"context"
	"errors"
)

type Store interface {
	Get(ctx context.Context, sessionID string) (Session, error)
}

type Session interface {
	GetWorkspace(context.Context) (string, error)
	SetWorkspace(context.Context, string) error
}

func FromContext(ctx context.Context) Session {
	return ctx.Value(sessionCtxKey).(Session)
}

// ContextWithWorkspace returns a request-scoped context whose workspace is
// independent of the underlying MCP transport session. SetWorkspace continues
// to update the underlying session so existing selection behavior is preserved.
func ContextWithWorkspace(ctx context.Context, workspaceID string) context.Context {
	parent, _ := ctx.Value(sessionCtxKey).(Session)
	return context.WithValue(ctx, sessionCtxKey, &requestSession{
		Session:     parent,
		workspaceID: workspaceID,
	})
}

type requestSession struct {
	Session
	workspaceID string
}

func (r *requestSession) GetWorkspace(context.Context) (string, error) {
	return r.workspaceID, nil
}

func (r *requestSession) SetWorkspace(ctx context.Context, workspaceID string) error {
	if r.Session == nil {
		return errors.New("cannot persist workspace without a session")
	}
	return r.Session.SetWorkspace(ctx, workspaceID)
}
