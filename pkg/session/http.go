package session

import (
	"context"
	"net/http"

	"github.com/mark3labs/mcp-go/server"
)

type sessionCtxKeyType struct{}

var sessionCtxKey sessionCtxKeyType

func ContextWithHTTPSession(store Store) func(ctx context.Context, _ *http.Request) context.Context {
	return func(ctx context.Context, _ *http.Request) context.Context {
		session, err := store.Get(ctx, server.ClientSessionFromContext(ctx).SessionID())
		if err != nil {
			return ctx
		}
		return context.WithValue(ctx, sessionCtxKey, session)
	}
}
