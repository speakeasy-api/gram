package platformmcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShadowInventoryToolsAreExternalOnlyWithStableUnavailableDescriptors(t *testing.T) {
	t.Parallel()

	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})

	external := registrar.For(AudienceExternal)
	assistant := registrar.For(AudienceAssistant)
	for _, name := range []string{"list_shadow_mcp_inventory", "get_shadow_mcp_review"} {
		require.Contains(t, names(external), name)
		require.NotContains(t, names(assistant), name)
	}
}
