package blueprint

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlueprintIDFromRequest(t *testing.T) {
	t.Run("trims surrounding whitespace", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{"blueprintId": " \texs-test\n\u00a0"}

		id, err := blueprintIDFromRequest(request)

		require.NoError(t, err)
		assert.Equal(t, "exs-test", id)
	})

	for _, tt := range []struct {
		name    string
		args    map[string]any
		message string
	}{
		{
			name:    "missing Blueprint ID",
			args:    nil,
			message: "required parameter not present: blueprintId",
		},
		{
			name:    "non-string Blueprint ID",
			args:    map[string]any{"blueprintId": 1},
			message: "parameter blueprintId is not of expected type: string",
		},
		{
			name:    "sync ID",
			args:    map[string]any{"blueprintId": "exe-test"},
			message: "blueprintId must be a Blueprint ID starting with exs-",
		},
		{
			name:    "empty Blueprint ID",
			args:    map[string]any{"blueprintId": ""},
			message: "blueprintId must be a Blueprint ID starting with exs-",
		},
		{
			name:    "whitespace-only Blueprint ID",
			args:    map[string]any{"blueprintId": " \t\n"},
			message: "blueprintId must be a Blueprint ID starting with exs-",
		},
	} {
		t.Run("rejects "+tt.name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Arguments = tt.args

			id, err := blueprintIDFromRequest(request)

			assert.EqualError(t, err, tt.message)
			assert.Empty(t, id)
		})
	}
}
