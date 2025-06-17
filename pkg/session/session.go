package session

import "context"

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
