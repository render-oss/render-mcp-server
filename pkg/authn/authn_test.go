package authn_test

import (
	"testing"

	"github.com/render-oss/render-mcp-server/pkg/authn"
	"github.com/stretchr/testify/require"
)

func TestBearerToken(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string
	}{
		"strips the Bearer prefix":       {"Bearer rnd_abc", "rnd_abc"},
		"strips case-insensitively":      {"bearer rnd_abc", "rnd_abc"},
		"bare token passes through":      {"rnd_abc", "rnd_abc"},
		"other schemes pass through":     {"Basic dXNlcjpwYXNz", "Basic dXNlcjpwYXNz"},
		"empty stays empty":              {"", ""},
		"prefix without a token is bare": {"Bearer ", "Bearer "},
		"prefix must end with the space": {"Bearerrnd_abc", "Bearerrnd_abc"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, authn.BearerToken(tc.in))
		})
	}
}
