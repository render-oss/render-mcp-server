package session

import "context"

type Session interface {
	GetWorkspace() (string, error)
	SetWorkspace(string) error
}

func FromContext(ctx context.Context) Session {
	return ctx.Value(sessionCtxKey).(Session)
}
