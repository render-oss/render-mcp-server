package blueprint

import (
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	blueprintclient "github.com/render-oss/render-mcp-server/pkg/client/blueprints"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

type updateBlueprintResponse struct {
	Blueprint       blueprintclient.Blueprint         `json:"blueprint"`
	PlanningOutcome *blueprintclient.PlanningOutcome  `json:"planningOutcome"`
	Sync            *blueprintclient.SyncWithPlan     `json:"sync"`
	Notices         []blueprintclient.BlueprintNotice `json:"notices"`
	Error           *updateBlueprintError             `json:"error"`
}

type updateBlueprintError struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

func validateUpdateBlueprintParams(request mcp.CallToolRequest) (string, client.UpdateBlueprintJSONRequestBody, error) {
	var input client.UpdateBlueprintJSONRequestBody
	blueprintID, err := blueprintIDFromRequest(request)
	if err != nil {
		return "", input, err
	}
	if name, present, err := validate.OptionalToolParam[string](request, "name"); err != nil {
		return "", input, err
	} else if present {
		if strings.TrimSpace(name) == "" {
			return "", input, fmt.Errorf("The name setting cannot be empty or contain only whitespace. Omit it to keep its current value.")
		}
		input.Name = &name
	}
	if branch, present, err := validate.OptionalToolParam[string](request, "branch"); err != nil {
		return "", input, err
	} else if present {
		if strings.TrimSpace(branch) == "" {
			return "", input, fmt.Errorf("The branch setting cannot be empty or contain only whitespace. Omit it to keep its current value.")
		}
		input.Branch = &branch
	}
	if path, present, err := validate.OptionalToolParam[string](request, "path"); err != nil {
		return "", input, err
	} else if present {
		if strings.TrimSpace(path) == "" {
			return "", input, fmt.Errorf("The path setting cannot be empty or contain only whitespace. Omit it to keep its current value.")
		}
		input.Path = &path
	}
	if autoSync, present, err := validate.OptionalToolParam[bool](request, "autoSync"); err != nil {
		return "", input, err
	} else if present {
		input.AutoSync = &autoSync
	}
	if input.Name == nil && input.Branch == nil && input.Path == nil && input.AutoSync == nil {
		return "", input, fmt.Errorf("Provide at least one of name, branch, path, or autoSync.")
	}
	return blueprintID, input, nil
}
