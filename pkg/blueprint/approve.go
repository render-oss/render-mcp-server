package blueprint

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	blueprintclient "github.com/render-oss/render-mcp-server/pkg/client/blueprints"
	"github.com/render-oss/render-mcp-server/pkg/mcpserver"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

func manualEnvVarServiceTypes() []string {
	return mcpserver.EnumValuesFromClientType(blueprintclient.ManualEnvVarTargetTypeValues()...)
}

// validateApproveBlueprintParams returns the trimmed Blueprint ID and REST approval
// request body after validating the tool parameters.
func validateApproveBlueprintParams(request mcp.CallToolRequest) (string, client.ApproveBlueprintJSONRequestBody, error) {
	var zero client.ApproveBlueprintJSONRequestBody
	blueprintID, err := blueprintIDFromRequest(request)
	if err != nil {
		return "", zero, err
	}

	entries, present, err := validate.OptionalToolArrayParam[map[string]any](request, "manualEnvVars")
	if err != nil {
		return "", zero, err
	}
	if !present {
		// No manual environment variables were supplied, so no further validation
		// is needed. The zero-value body is a valid approval request.
		return blueprintID, zero, nil
	}
	values := make([]blueprintclient.SuppliedManualEnvVar, 0, len(entries))
	seen := make(map[blueprintclient.RequiredManualEnvVar]int)
	validFields := []string{"serviceType", "serviceName", "key", "value"}
	for i, entry := range entries {
		// Do not include supplied field names or values in validation errors.
		for field := range entry {
			if !slices.Contains(validFields, field) {
				return "", zero, fmt.Errorf("manualEnvVars[%d] contains an unsupported field; use %s", i, strings.Join(validFields, ", "))
			}
		}
		fields := make(map[string]string, 4)
		for _, field := range []string{"serviceType", "serviceName", "key", "value"} {
			fieldValue, ok := entry[field].(string)
			if !ok {
				return "", zero, fmt.Errorf("manualEnvVars[%d].%s is required and must be a string", i, field)
			}
			if field != "value" && fieldValue == "" {
				// env var values are allowed to be empty, otherwise reject empty strings
				return "", zero, fmt.Errorf("manualEnvVars[%d].%s cannot be empty", i, field)
			}
			fields[field] = fieldValue
		}
		serviceType := blueprintclient.ManualEnvVarTargetType(fields["serviceType"])
		if !serviceType.Valid() {
			return "", zero, fmt.Errorf("manualEnvVars[%d].serviceType must be one of: %s", i, strings.Join(manualEnvVarServiceTypes(), ", "))
		}
		target := blueprintclient.RequiredManualEnvVar{ServiceType: serviceType, ServiceName: fields["serviceName"], Key: fields["key"]}
		if inputPosition, alreadyExists := seen[target]; alreadyExists {
			return "", zero, fmt.Errorf("manualEnvVars[%d] duplicates manualEnvVars[%d] for the same resource and key", i, inputPosition)
		}
		seen[target] = i
		values = append(values, blueprintclient.SuppliedManualEnvVar{
			ServiceType: serviceType,
			ServiceName: fields["serviceName"],
			Key:         fields["key"],
			Value:       new(fields["value"]),
		})
	}
	return blueprintID, client.ApproveBlueprintJSONRequestBody{ManualEnvVars: &values}, nil
}
