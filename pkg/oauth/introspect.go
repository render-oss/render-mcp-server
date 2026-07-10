package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/render-oss/render-mcp-server/pkg/cfg"
)

const (
	// DefaultIntrospectionCacheTTL is the recommended cacheTTL for
	// NewIntrospector. It is the revocation-propagation bound: a revoked
	// token keeps working for at most this long on a warm cache.
	DefaultIntrospectionCacheTTL = 60 * time.Second

	// maxCacheEntries bounds the introspection cache so unauthenticated
	// garbage tokens can't grow it without limit.
	maxCacheEntries = 16384

	// maxResponseBytes bounds how much of an introspection response body is
	// read. Real responses are a few hundred bytes.
	maxResponseBytes = 1 << 20
)

// tokenKindOAuthAccess is the render_token_kind value the Render authorization
// server sets on an inactive introspection response when the presented token
// was a known (expired or revoked) OAuth access token, as opposed to an API
// key. It lets us challenge a dead OAuth token instead of passing it through.
const tokenKindOAuthAccess = "oauth_access"

// IntrospectionResponse is the RFC 7662 introspection payload returned by the
// authorization server. Field names match the on-the-wire JSON.
type IntrospectionResponse struct {
	Active   bool     `json:"active"`
	Subject  string   `json:"sub"`
	ClientID string   `json:"client_id"`
	Audience Audience `json:"aud"`
	Exp      int64    `json:"exp"`
	// RenderTokenKind is a Render extension; see tokenKindOAuthAccess.
	RenderTokenKind string `json:"render_token_kind"`
}

// Audience is the introspected "aud" claim, which may be a single string or
// an array of strings on the wire (RFC 7662 §2.2, RFC 7519 §4.1.3).
type Audience []string

func (a *Audience) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*a = Audience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return fmt.Errorf("aud must be a string or array of strings: %w", err)
	}
	*a = many
	return nil
}

// Introspector validates bearer tokens against the authorization server's RFC
// 7662 endpoint, caching results in-process keyed by sha256(token). Active
// results are cached for min(cacheTTL, token expiry); inactive ones for
// cacheTTL, so non-OAuth bearers don't re-hit the endpoint every request.
// Concurrent lookups of the same token collapse into one upstream call.
type Introspector struct {
	introspectURL string
	serviceToken  string
	httpClient    *http.Client
	now           func() time.Time
	cacheTTL      time.Duration
	cache         *tokenCache

	mu       sync.Mutex
	inflight map[string]*inflightCall
}

type inflightCall struct {
	done chan struct{}
	resp IntrospectionResponse
	err  error
}

// NewIntrospector constructs an Introspector against the given authorization
// server base URL. cacheTTL bounds how long any introspection result is
// reused.
func NewIntrospector(authServerURL, serviceToken string, cacheTTL time.Duration) *Introspector {
	return &Introspector{
		introspectURL: strings.TrimRight(authServerURL, "/") + "/v1/oauth/introspect",
		serviceToken:  serviceToken,
		httpClient:    &http.Client{Timeout: 5 * time.Second},
		now:           time.Now,
		cacheTTL:      cacheTTL,
		cache:         newTokenCache(maxCacheEntries),
		inflight:      map[string]*inflightCall{},
	}
}

// Introspect returns the introspection result for token. Active=false is a
// valid result, not an error (RFC 7662 doesn't say why a token is inactive);
// an error means the result couldn't be obtained and the caller must fail
// closed. The result is not audience-checked — callers must verify aud
// themselves, as Middleware does.
func (i *Introspector) Introspect(ctx context.Context, token string) (IntrospectionResponse, error) {
	key := hashToken(token)
	if resp, ok := i.cache.get(key, i.now()); ok {
		return resp, nil
	}

	i.mu.Lock()
	// Re-check under the lock: a leader that finished between our cache miss
	// and here has already cached its result and left the inflight map.
	if resp, ok := i.cache.get(key, i.now()); ok {
		i.mu.Unlock()
		return resp, nil
	}
	if call, ok := i.inflight[key]; ok {
		i.mu.Unlock()
		select {
		case <-call.done:
			return call.resp, call.err
		case <-ctx.Done():
			return IntrospectionResponse{}, ctx.Err()
		}
	}
	call := &inflightCall{done: make(chan struct{})}
	i.inflight[key] = call
	i.mu.Unlock()

	// Detached from the caller's cancellation: the result is shared with any
	// concurrent waiters, so the first caller hanging up must not fail the
	// others. The HTTP client timeout still bounds the call.
	call.resp, call.err = i.fetch(context.WithoutCancel(ctx), token)
	if call.err == nil {
		i.cacheResult(call.resp, key)
	}

	i.mu.Lock()
	delete(i.inflight, key)
	i.mu.Unlock()
	close(call.done)

	return call.resp, call.err
}

func (i *Introspector) fetch(ctx context.Context, token string) (IntrospectionResponse, error) {
	form := url.Values{"token": {token}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, i.introspectURL, strings.NewReader(form))
	if err != nil {
		return IntrospectionResponse{}, fmt.Errorf("build introspection request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cfg.AddUserAgent(req.Header, "")
	if i.serviceToken != "" {
		req.Header.Set("Authorization", "Bearer "+i.serviceToken)
	}

	resp, err := i.httpClient.Do(req)
	if err != nil {
		return IntrospectionResponse{}, fmt.Errorf("introspection request: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		// Our own credentials were refused — distinct from "token inactive",
		// which is a 200 with active=false.
		if i.serviceToken == "" {
			return IntrospectionResponse{}, fmt.Errorf("introspection endpoint requires authentication and no service token is configured")
		}
		return IntrospectionResponse{}, fmt.Errorf("introspection endpoint rejected the service token")
	case resp.StatusCode != http.StatusOK:
		return IntrospectionResponse{}, fmt.Errorf("introspection endpoint returned status %d", resp.StatusCode)
	}

	var out IntrospectionResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&out); err != nil {
		return IntrospectionResponse{}, fmt.Errorf("decode introspection response: %w", err)
	}
	// Canonicalize audience values once here so the per-request audience
	// check (and any cached reuse of it) is plain string equality.
	for k, a := range out.Audience {
		out.Audience[k] = canonicalResourceURI(a)
	}
	return out, nil
}

func (i *Introspector) cacheResult(resp IntrospectionResponse, key string) {
	now := i.now()
	expiresAt := now.Add(i.cacheTTL)
	if resp.Active && resp.Exp > 0 {
		exp := time.Unix(resp.Exp, 0)
		if !exp.After(now) {
			return // active but already expired by its own claim; don't cache
		}
		if exp.Before(expiresAt) {
			expiresAt = exp
		}
	}
	i.cache.put(key, resp, expiresAt, now)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
