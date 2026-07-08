package mcpserver

import (
	"slices"

	"github.com/render-oss/render-mcp-server/pkg/client"
	pgclient "github.com/render-oss/render-mcp-server/pkg/client/postgres"
)

// ValidServicePlanValues are the service plans the MCP server exposes and
// accepts. It is derived from the generated client.PaidPlanValues() so newly
// added paid plans appear automatically. "free" is a valid service plan but not
// a member of the PaidPlan enum, so it is prepended explicitly.
var ValidServicePlanValues = servicePlanValues()

func servicePlanValues() []client.PaidPlan {
	return append([]client.PaidPlan{client.PaidPlan("free")}, client.PaidPlanValues()...)
}

// legacyPostgresPlans are the deprecated plans that predate the
// basic_*/pro_*gb/accelerated_* naming. They remain valid PostgresPlans enum members
// (so PostgresPlansValues() returns them) but the MCP server does not expose or accept
// them.
var legacyPostgresPlans = []pgclient.PostgresPlans{
	pgclient.Starter,
	pgclient.Standard,
	pgclient.Pro,
	pgclient.ProPlus,
}

// ValidPostgresPlanValues are the non-legacy Postgres plans the MCP server exposes
// and accepts. It is derived from the generated PostgresPlansValues() with the
// "custom" sentinel and the legacy plans filtered out.
var ValidPostgresPlanValues = modernPostgresPlanValues()

func modernPostgresPlanValues() []pgclient.PostgresPlans {
	return slices.DeleteFunc(pgclient.PostgresPlansValues(), func(p pgclient.PostgresPlans) bool {
		return p == pgclient.Custom || slices.Contains(legacyPostgresPlans, p)
	})
}

// ValidKeyValuePlanValues are the Key Value plans the MCP server exposes and
// accepts. It is derived from the generated KeyValuePlanValues() with the
// "custom" sentinel filtered out (the MCP server does not support custom KV
// plans).
var ValidKeyValuePlanValues = nonCustomKeyValuePlanValues()

func nonCustomKeyValuePlanValues() []client.KeyValuePlan {
	return slices.DeleteFunc(client.KeyValuePlanValues(), func(p client.KeyValuePlan) bool {
		return p == client.KeyValuePlanCustom
	})
}

func RegionEnumValues() []string {
	return EnumValuesFromClientType(
		client.Oregon,
		client.Frankfurt,
		client.Singapore,
		client.Ohio,
		client.Virginia,
	)
}

func ServicePlanEnumValues() []string {
	return EnumValuesFromClientType(ValidServicePlanValues...)
}

func PostgresPlanEnumValues() []string {
	return EnumValuesFromClientType(ValidPostgresPlanValues...)
}

func KeyValuePlanEnumValues() []string {
	return EnumValuesFromClientType(ValidKeyValuePlanValues...)
}

func EnumValuesFromClientType[T ~string](t ...T) []string {
	values := make([]string, 0, len(t))
	for _, val := range t {
		values = append(values, string(val))
	}
	return values
}
