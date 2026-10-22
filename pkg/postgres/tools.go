package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/client"
	pgclient "github.com/render-oss/render-mcp-server/pkg/client/postgres"
	"github.com/render-oss/render-mcp-server/pkg/mcpserver"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

func Tools(c *client.ClientWithResponses) []server.ServerTool {
	postgresRepo := NewRepo(c)

	return []server.ServerTool{
		listPostgresInstances(postgresRepo),
		getPostgres(postgresRepo),
		createPostgres(postgresRepo),
		queryPostgres(postgresRepo),
	}
}

func listPostgresInstances(postgresRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("list_postgres_instances",
			mcp.WithDescription("List all Postgres databases in your Render account"),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "List Postgres instances",
				ReadOnlyHint:    pointers.From(true),
				DestructiveHint: pointers.From(false),
				IdempotentHint:  pointers.From(true),
				OpenWorldHint:   pointers.From(false),
			}),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			postgres, err := postgresRepo.ListPostgres(ctx, &client.ListPostgresParams{})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			if len(postgres) == 0 {
				return mcp.NewToolResultText("No Postgres instances found"), nil
			}

			respJSON, err := json.Marshal(postgres)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(string(respJSON)), nil
		},
	}
}

func getPostgres(postgresRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("get_postgres",
			mcp.WithDescription("Retrieve a Postgres instance by ID"),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "Get Postgres instance details",
				ReadOnlyHint:    pointers.From(true),
				DestructiveHint: pointers.From(false),
				IdempotentHint:  pointers.From(true),
				OpenWorldHint:   pointers.From(false),
			}),
			mcp.WithString("postgresId",
				mcp.Required(),
				mcp.Description("The ID of the Postgres instance to retrieve"),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			postgresId, err := validate.RequiredToolParam[string](request, "postgresId")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			postgres, err := postgresRepo.GetPostgres(ctx, postgresId)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			respJSON, err := json.Marshal(postgres)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(string(respJSON)), nil
		},
	}
}

func createPostgres(postgresRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("create_postgres",
			mcp.WithDescription("Create a new Postgres instance in your Render account"),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "Create Postgres instance",
				ReadOnlyHint:    pointers.From(false),
				DestructiveHint: pointers.From(false),
				IdempotentHint:  pointers.From(false),
				OpenWorldHint:   pointers.From(true),
			}),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description("The name of the database as it will appear in the Render Dashboard"),
			),
			mcp.WithString("plan",
				mcp.Description("Pricing plan for the database"),
				mcp.Enum(mcpserver.PostgresPlanEnumValues()...),
				mcp.DefaultString(string(pgclient.Free)),
			),
			mcp.WithString("region",
				mcp.Description("Region where the database will be deployed"),
				mcp.Enum(mcpserver.RegionEnumValues()...),
			),
			mcp.WithNumber("version",
				mcp.Description("PostgreSQL version to use (e.g., 14, 15)"),
				mcp.Min(13),
				mcp.Max(18),
				mcp.DefaultNumber(18),
			),
			mcp.WithNumber("diskSizeGb",
				mcp.Description("Your database's capacity, in GB. You can increase storage at any time, but you can't decrease it. Specify 1 GB or any multiple of 5 GB."),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, err := validate.RequiredToolParam[string](request, "name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ownerId, err := session.FromContext(ctx).GetWorkspace(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			var postgresPlan pgclient.PostgresPlans
			if plan, ok, err := validate.OptionalToolParam[string](request, "plan"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				postgresPlan, err = validate.PostgresPlan(plan)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
			} else {
				postgresPlan = pgclient.Free
			}

			createParams := client.PostgresPOSTInput{
				Name:    name,
				OwnerId: ownerId,
				Plan:    postgresPlan,
			}

			if region, ok, err := validate.OptionalToolParam[string](request, "region"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				createParams.Region = (*client.Region)(&region)
			}

			if version, ok, err := validate.OptionalToolParam[float64](request, "version"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				createParams.Version = client.PostgresVersion(fmt.Sprintf("%.0f", version))
			}

			if diskSizeGb, ok, err := validate.OptionalToolParam[float64](request, "diskSizeGb"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				diskSizeGbInt := int(diskSizeGb)
				err = validate.PostgresDiskSizeGb(diskSizeGbInt)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				if postgresPlan == pgclient.Free && diskSizeGbInt > 0 {
					return mcp.NewToolResultError("Free plan does not support custom disk size"), nil
				}
				createParams.DiskSizeGB = &diskSizeGbInt
			}

			postgres, err := postgresRepo.CreatePostgres(ctx, createParams)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			respJSON, err := json.Marshal(postgres)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(string(respJSON)), nil
		},
	}
}

func queryPostgres(postgresRepo *Repo) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("query_render_postgres",
			mcp.WithDescription("Run a read-only SQL query against a Render-hosted Postgres database. "+
				"This tool creates a new connection for each query and closes it after the query completes."),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "Query Postgres",
				ReadOnlyHint:    pointers.From(true),
				DestructiveHint: pointers.From(false),
				IdempotentHint:  pointers.From(true),
				OpenWorldHint:   pointers.From(false),
			}),
			mcp.WithString("postgresId",
				mcp.Required(),
				mcp.Description("The ID of the Postgres instance to query"),
			),
			mcp.WithString("sql",
				mcp.Required(),
				mcp.Description("The SQL query to run. Note that the query will be wrapped in a read-only transaction."),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			postgresId, err := validate.RequiredToolParam[string](request, "postgresId")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			sqlQuery, err := validate.RequiredToolParam[string](request, "sql")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			postgres, err := postgresRepo.GetPostgres(ctx, postgresId)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			// An empty allowlist blocks every external connection, so don't try.
			if len(postgres.IpAllowList) == 0 {
				return mcp.NewToolResultError(externalAccessBlockedMessage), nil
			}

			connectionInfo, err := postgresRepo.GetPostgresConnectionInfo(ctx, postgresId)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			config, err := pgx.ParseConfig(connectionInfo.ExternalConnectionString)
			if err != nil {
				return mcp.NewToolResultErrorFromErr("Error parsing connection string", err), nil
			}
			conn, err := pgx.ConnectConfig(ctx, config)
			if err != nil {
				return connectErrorResult(err, postgres.IpAllowList), nil
			}
			defer conn.Close(ctx)

			// Wrap all queries in a READ ONLY transaction
			tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if err != nil {
				return mcp.NewToolResultErrorFromErr("Error beginning transaction", err), nil
			}

			// Make sure we roll back the transaction if it's not committed successfully
			defer func() {
				_ = tx.Rollback(ctx) // Ignore error from rollback, as it might already be committed/rolled back
			}()

			// Canceling queryCtx after an early stop makes pgx drop the
			// connection instead of draining the unread rows.
			queryCtx, cancelQuery := context.WithCancel(ctx)
			defer cancelQuery()
			rows, err := tx.Query(queryCtx, sqlQuery)
			if err != nil {
				return mcp.NewToolResultErrorFromErr("Error executing query", err), nil
			}
			defer rows.Close()

			// Get field descriptions
			fieldDescriptions := rows.FieldDescriptions()
			columnNames := make([]string, len(fieldDescriptions))
			for i, fd := range fieldDescriptions {
				columnNames[i] = string(fd.Name)
			}

			respJSON, rowCount, truncated, err := collectRows(rows, columnNames, maxQueryResultBytes)
			if err != nil {
				return mcp.NewToolResultErrorFromErr("Error reading query results", err), nil
			}
			result := mcp.NewToolResultText(string(respJSON))
			if truncated {
				cancelQuery()
				result.Content = append(result.Content, mcp.NewTextContent(truncatedResultMessage(rowCount)))
			}
			return result, nil
		},
	}
}

// maxQueryResultBytes bounds the JSON that query_render_postgres holds in
// memory. Without it, one large SELECT can exhaust the hosted server's memory
// and crash it for every user. 5 MB is already more than a model can read.
const maxQueryResultBytes = 5 << 20

// rowSource is the part of pgx.Rows that collectRows reads.
type rowSource interface {
	Next() bool
	Values() ([]any, error)
	Err() error
}

// collectRows encodes rows as a JSON array of objects keyed by column name.
// It stops before the array would exceed maxBytes bytes, and reports how many
// rows it kept and whether it stopped early.
func collectRows(rows rowSource, columnNames []string, maxBytes int) (out []byte, rowCount int, truncated bool, err error) {
	out = []byte{'['}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, 0, false, fmt.Errorf("read row values: %w", err)
		}

		rowMap := make(map[string]any, len(columnNames))
		for i, col := range columnNames {
			// Handle pgx types appropriately
			switch v := values[i].(type) {
			case []byte:
				// Convert byte arrays to string
				rowMap[col] = string(v)
			default:
				// For other types, use as-is
				rowMap[col] = v
			}
		}
		rowJSON, err := json.Marshal(rowMap)
		if err != nil {
			return nil, 0, false, fmt.Errorf("marshal row: %w", err)
		}

		// One byte for the separating comma, one for the closing bracket.
		if len(out)+len(rowJSON)+2 > maxBytes {
			return append(out, ']'), rowCount, true, nil
		}
		if rowCount > 0 {
			out = append(out, ',')
		}
		out = append(out, rowJSON...)
		rowCount++
	}
	if err := rows.Err(); err != nil {
		return nil, 0, false, fmt.Errorf("iterate rows: %w", err)
	}
	return append(out, ']'), rowCount, false, nil
}

func truncatedResultMessage(rowCount int) string {
	// Paging can't help when one row is over the limit on its own.
	if rowCount == 0 {
		return fmt.Sprintf("Results truncated: the first row alone is over %d MB of JSON, so no rows are shown. "+
			"Select fewer columns, or trim large values in SQL, for example with left(column, 1000).",
			maxQueryResultBytes>>20)
	}
	// Without ORDER BY, Postgres can return rows in a different order on
	// each run, so OFFSET pages could skip or repeat rows.
	return fmt.Sprintf("Results truncated: showing the first %d rows. "+
		"This tool returns at most %d MB of JSON per query. "+
		"To see the rest, rerun the query with an ORDER BY plus LIMIT and OFFSET, select fewer columns, or aggregate in SQL.",
		rowCount, maxQueryResultBytes>>20)
}

const (
	allowlistAdvice = "To query it, add the IP address you connect from to the database's IP allowlist " +
		"in the Networking section of its page in the Render Dashboard. " +
		"If you're using the hosted Render MCP server, it doesn't have IP addresses you can allowlist yet, " +
		"so run the Render MCP server locally instead."
	externalAccessBlockedMessage = "This database blocks all external connections because its IP allowlist is empty, " +
		"so this tool can't reach it. " + allowlistAdvice
	ipRestrictedHint = "This database only accepts external connections from IP addresses on its allowlist, " +
		"which may not include the address this MCP server connects from. " + allowlistAdvice
)

// connectErrorResult reports a failed connection, adding allowlist advice unless the database accepts any IP.
func connectErrorResult(err error, allowList []client.CidrBlockAndDescription) *mcp.CallToolResult {
	for _, block := range allowList {
		if block.CidrBlock == "0.0.0.0/0" {
			return mcp.NewToolResultErrorFromErr("Error connecting to database", err)
		}
	}
	return mcp.NewToolResultError(fmt.Sprintf("Error connecting to database: %v\n\n%s", err, ipRestrictedHint))
}
