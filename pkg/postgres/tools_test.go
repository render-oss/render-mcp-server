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

// fakeRows is a rowSource over fixed values.
type fakeRows struct {
	values  [][]any
	next    int
	valsErr error
	iterErr error
}

func (f *fakeRows) Next() bool {
	if f.next >= len(f.values) {
		return false
	}
	f.next++
	return true
}

func (f *fakeRows) Values() ([]any, error) {
	if f.valsErr != nil {
		return nil, f.valsErr
	}
	return f.values[f.next-1], nil
}

func (f *fakeRows) Err() error { return f.iterErr }

func idRows(n int) [][]any {
	rows := make([][]any, n)
	for i := range rows {
		rows[i] = []any{i}
	}
	return rows
}

func TestCollectRows(t *testing.T) {
	// `{"id":N}` for one digit N is 8 bytes.
	tests := []struct {
		name          string
		rows          [][]any
		maxBytes      int
		wantJSON      string
		wantCount     int
		wantTruncated bool
	}{
		{
			name:     "empty result is an empty array",
			maxBytes: 1 << 20,
			wantJSON: `[]`,
		},
		{
			name:      "rows within the limit are all returned",
			rows:      idRows(3),
			maxBytes:  1 << 20,
			wantJSON:  `[{"id":0},{"id":1},{"id":2}]`,
			wantCount: 3,
		},
		{
			name:      "row count alone does not truncate",
			rows:      idRows(5000),
			maxBytes:  1 << 20,
			wantCount: 5000,
		},
		{
			name:      "result of exactly maxBytes fits",
			rows:      idRows(2),
			maxBytes:  len(`[{"id":0},{"id":1}]`),
			wantJSON:  `[{"id":0},{"id":1}]`,
			wantCount: 2,
		},
		{
			name:          "byte limit stops before the row that would exceed it",
			rows:          idRows(3),
			maxBytes:      len(`[{"id":0},{"id":1}]`),
			wantJSON:      `[{"id":0},{"id":1}]`,
			wantCount:     2,
			wantTruncated: true,
		},
		{
			name:          "one row over the byte limit returns no rows",
			rows:          [][]any{{"a long value"}},
			maxBytes:      8,
			wantJSON:      `[]`,
			wantTruncated: true,
		},
		{
			name:      "byte slices become strings",
			rows:      [][]any{{[]byte("abc")}},
			maxBytes:  1 << 20,
			wantJSON:  `[{"id":"abc"}]`,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, count, truncated, err := collectRows(&fakeRows{values: tt.rows}, []string{"id"}, tt.maxBytes)
			require.NoError(t, err)
			if tt.wantJSON != "" {
				assert.Equal(t, tt.wantJSON, string(out))
			}
			assert.LessOrEqual(t, len(out), tt.maxBytes)
			assert.Equal(t, tt.wantCount, count)
			assert.Equal(t, tt.wantTruncated, truncated)
		})
	}
}

func TestCollectRowsErrors(t *testing.T) {
	t.Run("row values error", func(t *testing.T) {
		_, _, _, err := collectRows(&fakeRows{values: idRows(1), valsErr: fmt.Errorf("bad row")}, []string{"id"}, 1<<20)
		require.ErrorContains(t, err, "bad row")
	})
	t.Run("iteration error", func(t *testing.T) {
		_, _, _, err := collectRows(&fakeRows{values: idRows(1), iterErr: fmt.Errorf("conn lost")}, []string{"id"}, 1<<20)
		require.ErrorContains(t, err, "conn lost")
	})
}

func TestTruncatedResultMessage(t *testing.T) {
	t.Run("rows shown asks for an ordered rerun", func(t *testing.T) {
		msg := truncatedResultMessage(23)
		assert.Contains(t, msg, "first 23 rows")
		assert.Contains(t, msg, "ORDER BY plus LIMIT and OFFSET")
	})
	t.Run("no rows shown explains the first row is too large", func(t *testing.T) {
		msg := truncatedResultMessage(0)
		assert.Contains(t, msg, "first row alone is over 5 MB")
		assert.Contains(t, msg, "left(column, 1000)")
		assert.NotContains(t, msg, "OFFSET", "paging can't help when one row is over the limit")
	})
}
