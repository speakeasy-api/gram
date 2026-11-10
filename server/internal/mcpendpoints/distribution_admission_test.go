package mcpendpoints_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func TestCreateMcpEndpoint_DisabledDirectRemoteSkipsAdmission(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	seedBlockedDirectRemoteDistribution(t, ctx, ti, serverID)
	plugin, err := pluginsrepo.New(ti.conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	id := serverID.String()

	endpoint, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		McpServerID: &id, Slug: types.McpEndpointSlug(authCtx.OrganizationSlug + "-disabled-direct-remote"),
	})
	require.NoError(t, err)
	require.NotEmpty(t, endpoint.ID)
	servers, err := pluginsrepo.New(ti.conn).ListPluginServers(ctx, plugin.ID)
	require.NoError(t, err)
	require.Empty(t, servers)
}
