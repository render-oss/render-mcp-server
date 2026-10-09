package postgres

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/render-oss/render-mcp-server/pkg/client"
	pgclient "github.com/render-oss/render-mcp-server/pkg/client/postgres"
	"github.com/render-oss/render-mcp-server/pkg/fakes"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/render-oss/render-mcp-server/pkg/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreatePostgresToolSchemaDefault(t *testing.T) {
	fakeClient := &fakes.FakePostgresRepoClient{}
	repo := NewRepo(fakeClient)
	tool := createPostgres(repo)

	planProp := tool.Tool.InputSchema.Properties["plan"].(map[string]any)
	assert.Equal(t, "free", planProp["default"])
}

func TestCreatePostgresTool(t *testing.T) {
	ownerId := "own-123456"
	dbName := "test-database"

	tests := []struct {
		name         string
		plan         *string
		expectedPlan pgclient.PostgresPlans
	}{
		{
			name:         "Create postgres with no plan defaults to free",
			plan:         nil,
			expectedPlan: pgclient.Free,
		},
		{
			name:         "Create postgres with free plan",
			plan:         pointers.From("free"),
			expectedPlan: pgclient.Free,
		},
		{
			name:         "Create postgres with basic_256mb plan",
			plan:         pointers.From("basic_256mb"),
			expectedPlan: pgclient.Basic256mb,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakePostgresRepoClient{}
			repo := NewRepo(fakeClient)

			fakeClient.CreatePostgresWithResponseReturns(&client.CreatePostgresResponse{
				JSON201: &client.PostgresDetail{
					Id:   "pg-123",
					Name: dbName,
				},
				HTTPResponse: &http.Response{
					StatusCode: 201,
				},
			}, nil)

			ctx := createTestContext(t, ownerId)

			args := map[string]any{
				"name": dbName,
			}
			if tt.plan != nil {
				args["plan"] = *tt.plan
			}
			request := mcp.CallToolRequest{}
			request.Params.Arguments = args

			tool := createPostgres(repo)
			result, err := tool.Handler(ctx, request)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.False(t, result.IsError, "expected no error but got: %v", result.Content)

			assert.Equal(t, 1, fakeClient.CreatePostgresWithResponseCallCount())
			_, requestBody, _ := fakeClient.CreatePostgresWithResponseArgsForCall(0)
			assert.Equal(t, dbName, requestBody.Name)
			assert.Equal(t, ownerId, requestBody.OwnerId)
			assert.Equal(t, tt.expectedPlan, requestBody.Plan)
		})
	}
}

func TestQueryPostgresToolIPAllowList(t *testing.T) {
	office := client.CidrBlockAndDescription{CidrBlock: "203.0.113.0/24", Description: "office"}
	anyIP := client.CidrBlockAndDescription{CidrBlock: "0.0.0.0/0", Description: "everywhere"}

	tests := []struct {
		name        string
		ipAllowList []client.CidrBlockAndDescription
		wantConnect bool
		wantHint    bool
	}{
		{
			name:        "empty allowlist skips the connection",
			ipAllowList: []client.CidrBlockAndDescription{},
		},
		{
			name:        "missing allowlist skips the connection",
			ipAllowList: nil,
		},
		{
			name:        "restricted allowlist explains a failed connection",
			ipAllowList: []client.CidrBlockAndDescription{office},
			wantConnect: true,
			wantHint:    true,
		},
		{
			name:        "open allowlist reports the connection error alone",
			ipAllowList: []client.CidrBlockAndDescription{anyIP},
			wantConnect: true,
		},
		{
			name:        "open entry among others reports the connection error alone",
			ipAllowList: []client.CidrBlockAndDescription{office, anyIP},
			wantConnect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakePostgresRepoClient{}
			fakeClient.RetrievePostgresWithResponseReturns(&client.RetrievePostgresResponse{
				JSON200:      &client.PostgresDetail{Id: "dpg-123", IpAllowList: tt.ipAllowList},
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
			}, nil)
			fakeClient.RetrievePostgresConnectionInfoWithResponseReturns(&client.RetrievePostgresConnectionInfoResponse{
				JSON200:      &client.PostgresConnectionInfo{ExternalConnectionString: droppingPostgresURL(t)},
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
			}, nil)

			request := mcp.CallToolRequest{}
			request.Params.Arguments = map[string]any{"postgresId": "dpg-123", "sql": "SELECT 1"}

			result, err := queryPostgres(NewRepo(fakeClient)).Handler(createTestContext(t, "own-123"), request)
			require.NoError(t, err)
			require.True(t, result.IsError)
			require.NotEmpty(t, result.Content)
			content, ok := result.Content[0].(mcp.TextContent)
			require.True(t, ok, "expected text content, got %T", result.Content[0])
			text := content.Text

			if !tt.wantConnect {
				assert.Equal(t, externalAccessBlockedMessage, text)
				assert.Zero(t, fakeClient.RetrievePostgresConnectionInfoWithResponseCallCount())
				return
			}
			assert.Equal(t, 1, fakeClient.RetrievePostgresConnectionInfoWithResponseCallCount())
			assert.Contains(t, text, "Error connecting to database")
			if tt.wantHint {
				assert.Contains(t, text, ipRestrictedHint)
			} else {
				assert.NotContains(t, text, "allowlist")
			}
		})
	}
}

// droppingPostgresURL returns a connection string for a local server that closes every connection it accepts.
func droppingPostgresURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return fmt.Sprintf("postgresql://user:pass@%s/db?sslmode=disable", listener.Addr())
}

func createTestContext(t *testing.T, workspaceID string) context.Context {
	t.Helper()
	t.Setenv("RENDER_CONFIG_PATH", filepath.Join(t.TempDir(), "mcp-server.yaml"))
	ctx := session.ContextWithStdioSession(context.Background())
	sess := session.FromContext(ctx)
	sess.SetWorkspace(ctx, workspaceID)
	return ctx
}

// TestCreatePostgresToolPlanEnumIsAccepted pins the create_postgres plan
// parameter to the generated enum. Advertising a plan validate.PostgresPlan
// rejects is an error an MCP client only discovers by calling the tool, and
// requiring a spec-based name catches a schema that has been pinned to a
// hardcoded list and so no longer picks up newly added plans.
func TestCreatePostgresToolPlanEnumIsAccepted(t *testing.T) {
	repo := NewRepo(&fakes.FakePostgresRepoClient{})
	tool := createPostgres(repo)

	planProp, ok := tool.Tool.InputSchema.Properties["plan"].(map[string]any)
	require.True(t, ok, "create_postgres has no plan property")
	plans, ok := planProp["enum"].([]string)
	require.True(t, ok, "create_postgres plan property has no string enum")

	for _, plan := range plans {
		_, err := validate.PostgresPlan(plan)
		assert.NoError(t, err, "advertised plan %q is rejected by validate.PostgresPlan", plan)
	}

	specBased := regexp.MustCompile(`^[0-9]+(\.[0-9]+)?c-[0-9]+(mb|g)$`)
	assert.True(t, slices.ContainsFunc(plans, specBased.MatchString),
		"no spec-based plan name advertised, got %v", plans)
}

// TestParsePostgresConnConfigEnforcesTLS locks require-TLS semantics for
// Render-managed Postgres (issue #6). pgx defaults to sslmode=prefer, so a
// connection string without an explicit sslmode parses to a TLS primary plus
// a plaintext fallback; any TLS hiccup then downgrades to the unencrypted
// fallback that Render rejects with FATAL: SSL/TLS required. The helper must
// leave no plaintext path: primary TLS present and every fallback TLS-only.
func TestParsePostgresConnConfigEnforcesTLS(t *testing.T) {
	cases := []struct {
		name    string
		connStr string
	}{
		{
			name:    "no sslmode (Render default)",
			connStr: "postgres://user:pass@myhost.ohio-postgres.render.com:5432/mydb",
		},
		{
			name:    "explicit prefer",
			connStr: "postgres://user:pass@myhost.ohio-postgres.render.com:5432/mydb?sslmode=prefer",
		},
		{
			name:    "explicit require",
			connStr: "postgres://user:pass@myhost.ohio-postgres.render.com:5432/mydb?sslmode=require",
		},
		{
			name:    "explicit disable is upgraded",
			connStr: "postgres://user:pass@myhost.ohio-postgres.render.com:5432/mydb?sslmode=disable",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parsePostgresConnConfig(tc.connStr)
			require.NoError(t, err)
			require.NotNil(t, cfg.TLSConfig, "primary connection must use TLS")
			for i, fb := range cfg.Fallbacks {
				require.NotNil(t, fb, "fallback %d must not be nil", i)
				assert.NotNil(t, fb.TLSConfig, "fallback %d must use TLS, no plaintext downgrade", i)
			}
		})
	}
}
