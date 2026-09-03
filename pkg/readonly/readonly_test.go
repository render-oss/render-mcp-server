package readonly

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPGuardAcceptedModes(t *testing.T) {
	tests := []struct {
		name       string
		headers    []string
		readOnly   bool
		statusCode int
	}{
		{name: "absent keeps full access", readOnly: false, statusCode: http.StatusOK},
		{name: "false keeps full access", headers: []string{"false"}, readOnly: false, statusCode: http.StatusOK},
		{name: "true enables read only", headers: []string{"true"}, readOnly: true, statusCode: http.StatusOK},
		{name: "uppercase is malformed", headers: []string{"TRUE"}, statusCode: http.StatusBadRequest},
		{name: "whitespace is malformed", headers: []string{" true"}, statusCode: http.StatusBadRequest},
		{name: "arbitrary value is malformed", headers: []string{"yes"}, statusCode: http.StatusBadRequest},
		{name: "duplicate is ambiguous", headers: []string{"true", "true"}, statusCode: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				assert.Equal(t, tt.readOnly, FromContext(r.Context()))
				w.WriteHeader(http.StatusOK)
			})
			request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			for _, value := range tt.headers {
				request.Header.Add("x-mcp-readonly", value)
			}
			recorder := httptest.NewRecorder()

			NewHTTPGuard().Middleware(handler).ServeHTTP(recorder, request)

			assert.Equal(t, tt.statusCode, recorder.Code)
			assert.Equal(t, tt.statusCode == http.StatusOK, called)
		})
	}
}

func TestHTTPGuardBindsReadOnlyModeToInitializedSession(t *testing.T) {
	const sessionID = "mcp-session-test"
	calls := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Mcp-Session-Id", sessionID)
		}
		w.WriteHeader(http.StatusOK)
	})
	guarded := NewHTTPGuard().Middleware(handler)

	initialize := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	initialize.Header.Set(HeaderName, "true")
	guarded.ServeHTTP(httptest.NewRecorder(), initialize)

	matching := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	matching.Header.Set("Mcp-Session-Id", sessionID)
	matching.Header.Set(HeaderName, "true")
	matchingRecorder := httptest.NewRecorder()
	guarded.ServeHTTP(matchingRecorder, matching)
	require.Equal(t, http.StatusOK, matchingRecorder.Code)
	require.Equal(t, 2, calls)

	for _, header := range []string{"", "false"} {
		downgrade := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		downgrade.Header.Set("Mcp-Session-Id", sessionID)
		if header != "" {
			downgrade.Header.Set(HeaderName, header)
		}
		downgradeRecorder := httptest.NewRecorder()
		guarded.ServeHTTP(downgradeRecorder, downgrade)
		assert.Equal(t, http.StatusConflict, downgradeRecorder.Code)
		assert.Contains(t, downgradeRecorder.Body.String(), "must match")
	}
	require.Equal(t, 2, calls, "downgrade attempts must not reach the MCP handler")
}

func TestHTTPGuardFullAccessModesReturnCompleteToolset(t *testing.T) {
	readOnlyTool := classifiedTool("get_thing", true)
	mutatingTool := classifiedTool("delete_thing", false)
	policy, err := NewPolicy([]server.ServerTool{readOnlyTool, mutatingTool})
	require.NoError(t, err)

	for _, header := range []string{"", "false"} {
		t.Run("header="+header, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				listed := policy.Filter(r.Context(), []mcp.Tool{readOnlyTool.Tool, mutatingTool.Tool})
				assert.Equal(t, []string{"get_thing", "delete_thing"}, toolNames(listed))
				w.WriteHeader(http.StatusOK)
			})
			request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if header != "" {
				request.Header.Set(HeaderName, header)
			}
			recorder := httptest.NewRecorder()
			NewHTTPGuard().Middleware(handler).ServeHTTP(recorder, request)
			require.Equal(t, http.StatusOK, recorder.Code)
		})
	}
}

func TestPolicyFiltersDiscoveryAndGuardsDirectCalls(t *testing.T) {
	readOnlyTool := classifiedTool("get_thing", true)
	mutatingTool := classifiedTool("delete_thing", false)
	policy, err := NewPolicy([]server.ServerTool{readOnlyTool, mutatingTool})
	require.NoError(t, err)

	fullTools := policy.Filter(context.Background(), []mcp.Tool{readOnlyTool.Tool, mutatingTool.Tool})
	require.Len(t, fullTools, 2)

	ctx := context.WithValue(context.Background(), modeContextKey{}, readOnly)
	unclassified := mcp.NewTool("not_audited")
	unclassified.Annotations.ReadOnlyHint = nil
	filtered := policy.Filter(ctx, []mcp.Tool{readOnlyTool.Tool, mutatingTool.Tool, unclassified})
	require.Equal(t, []string{"get_thing"}, toolNames(filtered))

	readCalls := 0
	readHandler := policy.Middleware(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		readCalls++
		return mcp.NewToolResultText("read"), nil
	})
	readResult, err := readHandler(ctx, callRequest("get_thing"))
	require.NoError(t, err)
	require.False(t, readResult.IsError)
	require.Equal(t, 1, readCalls)

	mutationCalls := 0
	mutationHandler := policy.Middleware(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		mutationCalls++
		return mcp.NewToolResultText("mutated"), nil
	})
	fullAccessResult, err := mutationHandler(context.Background(), callRequest("delete_thing"))
	require.NoError(t, err)
	require.False(t, fullAccessResult.IsError, "stdio and HTTP requests without read-only context retain full access")
	require.Equal(t, 1, mutationCalls)
	for _, name := range []string{"delete_thing", "not_audited"} {
		result, err := mutationHandler(ctx, callRequest(name))
		require.NoError(t, err)
		require.True(t, result.IsError)
		assert.Equal(t, PermissionDeniedMessage, result.Content[0].(mcp.TextContent).Text)
	}
	require.Equal(t, 1, mutationCalls, "read-only calls must not enter the wrapped handler chain")
}

func TestPolicyRejectsMissingClassification(t *testing.T) {
	tool := mcp.NewTool("not_audited")
	tool.Annotations.ReadOnlyHint = nil
	_, err := NewPolicy([]server.ServerTool{{Tool: tool}})
	require.ErrorContains(t, err, "no explicit read-only classification")
}

func TestStreamableHTTPReadOnlyDiscoveryAndDirectCallGuard(t *testing.T) {
	readCalls := 0
	mutationCalls := 0
	readOnlyTool := classifiedTool("get_thing", true)
	readOnlyTool.Handler = func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		readCalls++
		return mcp.NewToolResultText("read"), nil
	}
	mutatingTool := classifiedTool("delete_thing", false)
	mutatingTool.Handler = func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		mutationCalls++
		return mcp.NewToolResultText("mutated"), nil
	}
	policy, err := NewPolicy([]server.ServerTool{readOnlyTool, mutatingTool})
	require.NoError(t, err)
	mcpServer := server.NewMCPServer("test", "1.0",
		server.WithToolFilter(policy.Filter),
		server.WithToolHandlerMiddleware(policy.Middleware),
	)
	mcpServer.AddTools(readOnlyTool, mutatingTool)
	handler := NewHTTPGuard().Middleware(server.NewStreamableHTTPServer(mcpServer))

	initialize := postMCPRequest(t, `{
		"jsonrpc":"2.0","id":1,"method":"initialize","params":{
			"protocolVersion":"2025-03-26","clientInfo":{"name":"test","version":"1.0"}
		}}`, "", "true")
	initializeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(initializeRecorder, initialize)
	require.Equal(t, http.StatusOK, initializeRecorder.Code)
	sessionID := initializeRecorder.Header().Get("Mcp-Session-Id")
	require.NotEmpty(t, sessionID)

	list := postMCPRequest(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`, sessionID, "true")
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, list)
	require.Equal(t, http.StatusOK, listRecorder.Code)
	var listResponse struct {
		Result struct {
			Tools []mcp.Tool `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(listRecorder.Body.Bytes(), &listResponse))
	require.Equal(t, []string{"get_thing"}, toolNames(listResponse.Result.Tools))

	call := postMCPRequest(t, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"delete_thing","arguments":{}}}`, sessionID, "true")
	callRecorder := httptest.NewRecorder()
	handler.ServeHTTP(callRecorder, call)
	require.Equal(t, http.StatusOK, callRecorder.Code)
	var callResponse struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(callRecorder.Body.Bytes(), &callResponse))
	require.True(t, callResponse.Result.IsError)
	require.Equal(t, PermissionDeniedMessage, callResponse.Result.Content[0].Text)
	require.Zero(t, mutationCalls)
	require.Zero(t, readCalls)
}

func classifiedTool(name string, readOnly bool) server.ServerTool {
	return server.ServerTool{Tool: mcp.NewTool(name, mcp.WithToolAnnotation(mcp.ToolAnnotation{
		ReadOnlyHint: &readOnly,
	}))}
}

func toolNames(tools []mcp.Tool) []string {
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name
	}
	return names
}

func callRequest(name string) mcp.CallToolRequest {
	request := mcp.CallToolRequest{}
	request.Params.Name = name
	return request
}

func postMCPRequest(t *testing.T, body, sessionID, readOnlyHeader string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
	if readOnlyHeader != "" {
		request.Header.Set(HeaderName, readOnlyHeader)
	}
	return request
}
