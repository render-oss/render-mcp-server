package authn

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/render-oss/render-mcp-server/pkg/cfg"
)

const apiTokenKey string = "token"

var ErrNotAuthorized = errors.New("resource not found")

func APITokenFromContext(ctx context.Context) string {
	if token, ok := ctx.Value(apiTokenKey).(string); ok {
		return token
	}
	return ""
}

func ContextWithAPIToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, apiTokenKey, token)
}

func ContextWithAPITokenFromHeader(ctx context.Context, req *http.Request) context.Context {
	token := req.Header.Get("Authorization")

	if token == "" {
		return ctx
	}

	// Note: we strip the "Bearer " prefix if it exists
	// MCP Inspector attaches this prefix automatically, but it's unclear how standard this is
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}

	return ContextWithAPIToken(ctx, token)
}

func ContextWithAPITokenFromConfig(ctx context.Context) context.Context {
	token := cfg.GetAPIKey()
	if token == "" {
		log.Fatal("Error getting API token from config")
	}
	return ContextWithAPIToken(ctx, token)
}
