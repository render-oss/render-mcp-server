package mcpserver

import (
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	pgclient "github.com/render-oss/render-mcp-server/pkg/client/postgres"
)

func RegionEnumValues() []string {
	return EnumValuesFromClientType(
		client.Oregon,
		client.Frankfurt,
		client.Singapore,
		client.Ohio,
		client.Virginia,
	)
}

func PostgresPlanEnumValues() []string {
	return EnumValuesFromClientType(
		pgclient.Free,
		pgclient.Basic256mb,
		pgclient.Basic1gb,
		pgclient.Basic4gb,
		pgclient.Pro4gb,
		pgclient.Pro8gb,
		pgclient.Pro16gb,
		pgclient.Pro32gb,
		pgclient.Pro64gb,
		pgclient.Pro128gb,
		pgclient.Pro192gb,
		pgclient.Pro256gb,
		pgclient.Pro384gb,
		pgclient.Pro512gb,
		pgclient.Accelerated16gb,
		pgclient.Accelerated32gb,
		pgclient.Accelerated64gb,
		pgclient.Accelerated128gb,
		pgclient.Accelerated256gb,
		pgclient.Accelerated384gb,
		pgclient.Accelerated512gb,
		pgclient.Accelerated768gb,
		pgclient.Accelerated1024gb,
	)
}

func EnumValuesFromClientType[T ~string](t ...T) []string {
	values := make([]string, 0, len(t))
	for _, val := range t {
		values = append(values, string(val))
	}
	return values
}

var UnavailableDueToSensitiveInfoToolResult = mcp.NewToolResultText(
	"This tool is not available when sensitive info is disabled.")
