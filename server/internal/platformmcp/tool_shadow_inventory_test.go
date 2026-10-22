package platformmcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

func TestShadowInventoryToolsAreExternalOnlyWithStableUnavailableDescriptors(t *testing.T) {
	t.Parallel()

	_, registrar := newServer(aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())), nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})

	external := registrar.For(AudienceExternal)
	assistant := registrar.For(AudienceAssistant)
	for _, name := range []string{"list_shadow_mcp_inventory", "get_shadow_mcp_review"} {
		require.Contains(t, names(external), name)
		require.NotContains(t, names(assistant), name)
	}
}
