package blueprint

import (
	"errors"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

func blueprintIDFromRequest(request mcp.CallToolRequest) (string, error) {
	blueprintID, err := validate.RequiredToolParam[string](request, "blueprintId")
	if err != nil {
		return "", err
	}
	blueprintID = strings.TrimSpace(blueprintID)
	if !strings.HasPrefix(blueprintID, "exs-") {
		return "", errors.New("blueprintId must be a Blueprint ID starting with exs-")
	}
	return blueprintID, nil
}
