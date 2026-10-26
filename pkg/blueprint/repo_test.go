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
