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
	"github.com/render-oss/render-mcp-server/pkg/config"
	"github.com/render-oss/render-mcp-server/pkg/mcpserver"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
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
				Title:          "List Postgres instances",
				ReadOnlyHint:   pointers.From(true),
				IdempotentHint: pointers.From(true),
				OpenWorldHint:  pointers.From(true),
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
				Title:          "Get Postgres instance details",
				ReadOnlyHint:   pointers.From(true),
				IdempotentHint: pointers.From(true),
				OpenWorldHint:  pointers.From(true),
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
				Title:          "Create Postgres instance",
				ReadOnlyHint:   pointers.From(false),
				IdempotentHint: pointers.From(false),
				OpenWorldHint:  pointers.From(true),
			}),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description("The name of the database as it will appear in the Render Dashboard"),
			),
			mcp.WithString("plan",
				mcp.Required(),
				mcp.Description("Pricing plan for the database"),
				mcp.Enum(mcpserver.PostgresPlanEnumValues()...),
			),
			mcp.WithString("region",
				mcp.Description("Region where the database will be deployed"),
				mcp.Enum(mcpserver.RegionEnumValues()...),
			),
			mcp.WithNumber("version",
				mcp.Description("PostgreSQL version to use (e.g., 14, 15)"),
				mcp.Min(12),
				mcp.Max(16),
				mcp.DefaultNumber(16),
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

			ownerId, err := config.WorkspaceID()
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			plan, err := validate.RequiredToolParam[string](request, "plan")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			postgresPlan, err := validate.PostgresPlan(plan)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			createParams := client.PostgresPOSTInput{
				Name:    name,
				OwnerId: ownerId,
				Plan:    postgresPlan,
			}

			if region, ok, err := validate.OptionalToolParam[string](request, "region"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				createParams.Region = &region
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
				Title:          "Query Postgres",
				ReadOnlyHint:   pointers.From(true),
				IdempotentHint: pointers.From(true),
				OpenWorldHint:  pointers.From(true),
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

			connectionInfo, err := postgresRepo.GetPostgresConnectionInfo(ctx, postgresId)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			config, err := pgx.ParseConfig(connectionInfo.ExternalConnectionString)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error parsing connection string: %s", err.Error())), nil
			}
			conn, err := pgx.ConnectConfig(ctx, config)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error connecting to database: %s", err.Error())), nil
			}
			defer conn.Close(ctx)

			// Wrap all queries in a READ ONLY transaction
			tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error beginning transaction: %s", err.Error())), nil
			}

			// Make sure we roll back the transaction if it's not committed successfully
			defer func() {
				_ = tx.Rollback(ctx) // Ignore error from rollback, as it might already be committed/rolled back
			}()

			rows, err := tx.Query(ctx, sqlQuery)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error executing query: %s", err.Error())), nil
			}
			defer rows.Close()

			// Get field descriptions
			fieldDescriptions := rows.FieldDescriptions()
			columnNames := make([]string, len(fieldDescriptions))
			for i, fd := range fieldDescriptions {
				columnNames[i] = string(fd.Name)
			}

			results := []map[string]interface{}{}

			for rows.Next() {
				values, err := rows.Values()
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Error reading row values: %s", err.Error())), nil
				}

				rowMap := make(map[string]interface{})
				for i, col := range columnNames {
					// Handle pgx types appropriately
					val := values[i]
					switch v := val.(type) {
					case []byte:
						// Convert byte arrays to string
						rowMap[col] = string(v)
					default:
						// For other types, use as-is
						rowMap[col] = v
					}
				}
				results = append(results, rowMap)
			}

			// Check for any errors encountered during iteration
			if err := rows.Err(); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error iterating rows: %s", err.Error())), nil
			}

			respJSON, err := json.Marshal(results)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error marshaling results: %s", err.Error())), nil
			}

			return mcp.NewToolResultText(string(respJSON)), nil
		},
	}
}
