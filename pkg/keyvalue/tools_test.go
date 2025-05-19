package keyvalue

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

func TestGetKeyValueConnectionInfoTool(t *testing.T) {
	keyValueId := "kv-123456"
	sensitiveValue := "sensitive_value"

	tests := []struct {
		name                     string
		includeSensitiveInfo     bool
		expectedResponseContains string
	}{
		{
			name:                     "Get Key Value connection info without sensitive info",
			includeSensitiveInfo:     false,
			expectedResponseContains: "tool is not available when sensitive info is disabled",
		},
		{
			name:                     "Get Key Value connection info with sensitive info",
			includeSensitiveInfo:     true,
			expectedResponseContains: `Connection info: {"cliCommand":"sensitive_value","externalConnectionString":"sensitive_value","internalConnectionString":"potentially sensitive_value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.InitRuntimeConfig(tt.includeSensitiveInfo)
			fakeClient := &fakes.FakeKeyValueRepoClient{}
			repo := NewRepo(fakeClient)

			fakeClient.RetrieveKeyValueConnectionInfoWithResponseReturns(
				&client.RetrieveKeyValueConnectionInfoResponse{
					JSON200: &client.KeyValueConnectionInfo{
						InternalConnectionString: "potentially " + sensitiveValue,
						ExternalConnectionString: sensitiveValue,
						CliCommand:               sensitiveValue,
					},
					HTTPResponse: &http.Response{
						StatusCode: 200,
					},
				}, nil)

			request := mcp.CallToolRequest{}
			request.Params.Arguments = map[string]interface{}{
				"keyValueId": keyValueId,
			}

			tool := getKeyValueConnectionInfo(repo)
			result, err := tool.Handler(context.Background(), request)

			assert.NoError(t, err)
			assert.NotNil(t, result)

			for _, content := range result.Content {
				if textContent, ok := content.(mcp.TextContent); ok {
					assert.Contains(t, textContent.Text, tt.expectedResponseContains)
					if tt.includeSensitiveInfo {
						assert.Contains(t, textContent.Text, sensitiveValue)
					} else {
						assert.NotContains(t, textContent.Text, sensitiveValue)
					}
				}
			}
		})
	}
}
