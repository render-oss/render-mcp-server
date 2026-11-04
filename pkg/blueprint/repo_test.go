package blueprint

import (
	"testing"

	"github.com/render-oss/render-mcp-server/pkg/client"
	blueprintclient "github.com/render-oss/render-mcp-server/pkg/client/blueprints"
	"github.com/render-oss/render-mcp-server/pkg/fakes"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateBlueprintWorkspaceMismatch(t *testing.T) {
	fakeClient := &fakes.FakeBlueprintRepoClient{}
	ctx := session.ContextWithWorkspace(t.Context(), "tea-selected")
	_, err := NewRepo(fakeClient).CreateBlueprint(ctx, client.CreateBlueprintJSONRequestBody{
		OwnerId: "tea-other",
		Name:    "my-app",
		Source:  blueprintclient.BlueprintSource{Repo: "https://github.com/example/my-app"},
	})
	require.ErrorContains(t, err, "belongs to workspace tea-other")
	assert.Zero(t, fakeClient.CreateBlueprintWithResponseCallCount())
}

func TestRetrieveBlueprintAndCheckWorkspace(t *testing.T) {
	t.Run("returns Blueprint when workspace matches", func(t *testing.T) {
		repo := NewRepo(newTestBlueprintClient())
		ctx := session.ContextWithWorkspace(t.Context(), "tea-test")

		blueprint, err := repo.RetrieveBlueprintAndCheckWorkspace(ctx, "exs-test")

		require.NoError(t, err)
		require.NotNil(t, blueprint)
		assert.Equal(t, "exs-test", blueprint.Id)
		assert.Equal(t, "tea-test", blueprint.OwnerId)
	})

	t.Run("rejects mismatched workspace", func(t *testing.T) {
		repo := NewRepo(newTestBlueprintClient())
		ctx := session.ContextWithWorkspace(t.Context(), "tea-other")

		blueprint, err := repo.RetrieveBlueprintAndCheckWorkspace(ctx, "exs-test")

		require.ErrorContains(t, err, "belongs to workspace tea-test")
		assert.ErrorContains(t, err, "targets workspace tea-other")
		assert.Nil(t, blueprint)
	})
}

func TestCheckBlueprintWorkspace(t *testing.T) {
	t.Run("accepts matching workspace", func(t *testing.T) {
		repo := NewRepo(newTestBlueprintClient())
		ctx := session.ContextWithWorkspace(t.Context(), "tea-test")

		err := repo.CheckBlueprintWorkspace(ctx, "exs-test")

		require.NoError(t, err)
	})

	t.Run("rejects mismatched workspace", func(t *testing.T) {
		repo := NewRepo(newTestBlueprintClient())
		ctx := session.ContextWithWorkspace(t.Context(), "tea-other")

		err := repo.CheckBlueprintWorkspace(ctx, "exs-test")

		require.ErrorContains(t, err, "belongs to workspace tea-test")
		assert.ErrorContains(t, err, "targets workspace tea-other")
	})
}
