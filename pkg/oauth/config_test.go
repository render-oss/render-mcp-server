package oauth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func setEnabledEnv(t *testing.T) {
	t.Setenv("OAUTH_ENABLED", "true")
	t.Setenv("OAUTH_AUTHORIZATION_SERVER_URL", "https://api.example.com")
	t.Setenv("OAUTH_CANONICAL_RESOURCE_URI", "https://mcp.example.com/mcp")
	t.Setenv("OAUTH_INTROSPECTION_SERVICE_TOKEN", "svc-token")
}

func TestFromEnv(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		t.Setenv("OAUTH_ENABLED", "")

		cfg, err := FromEnv()

		require.NoError(t, err)
		require.False(t, cfg.Enabled)
	})

	t.Run("disabled ignores missing URLs", func(t *testing.T) {
		t.Setenv("OAUTH_ENABLED", "false")
		t.Setenv("OAUTH_AUTHORIZATION_SERVER_URL", "")
		t.Setenv("OAUTH_CANONICAL_RESOURCE_URI", "")

		cfg, err := FromEnv()

		require.NoError(t, err)
		require.False(t, cfg.Enabled)
	})

	t.Run("enabled with valid config", func(t *testing.T) {
		setEnabledEnv(t)

		cfg, err := FromEnv()

		require.NoError(t, err)
		require.True(t, cfg.Enabled)
		require.Equal(t, "https://api.example.com", cfg.AuthorizationServerURL)
		require.Equal(t, "https://mcp.example.com/mcp", cfg.CanonicalResourceURI)
		require.Equal(t, "svc-token", cfg.IntrospectionServiceToken)
		require.True(t, cfg.APIKeyPassthrough, "passthrough must default on for the rollout window")
		require.Equal(t,
			"https://mcp.example.com/.well-known/oauth-protected-resource/mcp",
			cfg.MetadataURL())
	})

	t.Run("API-key passthrough can be disabled", func(t *testing.T) {
		setEnabledEnv(t)
		t.Setenv("OAUTH_API_KEY_PASSTHROUGH", "false")

		cfg, err := FromEnv()

		require.NoError(t, err)
		require.False(t, cfg.APIKeyPassthrough)
	})

	t.Run("trims trailing slash from authorization server URL", func(t *testing.T) {
		setEnabledEnv(t)
		t.Setenv("OAUTH_AUTHORIZATION_SERVER_URL", "https://api.example.com/")

		cfg, err := FromEnv()

		require.NoError(t, err)
		require.Equal(t, "https://api.example.com", cfg.AuthorizationServerURL)
	})

	t.Run("canonicalizes the resource URI", func(t *testing.T) {
		setEnabledEnv(t)
		t.Setenv("OAUTH_CANONICAL_RESOURCE_URI", "HTTPS://MCP.Example.com:443/mcp")

		cfg, err := FromEnv()

		require.NoError(t, err)
		require.Equal(t, "https://mcp.example.com/mcp", cfg.CanonicalResourceURI)
	})
}

// TestFromEnv_Errors covers the validation failures, which all share the same
// shape: start from a valid enabled config, break one variable, expect a
// startup error. env overrides the baseline set by setEnabledEnv.
func TestFromEnv_Errors(t *testing.T) {
	cases := map[string]struct {
		env     map[string]string
		wantErr string
	}{
		"missing authorization server URL": {
			env:     map[string]string{"OAUTH_AUTHORIZATION_SERVER_URL": ""},
			wantErr: "OAUTH_AUTHORIZATION_SERVER_URL is required",
		},
		"missing resource URI": {
			env:     map[string]string{"OAUTH_CANONICAL_RESOURCE_URI": ""},
			wantErr: "OAUTH_CANONICAL_RESOURCE_URI is required",
		},
		"relative URL": {
			env:     map[string]string{"OAUTH_CANONICAL_RESOURCE_URI": "mcp.example.com/mcp"},
			wantErr: "must be an absolute http(s) URL",
		},
		"non-http scheme": {
			env:     map[string]string{"OAUTH_AUTHORIZATION_SERVER_URL": "ftp://api.example.com"},
			wantErr: "must be an absolute http(s) URL",
		},
		"query component": {
			env:     map[string]string{"OAUTH_CANONICAL_RESOURCE_URI": "https://mcp.example.com/mcp?env=prod"},
			wantErr: "must not contain a query or fragment",
		},
		// url.Parse reports a bare "?" as no query (ForceQuery), but the raw
		// character still corrupts the concatenated introspection URL.
		"bare trailing question mark": {
			env:     map[string]string{"OAUTH_AUTHORIZATION_SERVER_URL": "https://api.example.com?"},
			wantErr: "must not contain a query or fragment",
		},
		"fragment component": {
			env:     map[string]string{"OAUTH_AUTHORIZATION_SERVER_URL": "https://api.example.com#prod"},
			wantErr: "must not contain a query or fragment",
		},
		// Embedded in the WWW-Authenticate quoted-string grammar, which can't
		// carry non-ASCII; operators must supply the encoded form.
		"non-ASCII URL": {
			env:     map[string]string{"OAUTH_CANONICAL_RESOURCE_URI": "https://mcp.exämple.com/mcp"},
			wantErr: "must be printable ASCII",
		},
		// A typo'd boolean must fail loudly rather than silently disabling
		// OAuth (and skipping all the validation above).
		"unrecognized OAUTH_ENABLED": {
			env:     map[string]string{"OAUTH_ENABLED": "on"},
			wantErr: "OAUTH_ENABLED must be true or false",
		},
		"unrecognized passthrough": {
			env:     map[string]string{"OAUTH_API_KEY_PASSTHROUGH": "ture"},
			wantErr: "OAUTH_API_KEY_PASSTHROUGH must be true or false",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			setEnabledEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			_, err := FromEnv()

			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestCanonicalResourceURI(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string
	}{
		"lowercases scheme and host":      {"HTTPS://MCP.Example.com/mcp", "https://mcp.example.com/mcp"},
		"drops default https port":        {"https://mcp.example.com:443/mcp", "https://mcp.example.com/mcp"},
		"drops default http port":         {"http://mcp.example.com:80/mcp", "http://mcp.example.com/mcp"},
		"keeps non-default port":          {"https://mcp.example.com:8443/mcp", "https://mcp.example.com:8443/mcp"},
		"re-brackets IPv6 literals":       {"https://[::1]:443/mcp", "https://[::1]/mcp"},
		"bare slash equals no path":       {"https://mcp.example.com/", "https://mcp.example.com"},
		"preserves deep trailing slash":   {"https://mcp.example.com/mcp/", "https://mcp.example.com/mcp/"},
		"preserves path case":             {"https://mcp.example.com/MCP", "https://mcp.example.com/MCP"},
		"preserves query":                 {"https://mcp.example.com/mcp?a=B", "https://mcp.example.com/mcp?a=B"},
		"unparseable returned unchanged":  {"::not-a-url::", "::not-a-url::"},
		"missing host returned unchanged": {"/mcp", "/mcp"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, canonicalResourceURI(tc.in))
		})
	}
}

func TestMetadataPaths(t *testing.T) {
	t.Run("resource with a path gets the path-insertion form first", func(t *testing.T) {
		cfg := Config{CanonicalResourceURI: "https://mcp.example.com/mcp"}

		require.Equal(t, []string{
			"/.well-known/oauth-protected-resource/mcp",
			"/.well-known/oauth-protected-resource",
		}, cfg.MetadataPaths())
	})

	t.Run("resource without a path gets the root form only", func(t *testing.T) {
		cfg := Config{CanonicalResourceURI: "https://mcp.example.com"}

		require.Equal(t, []string{"/.well-known/oauth-protected-resource"}, cfg.MetadataPaths())
	})
}

func TestMetadataURL(t *testing.T) {
	t.Run("inserts the well-known path between host and resource path", func(t *testing.T) {
		cfg := Config{CanonicalResourceURI: "https://mcp.example.com/mcp"}

		require.Equal(t,
			"https://mcp.example.com/.well-known/oauth-protected-resource/mcp",
			cfg.MetadataURL())
	})

	t.Run("resource without a path", func(t *testing.T) {
		cfg := Config{CanonicalResourceURI: "https://mcp.example.com"}

		require.Equal(t,
			"https://mcp.example.com/.well-known/oauth-protected-resource",
			cfg.MetadataURL())
	})
}

func TestBoolEnv(t *testing.T) {
	for _, v := range []string{"true", "TRUE", "1", "yes", " true "} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("OAUTH_ENABLED", v)
			got, err := boolEnv("OAUTH_ENABLED", false)
			require.NoError(t, err)
			require.True(t, got)
		})
	}
	for _, v := range []string{"false", "0", "no", "FALSE"} {
		t.Run("not "+v, func(t *testing.T) {
			t.Setenv("OAUTH_ENABLED", v)
			got, err := boolEnv("OAUTH_ENABLED", true)
			require.NoError(t, err)
			require.False(t, got)
		})
	}

	t.Run("unset returns the default", func(t *testing.T) {
		t.Setenv("OAUTH_ENABLED", "")
		got, err := boolEnv("OAUTH_ENABLED", true)
		require.NoError(t, err)
		require.True(t, got)
	})

	t.Run("unrecognized value is an error", func(t *testing.T) {
		t.Setenv("OAUTH_ENABLED", "enabled")
		_, err := boolEnv("OAUTH_ENABLED", false)
		require.ErrorContains(t, err, `got "enabled"`)
	})
}
