// Package oauth implements the OAuth 2.1 resource-server pieces of the Render
// MCP server: RFC 9728 protected-resource metadata, RFC 6750 bearer-token
// middleware, and a cached RFC 7662 token-introspection client.
//
// The MCP server has no direct access to token storage — it validates bearer
// tokens by calling the authorization server's introspection endpoint.
package oauth

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

const wellKnownPath = "/.well-known/oauth-protected-resource"

// Config is the OAuth resource-server configuration, loaded from environment
// variables by FromEnv.
type Config struct {
	// Enabled gates the OAuth middleware. When false the server keeps its
	// pre-OAuth behavior — bearer tokens pass through to the Render API
	// unchanged — so enabling OAuth is opt-in per deployment.
	Enabled bool

	// AuthorizationServerURL is the base URL of the authorization server. It
	// is advertised in the protected-resource metadata and used to build the
	// introspection endpoint URL.
	AuthorizationServerURL string

	// CanonicalResourceURI identifies this resource server. Tokens are
	// accepted only when their introspected audience matches it. Held in
	// canonical form (see canonicalResourceURI).
	CanonicalResourceURI string

	// IntrospectionServiceToken optionally authenticates this server to the
	// introspection endpoint.
	IntrospectionServiceToken string

	// APIKeyPassthrough decides what happens to tokens the authorization
	// server reports inactive (e.g. Render API keys, which aren't OAuth
	// tokens). True (default) forwards them to the Render API so API-key users
	// keep working; false rejects them (strict OAuth-only mode).
	APIKeyPassthrough bool
}

// FromEnv builds a Config from OAUTH_* environment variables. When
// OAUTH_ENABLED is unset (or not "true"/"1"/"yes") it returns a disabled
// Config and ignores every other variable; when set, the URL variables are
// required and validated so a misconfigured deployment fails at startup
// instead of rejecting every request.
func FromEnv() (Config, error) {
	enabled, err := boolEnv("OAUTH_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	if !enabled {
		return Config{}, nil
	}
	authServer, err := requiredURLEnv("OAUTH_AUTHORIZATION_SERVER_URL")
	if err != nil {
		return Config{}, err
	}
	resource, err := requiredURLEnv("OAUTH_CANONICAL_RESOURCE_URI")
	if err != nil {
		return Config{}, err
	}
	passthrough, err := boolEnv("OAUTH_API_KEY_PASSTHROUGH", true)
	if err != nil {
		return Config{}, err
	}
	return Config{
		Enabled:                   true,
		AuthorizationServerURL:    strings.TrimRight(authServer, "/"),
		CanonicalResourceURI:      canonicalResourceURI(resource),
		IntrospectionServiceToken: os.Getenv("OAUTH_INTROSPECTION_SERVICE_TOKEN"),
		APIKeyPassthrough:         passthrough,
	}, nil
}

// MetadataPaths returns the request paths that serve the protected-resource
// metadata document: the RFC 9728 §3.1 path-insertion form derived from the
// resource URI (resource https://host/mcp → /.well-known/oauth-protected-resource/mcp)
// plus the root form for clients that probe it directly.
func (c Config) MetadataPaths() []string {
	if p := metadataPath(c.CanonicalResourceURI); p != wellKnownPath {
		return []string{p, wellKnownPath}
	}
	return []string{wellKnownPath}
}

// MetadataURL returns the absolute URL of the protected-resource metadata
// document, sent in WWW-Authenticate challenges so clients can discover the
// authorization server.
func (c Config) MetadataURL() string {
	u, err := url.Parse(c.CanonicalResourceURI)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimRight(c.CanonicalResourceURI, "/") + wellKnownPath
	}
	return u.Scheme + "://" + u.Host + metadataPath(c.CanonicalResourceURI)
}

// metadataPath is the RFC 9728 §3.1 well-known path for a resource URI: the
// well-known segment inserted between the host and the resource's path.
func metadataPath(canonicalResource string) string {
	u, err := url.Parse(canonicalResource)
	if err != nil || u.EscapedPath() == "" || u.EscapedPath() == "/" {
		return wellKnownPath
	}
	return wellKnownPath + u.EscapedPath()
}

// canonicalResourceURI normalizes an RFC 8707 resource indicator (RFC 3986 §6)
// so equivalent spellings compare equal: scheme/host lowercased, default ports
// dropped, bare "/" path treated as none; path, query, and fragment are
// otherwise preserved. It mirrors the authorization server's audience
// normalization so matching can't be defeated by spelling on either side.
// Unparseable values are returned unchanged (they fail matching anyway).
func canonicalResourceURI(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		// Hostname() strips the brackets from IPv6 literals ("[::1]" → "::1"),
		// so re-bracket before assigning back to Host.
		host := u.Hostname()
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		u.Host = host
	}
	if u.Path == "/" {
		u.Path = ""
	}
	return u.String()
}

func requiredURLEnv(name string) (string, error) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return "", fmt.Errorf("%s is required when OAUTH_ENABLED is set", name)
	}
	u, err := url.Parse(v)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%s must be an absolute http(s) URL, got %q", name, v)
	}
	// A query would corrupt URLs derived by concatenation (the introspection
	// endpoint, the RFC 9728 path-insertion metadata URL), and RFC 8707
	// forbids fragments in resource indicators. Neither identifies anything
	// here; reject rather than carry them inconsistently. Checked on the raw
	// string so a bare trailing "?" or "#" (empty query/fragment, which
	// url.Parse reports as no query/fragment) can't slip through.
	if strings.ContainsAny(v, "?#") {
		return "", fmt.Errorf("%s must not contain a query or fragment, got %q", name, v)
	}
	// These values are embedded in the WWW-Authenticate header, whose
	// quoted-string grammar can't carry non-ASCII; require the encoded form
	// (punycode hosts, percent-encoded paths) up front.
	for j := 0; j < len(v); j++ {
		if v[j] <= 0x20 || v[j] > 0x7e {
			return "", fmt.Errorf("%s must be printable ASCII (punycode / percent-encoded), got %q", name, v)
		}
	}
	return v, nil
}

// boolEnv parses a boolean environment variable, returning def when unset.
// Unrecognized values are an error rather than silently false — for these
// flags a typo must not flip a security posture unnoticed.
func boolEnv(name string, def bool) (bool, error) {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch v {
	case "":
		return def, nil
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true or false (also accepted: 1/0, yes/no), got %q", name, os.Getenv(name))
	}
}
