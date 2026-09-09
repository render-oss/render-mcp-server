package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateReadOnlySQLAllowsSingleReadStatement(t *testing.T) {
	queries := []string{
		"SELECT id, name FROM services",
		"WITH active AS (SELECT id FROM services) SELECT * FROM active; -- trailing comment",
		"SHOW transaction_read_only",
		"VALUES (1), (2)",
		"TABLE services",
		"SELECT 'delete; update', $$insert; drop$$, \"update\" FROM services",
	}

	for _, query := range queries {
		t.Run(query, func(t *testing.T) {
			require.NoError(t, validateReadOnlySQL(query))
		})
	}
}

func TestValidateReadOnlySQLRejectsWritesAndMultipleStatements(t *testing.T) {
	tests := map[string]string{
		"direct insert":         "INSERT INTO services(id) VALUES (1)",
		"direct update":         "UPDATE services SET name = 'changed'",
		"direct delete":         "DELETE FROM services",
		"writable CTE":          "WITH changed AS (UPDATE services SET name = 'x' RETURNING *) SELECT * FROM changed",
		"select into":           "SELECT * INTO archived_services FROM services",
		"sequence mutation":     "SELECT nextval('service_ids')",
		"advisory lock":         "SELECT pg_advisory_lock(42)",
		"row lock":              "SELECT * FROM services FOR UPDATE",
		"backend termination":   "SELECT pg_terminate_backend(42)",
		"configuration change":  "SELECT set_config('work_mem', '1GB', false)",
		"multiple statements":   "SELECT 1; DELETE FROM services",
		"double terminator":     "SELECT 1;;",
		"unterminated string":   "SELECT 'unfinished",
		"unterminated comment":  "SELECT 1 /* unfinished",
		"unterminated dollar":   "SELECT $body$unfinished",
		"transaction statement": "BEGIN READ ONLY",
	}

	for name, query := range tests {
		t.Run(name, func(t *testing.T) {
			require.Error(t, validateReadOnlySQL(query))
		})
	}
}
