package remotemcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Every remote construction goes through Build or a BuildTarget without a
// policy option, and both must select the hardened remote policy. Only an
// explicit WithHeaderPolicy selects another.
func TestProxyManagerSelectsRemoteHeaderPolicyByDefault(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	manager := NewProxyManager(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	server := &remotemcprepo.RemoteMcpServer{ID: uuid.New(), Url: "https://example.com/mcp"}

	for _, visibility := range []string{mcpservers.VisibilityPrivate, mcpservers.VisibilityPublic} {
		built := manager.Build(logger, server, uuid.NewString(), nil, visibility, "organization-id", "project-id", "", "", nil)
		require.Equal(t, proxy.HeaderPolicyRemote, built.HeaderPolicy)

		built = manager.Build(logger, server, uuid.NewString(), nil, visibility, "organization-id", "project-id", "", "", nil, WithoutToolsCallIdentityCoverage(), WithMetaMCPServerID(uuid.NewString()))
		require.Equal(t, proxy.HeaderPolicyRemote, built.HeaderPolicy)

		built = manager.BuildTarget(logger, proxy.ServerIdentity{RemoteMCPServerID: server.ID.String(), McpServerID: uuid.NewString()}, server.Url, nil, visibility, "organization-id", "project-id", "", "", nil)
		require.Equal(t, proxy.HeaderPolicyRemote, built.HeaderPolicy)
	}

	built := manager.BuildTarget(logger, proxy.ServerIdentity{TunneledMCPServerID: uuid.NewString(), McpServerID: uuid.NewString()}, "https://tunnel.example.com/mcp", nil, mcpservers.VisibilityPrivate, "organization-id", "project-id", "", "", nil, WithHeaderPolicy(proxy.HeaderPolicyTunneled))
	require.Equal(t, proxy.HeaderPolicyTunneled, built.HeaderPolicy)
}
