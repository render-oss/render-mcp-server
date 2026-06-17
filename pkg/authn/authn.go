package authn

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/render-oss/render-mcp-server/pkg/cfg"
	"github.com/render-oss/render-mcp-server/pkg/logging"
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

// BearerToken extracts the credential from an Authorization header value: the
// value with its "Bearer " prefix removed (case-insensitively, per RFC 7235)
// when present, otherwise the value unchanged — some MCP clients send bare
// tokens without the scheme. Every parser of the Authorization header must go
// through this so they can't disagree about what the credential is.
func BearerToken(headerValue string) string {
	if len(headerValue) > 7 && strings.EqualFold(headerValue[:7], "Bearer ") {
		return headerValue[7:]
	}
	return headerValue
}

func ContextWithAPITokenFromHeader(ctx context.Context, req *http.Request) context.Context {
	token := req.Header.Get("Authorization")

	if token == "" {
		logging.Error("auth: no Authorization header on %s %s", req.Method, req.URL.Path)
		return ctx
	}

	return ContextWithAPIToken(ctx, BearerToken(token))
}

func ContextWithAPITokenFromConfig(ctx context.Context) context.Context {
	token := cfg.GetAPIKey()
	if token == "" {
		log.Fatal("Error getting API token from config")
	}
	return ContextWithAPIToken(ctx, token)
}
