package validate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/render-oss/render-mcp-server/pkg/validate"
)

func TestServicePlan(t *testing.T) {
	cases := []struct {
		plan  string
		valid bool
	}{
		{"free", true}, // free is a valid service plan despite not being a PaidPlan enum member
		{"starter", true},
		{"pro_ultra", true},
		{"bogus", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.plan, func(t *testing.T) {
			got, err := validate.ServicePlan(tc.plan)
			if !tc.valid {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid service plan")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tc.plan, string(*got))
		})
	}
}

func TestKeyValuePlan(t *testing.T) {
	for _, plan := range []string{"free", "starter", "standard", "pro", "pro_plus"} {
		t.Run("valid/"+plan, func(t *testing.T) {
			got, err := validate.KeyValuePlan(plan)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, plan, string(*got))
		})
	}

	t.Run("custom rejected with dashboard hint", func(t *testing.T) {
		_, err := validate.KeyValuePlan("custom")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "custom Key Value plans")
	})

	t.Run("unknown rejected", func(t *testing.T) {
		_, err := validate.KeyValuePlan("bogus")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Key Value plan")
	})
}

func TestPostgresPlan(t *testing.T) {
	for _, plan := range []string{"free", "basic_256mb", "pro_4gb", "accelerated_1024gb"} {
		t.Run("valid/"+plan, func(t *testing.T) {
			got, err := validate.PostgresPlan(plan)
			require.NoError(t, err)
			assert.Equal(t, plan, string(got))
		})
	}

	// Legacy tier names remain valid PostgresPlans enum members but the MCP
	// server must keep rejecting them as ordinary invalid plans (not custom).
	for _, plan := range []string{"starter", "standard", "pro", "pro_plus"} {
		t.Run("legacy rejected/"+plan, func(t *testing.T) {
			_, err := validate.PostgresPlan(plan)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid Postgres plan")
		})
	}

	t.Run("custom rejected with dashboard hint", func(t *testing.T) {
		_, err := validate.PostgresPlan("custom")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "custom Postgres plans")
	})

	t.Run("unknown rejected", func(t *testing.T) {
		_, err := validate.PostgresPlan("bogus")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Postgres plan")
	})
}
