package postgres

import (
	"context"
	"net/http"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/config"
	"github.com/render-oss/render-mcp-server/pkg/fakes"
	"github.com/stretchr/testify/assert"
)

func TestGetPostgresConnectionInfoTool(t *testing.T) {
	postgresId := "pg-123456"
	sensitiveValue := "sensitive_value"
	toolIsNotAvailable := "tool is not available when sensitive info is disabled"

	tests := []struct {
		name                     string
		includeSensitiveInfo     bool
		expectedResponseContains string
	}{
		{
			name:                     "Get Postgres connection info without sensitive info",
			includeSensitiveInfo:     false,
			expectedResponseContains: "tool is not available when sensitive info is disabled",
		},
		{
			name:                     "Get Postgres connection info with sensitive info",
			includeSensitiveInfo:     true,
			expectedResponseContains: "externalConnectionString",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.InitRuntimeConfig(tt.includeSensitiveInfo)
			fakeClient := &fakes.FakePostgresRepoClient{}
			repo := NewRepo(fakeClient)

			fakeClient.RetrievePostgresConnectionInfoWithResponseReturns(
				&client.RetrievePostgresConnectionInfoResponse{
					JSON200: &client.PostgresConnectionInfo{
						Password: sensitiveValue,
					},
					HTTPResponse: &http.Response{
						StatusCode: 200,
					},
				}, nil)

			request := mcp.CallToolRequest{}
			request.Params.Arguments = map[string]interface{}{
				"postgresId": postgresId,
			}

			tool := getPostgresConnectionInfo(repo)
			result, err := tool.Handler(context.Background(), request)

			assert.NoError(t, err)
			assert.NotNil(t, result)

			for _, content := range result.Content {
				if textContent, ok := content.(mcp.TextContent); ok {
					assert.Contains(t, textContent.Text, tt.expectedResponseContains)
					if tt.includeSensitiveInfo {
						assert.Contains(t, textContent.Text, sensitiveValue)
						assert.NotContains(t, textContent.Text, toolIsNotAvailable)
					} else {
						assert.Contains(t, textContent.Text, toolIsNotAvailable)
						assert.NotContains(t, textContent.Text, sensitiveValue)
					}
				}
			}
		})
	}
}
