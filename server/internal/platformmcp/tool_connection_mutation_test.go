package platformmcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestMCPConnectionMutationToolsHaveSameInputSchemaWhenUnavailable(t *testing.T) {
	t.Parallel()

	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "connection-mutation-live", Version: "0.0.1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "connection-mutation-unavailable", Version: "0.0.1"}, nil))
	registerMCPConnectionMutationTools(live, &MCPConnectionMutationService{})
	registerMCPConnectionMutationTools(unavailable, nil)

	for _, name := range []string{setMCPAddressToolName, setMCPNetworkAccessToolName} {
		liveDescriptor := descriptorByName(t, live, name)
		unavailableDescriptor := descriptorByName(t, unavailable, name)
		require.JSONEq(t, string(liveDescriptor.InputSchema), string(unavailableDescriptor.InputSchema), name)
		require.Equal(t, liveDescriptor.Meta, unavailableDescriptor.Meta, name)
		require.Equal(t, externalOnly, liveDescriptor.Meta.Audiences, name)
		require.Equal(t, ProjectScopeExplicit, liveDescriptor.Meta.ProjectScope, name)
		require.NotEmpty(t, liveDescriptor.InputSchema, name)
		require.Contains(t, liveDescriptor.Description, "requests publication", name)
		require.Contains(t, liveDescriptor.Description, "not that publication", name)
		require.Contains(t, unavailableDescriptor.Description, "requests publication", name)
	}
}
