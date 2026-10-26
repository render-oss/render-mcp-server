package blueprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepoURLNormalization(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "preserves HTTPS URLs",
			input: "https://github.com/example/my-app.git",
			want:  "https://github.com/example/my-app.git",
		},
		{
			name:  "converts SSH URLs to HTTPS and removes the SSH port",
			input: "ssh://git@gitlab.com:22/group/subgroup/my-app.git",
			want:  "https://gitlab.com/group/subgroup/my-app.git",
		},
		{
			name:  "removes trailing slashes",
			input: "https://github.com/example/my-app/",
			want:  "https://github.com/example/my-app",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRepoURL(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.String())
		})
	}
}

func TestRepoURLValidation(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input string
	}{
		{"local path", "../my-app"},
		{"scp style SSH remote", "git@github.com:example/my-app.git"},
		{"unsupported scheme", "http://github.com/example/my-app"},
		{"missing host", "https:///example/my-app"},
		{"missing repository", "https://github.com/"},
		{"empty repository name", "https://github.com/example/.git"},
		{"query string", "https://github.com/example/my-app?branch=main"},
		{"fragment", "https://github.com/example/my-app#main"},
		{"invalid URL escape", "https://github.com/example/%"},
	} {
		t.Run("rejects "+tt.name, func(t *testing.T) {
			_, err := parseRepoURL(tt.input)
			require.Error(t, err)
		})
	}
}
