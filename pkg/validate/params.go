package validate

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

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

// cronFieldBounds describes the allowed numeric range for one cron field.
type cronFieldBounds struct {
	min int
	max int
}

// CronSchedule validates a 5-field standard cron expression (minute hour
// day-of-month month day-of-week) as accepted by create_cron_job. It supports
// wildcards, single values, ranges, steps, and comma-separated lists, plus
// JAN-DEC month names and SUN-SAT day-of-week names. Day-of-week accepts 0-7
// (both 0 and 7 mean Sunday).
func CronSchedule(schedule string) error {
	const formatHint = "expected 5 fields: minute (0-59) hour (0-23) day of month (1-31) month (1-12) day of week (0-6, Sunday=0)"

	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return fmt.Errorf("invalid schedule expression %q: %s", schedule, formatHint)
	}

	bounds := []cronFieldBounds{
		{min: 0, max: 59},
		{min: 0, max: 23},
		{min: 1, max: 31},
		{min: 1, max: 12},
		{min: 0, max: 7},
	}

	for i, field := range fields {
		if err := cronField(field, bounds[i], i == 3, i == 4); err != nil {
			return fmt.Errorf("invalid schedule expression %q: field %d (%q): %w", schedule, i+1, field, err)
		}
	}
	return nil
}

func cronField(field string, bounds cronFieldBounds, allowMonthNames, allowDowNames bool) error {
	if field == "" {
		return fmt.Errorf("empty field")
	}
	for _, item := range strings.Split(field, ",") {
		if err := cronItem(item, bounds, allowMonthNames, allowDowNames); err != nil {
			return err
		}
	}
	return nil
}

func cronItem(item string, bounds cronFieldBounds, allowMonthNames, allowDowNames bool) error {
	if item == "" {
		return fmt.Errorf("empty list entry")
	}
	base := item
	if slash := strings.Index(item, "/"); slash >= 0 {
		base = item[:slash]
		stepStr := item[slash+1:]
		if strings.Contains(stepStr, "/") || stepStr == "" {
			return fmt.Errorf("invalid step in %q", item)
		}
		step, err := strconv.Atoi(stepStr)
		if err != nil || step < 1 {
			return fmt.Errorf("invalid step in %q: step must be a positive integer", item)
		}
		if base == "" {
			return fmt.Errorf("invalid step in %q: missing base before '/'", item)
		}
	}
	if base == "*" {
		return nil
	}
	if strings.Contains(base, "-") {
		parts := strings.Split(base, "-")
		if len(parts) != 2 {
			return fmt.Errorf("invalid range in %q", item)
		}
		lo, err := cronValue(parts[0], bounds, allowMonthNames, allowDowNames)
		if err != nil {
			return err
		}
		hi, err := cronValue(parts[1], bounds, allowMonthNames, allowDowNames)
		if err != nil {
			return err
		}
		if lo > hi {
			return fmt.Errorf("invalid range in %q: start must not exceed end", item)
		}
		return nil
	}
	_, err := cronValue(base, bounds, allowMonthNames, allowDowNames)
	return err
}

func cronValue(token string, bounds cronFieldBounds, allowMonthNames, allowDowNames bool) (int, error) {
	if token == "" {
		return 0, fmt.Errorf("empty value")
	}
	if v, ok := cronNameValue(token, allowMonthNames, allowDowNames); ok {
		if v < bounds.min || v > bounds.max {
			return 0, fmt.Errorf("value %q out of range (%d-%d)", token, bounds.min, bounds.max)
		}
		return v, nil
	}
	if allowMonthNames || allowDowNames {
		lower := strings.ToLower(token)
		if isAlpha(lower) {
			return 0, fmt.Errorf("unknown name %q", token)
		}
	}
	v, err := strconv.Atoi(token)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q: must be an integer, a range, a step, a list, or *", token)
	}
	if v < bounds.min || v > bounds.max {
		return 0, fmt.Errorf("value %q out of range (%d-%d)", token, bounds.min, bounds.max)
	}
	return v, nil
}

func cronNameValue(token string, allowMonthNames, allowDowNames bool) (int, bool) {
	lower := strings.ToLower(token)
	if allowMonthNames {
		if v, ok := map[string]int{
			"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
			"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
		}[lower]; ok {
			return v, true
		}
	}
	if allowDowNames {
		if v, ok := map[string]int{
			"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
		}[lower]; ok {
			return v, true
		}
	}
	return 0, false
}

func isAlpha(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}
