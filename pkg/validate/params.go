package validate

import (
	"errors"
	"fmt"
	"slices"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	envvar "github.com/render-oss/render-mcp-server/pkg/client/envvar"
	pgclient "github.com/render-oss/render-mcp-server/pkg/client/postgres"
	"github.com/render-oss/render-mcp-server/pkg/config"
	"github.com/render-oss/render-mcp-server/pkg/mcpserver"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
)

func RequiredToolParam[T any](request mcp.CallToolRequest, param string) (T, error) {
	var zero T
	if _, ok := request.GetArguments()[param]; !ok {
		return zero, fmt.Errorf("required parameter not present: %s", param)
	}

	value, ok := request.GetArguments()[param].(T)
	if !ok {
		return zero, fmt.Errorf("parameter %s is not of expected type: %T", param, zero)
	}

	return value, nil
}

// OptionalToolParam retrieves an optional parameter from the request arguments.
// If the parameter is present and of the correct type, it returns the value and true.
// If the parameter is not present or of the incorrect type, it returns the zero value and false.
func OptionalToolParam[T any](request mcp.CallToolRequest, param string) (T, bool, error) {
	var zero T
	if _, ok := request.GetArguments()[param]; !ok {
		return zero, false, nil
	}

	value, ok := request.GetArguments()[param].(T)
	if !ok {
		return zero, false, fmt.Errorf("parameter %s is not of expected type: %T", param, zero)
	}

	return value, true, nil
}

func RequiredToolArrayParam[T any](request mcp.CallToolRequest, param string) ([]T, error) {
	if _, ok := request.GetArguments()[param]; !ok {
		return nil, fmt.Errorf("required parameter not present: %s", param)
	}

	return extractArrayParam[T](request, param)
}

func extractArrayParam[T any](request mcp.CallToolRequest, param string) ([]T, error) {
	interfaceArray, ok := request.GetArguments()[param].([]interface{})
	if !ok {
		return nil, fmt.Errorf("parameter %s is not a valid array", param)
	}

	outputArray := make([]T, 0, len(interfaceArray))
	for _, item := range interfaceArray {
		val, ok := item.(T)
		if !ok {
			return nil, fmt.Errorf("parameter %s is not of expected type", param)
		}
		outputArray = append(outputArray, val)
	}

	return outputArray, nil
}

func OptionalToolArrayParam[T any](request mcp.CallToolRequest, param string) ([]T, bool, error) {
	if _, ok := request.GetArguments()[param]; !ok {
		return nil, false, nil
	}

	outputArray, err := extractArrayParam[T](request, param)
	if err != nil {
		return nil, false, err
	}

	return outputArray, true, nil
}

func EnvVars(request mcp.CallToolRequest) ([]envvar.EnvVarInput, bool, error) {
	if _, ok := request.GetArguments()["envVars"]; !ok {
		return nil, false, nil
	}

	var envVars envvar.EnvVarInputArray
	invalidErr := errors.New("parameter envVars is not of expected type")
	if envVarsRaw, ok := request.GetArguments()["envVars"]; ok && envVarsRaw != nil {
		envVarsSlice, ok := envVarsRaw.([]interface{})
		if !ok {
			return nil, false, invalidErr
		}

		for _, item := range envVarsSlice {
			envVarMap, ok := item.(map[string]interface{})
			if !ok {
				return nil, false, invalidErr
			}

			key, ok := envVarMap["key"].(string)
			if !ok {
				return nil, false, invalidErr
			}

			value, ok := envVarMap["value"].(string)
			if !ok {
				return nil, false, invalidErr
			}

			var envVarInput envvar.EnvVarInput
			envVarKeyValue := envvar.EnvVarKeyValue{
				Key:   key,
				Value: value,
			}
			err := envVarInput.FromEnvVarKeyValue(envVarKeyValue)
			if err != nil {
				return nil, false, invalidErr
			}
			envVars = append(envVars, envVarInput)
		}
	}

	return envVars, true, nil
}

func ServicePlan(plan string) (*client.PaidPlan, error) {
	paidPlan := client.PaidPlan(plan)
	if slices.Contains(mcpserver.ValidServicePlanValues, paidPlan) {
		return &paidPlan, nil
	}
	return nil, fmt.Errorf("invalid service plan: %s", plan)
}

func KeyValuePlan(plan string) (*client.KeyValuePlan, error) {
	kvPlan := client.KeyValuePlan(plan)
	switch {
	case kvPlan == client.KeyValuePlanCustom:
		return nil, fmt.Errorf("MCP server doesn't support custom Key Value plans. "+
			"If you're looking to create a Key Value instance with a custom plan, use the dashboard at: %s/%s", config.DashboardURL(), "new/redis")
	case kvPlan.Valid():
		return pointers.From(kvPlan), nil
	default:
		return nil, fmt.Errorf("invalid Key Value plan: %s", plan)
	}
}

func PostgresPlan(plan string) (pgclient.PostgresPlans, error) {
	pgPlan := pgclient.PostgresPlans(plan)
	switch {
	case pgPlan == pgclient.Custom:
		return "", fmt.Errorf("MCP server doesn't support custom Postgres plans. "+
			"If you're looking to create a Postgres instance with a custom plan, use the dashboard at: %s/%s", config.DashboardURL(), "new/database")
	// PostgresPlans.Valid() would also admit the deprecated legacy tier names
	// (starter/standard/pro/pro_plus); membership in the modern source-of-truth
	// list keeps those rejected.
	case slices.Contains(mcpserver.ValidPostgresPlanValues, pgPlan):
		return pgPlan, nil
	default:
		return "", fmt.Errorf("invalid Postgres plan: %s", plan)
	}
}

func PostgresDiskSizeGb(diskSizeGb int) error {
	if diskSizeGb == 0 {
		// Allowed for free plan
		return nil
	}
	if diskSizeGb == 1 || diskSizeGb%5 == 0 {
		return nil
	}
	return fmt.Errorf("diskSizeGb can be 0 for the free plan, otherwise it must be either 1, or a multiple of 5")
}
