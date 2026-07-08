package mcpserver_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/mcpserver"
)

// TestServicePlanEnumValues guards that "free" stays in the service
// plan list even though it is not a member of the generated PaidPlan enum.
func TestServicePlanEnumValues(t *testing.T) {
	values := mcpserver.ServicePlanEnumValues()

	assert.Contains(t, values, "free")

	// Every generated paid plan must be advertised.
	for _, p := range client.PaidPlanValues() {
		assert.Contains(t, values, string(p))
	}
}

// TestPostgresPlanEnumValuesExcludesLegacyAndCustom guards that the Postgres
// enum does not include custom or legacy plan names.
func TestPostgresPlanEnumValuesExcludesLegacyAndCustom(t *testing.T) {
	values := mcpserver.PostgresPlanEnumValues()

	for _, excluded := range []string{"custom", "starter", "standard", "pro", "pro_plus"} {
		assert.NotContains(t, values, excluded)
	}
}

// TestKeyValuePlanEnumValuesExcludesCustom guards that the KV enum drops only the
// custom sentinel.
func TestKeyValuePlanEnumValuesExcludesCustom(t *testing.T) {
	values := mcpserver.KeyValuePlanEnumValues()

	assert.NotContains(t, values, "custom")
}
