package httpcontext

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/render-oss/render-mcp-server/pkg/logging"
)

// HTTPContext stores HTTP metadata extracted from incoming requests.
type HTTPContext struct {
	RequestID    string // Opaque Rndr-Id header; empty when absent
	UserAgent    string // Client's User-Agent header
	ForwardedFor string // X-Forwarded-For chain
}

type ctxKey struct{}

// FromContext retrieves HTTPContext from context. Returns empty HTTPContext if not present.
func FromContext(ctx context.Context) HTTPContext {
	if hc, ok := ctx.Value(ctxKey{}).(HTTPContext); ok {
		return hc
	}
	return HTTPContext{}
}

// ContextWithHTTPContext stores HTTPContext in the context.
func ContextWithHTTPContext(ctx context.Context, hc HTTPContext) context.Context {
	return context.WithValue(ctx, ctxKey{}, hc)
}

// Middleware attaches HTTP metadata to the request context.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := ContextWithHTTPRequest(r.Context(), r)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ContextWithHTTPRequest extracts HTTP metadata from a request and stores it in context.
func ContextWithHTTPRequest(ctx context.Context, req *http.Request) context.Context {
	hc := HTTPContext{
		RequestID:    req.Header.Get("Rndr-Id"),
		UserAgent:    req.Header.Get("User-Agent"),
		ForwardedFor: buildXFF(ctx, req.Header.Get("X-Forwarded-For"), req.RemoteAddr),
	}
	return ContextWithHTTPContext(ctx, hc)
}

// buildXFF constructs the X-Forwarded-For chain.
// It appends the client IP from RemoteAddr to the existing XFF header,
// avoiding consecutive duplicates at the end.
func buildXFF(ctx context.Context, existingXFF, remoteAddr string) string {
	clientIP := getClientIP(ctx, remoteAddr)
	if clientIP == "" {
		return existingXFF
	}

	if existingXFF == "" {
		return clientIP
	}

	// Check if clientIP is already the last entry to avoid consecutive duplicates
	lastEntry := lastXFFEntry(existingXFF)
	if lastEntry == clientIP {
		return existingXFF
	}

	return existingXFF + ", " + clientIP
}

// getClientIP extracts the IP address from RemoteAddr, stripping the port if present.
func getClientIP(ctx context.Context, remoteAddr string) string {
	if remoteAddr == "" {
		return ""
	}

	// net.SplitHostPort handles both IPv4 and IPv6 addresses
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// RemoteAddr doesn't have standard host:port format.
		// This can happen with non-TCP transports or unusual proxy configurations.
		// Log for debugging but use the raw value.
		logging.From(ctx).WithError(err).Debug("httpcontext: could not parse RemoteAddr",
			logging.OneOffField("remote", remoteAddr),
		)
		return remoteAddr
	}
	return host
}

// lastXFFEntry returns the last IP in the X-Forwarded-For chain.
func lastXFFEntry(xff string) string {
	parts := strings.Split(xff, ",")
	// strings.Split always returns at least one element, even for empty string
	return strings.TrimSpace(parts[len(parts)-1])
}
