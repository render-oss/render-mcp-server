package multicontext

import (
	"context"
	"net/http"

	"github.com/mark3labs/mcp-go/server"
)

func MultiHTTPContextFunc(fns ...server.HTTPContextFunc) server.HTTPContextFunc {
	return func(ctx context.Context, r *http.Request) context.Context {
		for _, fn := range fns {
			ctx = fn(ctx, r)
		}
		return ctx
	}
}

func MultiStdioContextFunc(fns ...server.StdioContextFunc) server.StdioContextFunc {
	return func(ctx context.Context) context.Context {
		for _, fn := range fns {
			ctx = fn(ctx)
		}
		return ctx
	}
}
