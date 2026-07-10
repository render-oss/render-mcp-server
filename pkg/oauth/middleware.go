package oauth

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"slices"
	"time"

	"github.com/render-oss/render-mcp-server/pkg/authn"
)

type ctxKey int

const introspectionResultKey ctxKey = 0

// IntrospectionFromContext returns the introspection result attached by
// Middleware for requests authenticated with an OAuth token. ok is false when
// the request did not carry one (API-key passthrough, or OAuth disabled).
func IntrospectionFromContext(ctx context.Context) (IntrospectionResponse, bool) {
	v, ok := ctx.Value(introspectionResultKey).(IntrospectionResponse)
	return v, ok
}

// Middleware returns an HTTP middleware that accepts either an OAuth bearer
// token (validated via introspection) or a Render API key (passed through
// unchanged — the Render API validates it on the downstream call, exactly as
// when OAuth is disabled). Both kinds coexist so existing API-key users keep
// working while OAuth clients are onboarded. Credentials are parsed with
// authn.BearerToken, so API keys sent without the Bearer scheme — accepted
// before OAuth existed — keep working too.
//
//   - No credentials → 401 with a WWW-Authenticate challenge pointing at the
//     protected-resource metadata (RFC 9728 §5.1) so clients can discover the
//     authorization server.
//   - Introspection unavailable → 503. We fail closed, but not with
//     invalid_token: that would make clients discard a valid token and rerun
//     their authorization flow during an outage.
//   - Active token whose audience isn't this resource server → 401
//     invalid_token (token replay across resource servers).
//   - Active token, audience match → request proceeds with the introspection
//     result attached to the context.
//   - Inactive → not a live OAuth token; passed through as an API key, or
//     rejected with invalid_token when cfg.APIKeyPassthrough is off.
//
// When cfg.Enabled is false the middleware is an identity function and the
// pre-OAuth behavior is preserved exactly.
func Middleware(cfg Config, introspector *Introspector) func(http.Handler) http.Handler {
	if !cfg.Enabled {
		return func(next http.Handler) http.Handler { return next }
	}
	// Canonicalized here (a no-op for FromEnv configs) so a hand-built Config
	// can't silently break audience matching; the challenge is fixed per
	// config, so build it once.
	canonicalResource := canonicalResourceURI(cfg.CanonicalResourceURI)
	challenge := fmt.Sprintf(`Bearer realm=%q, resource_metadata=%q`, canonicalResource, cfg.MetadataURL())

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := authn.BearerToken(r.Header.Get("Authorization"))
			if token == "" {
				writeChallenge(w, challenge, "", "")
				return
			}

			resp, err := introspector.Introspect(r.Context(), token)
			if err != nil {
				if r.Context().Err() != nil {
					return // client went away; nothing to answer or log
				}
				// Unconditional log: this is the only server-side signal that
				// 503s are an introspection outage, not bad tokens.
				log.Printf("oauth: introspection failed: %v", err)
				w.Header().Set("Retry-After", "5")
				http.Error(w, "token validation is temporarily unavailable", http.StatusServiceUnavailable)
				return
			}

			if !resp.Active {
				// A token the authorization server recognized as a dead OAuth
				// token is always challenged, even with passthrough on: passing
				// it through as an API key would surface an opaque downstream
				// error instead of the invalid_token signal a client needs to
				// refresh.
				if !cfg.APIKeyPassthrough || resp.RenderTokenKind == tokenKindOAuthAccess {
					writeChallenge(w, challenge, "invalid_token", "token is not active")
					return
				}
				// Otherwise this isn't a live OAuth token; pass it through as an
				// API key and let the Render API accept or reject it downstream.
				next.ServeHTTP(w, r.WithContext(authn.ContextWithAPIToken(r.Context(), token)))
				return
			}

			// Audience values are canonicalized at decode (see fetch), so
			// equality is the complete comparison.
			if !slices.Contains(resp.Audience, canonicalResource) {
				writeChallenge(w, challenge, "invalid_token", "token audience does not match this resource server")
				return
			}
			// Introspection caching never outlives exp, so this only fires on
			// clock skew or an authorization-server bug — defense in depth.
			if resp.Exp > 0 && !time.Now().Before(time.Unix(resp.Exp, 0)) {
				writeChallenge(w, challenge, "invalid_token", "token is expired")
				return
			}

			ctx := authn.ContextWithAPIToken(r.Context(), token)
			ctx = context.WithValue(ctx, introspectionResultKey, resp)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// writeChallenge writes a 401 with an RFC 6750 §3 WWW-Authenticate challenge.
// errorCode must be empty when the request carried no credentials at all —
// RFC 6750 §3.1 forbids an error attribute in that case.
func writeChallenge(w http.ResponseWriter, challenge, errorCode, description string) {
	if errorCode != "" {
		challenge += fmt.Sprintf(`, error=%q, error_description=%q`, errorCode, description)
	}
	w.Header().Set("WWW-Authenticate", challenge)
	w.WriteHeader(http.StatusUnauthorized)
}
