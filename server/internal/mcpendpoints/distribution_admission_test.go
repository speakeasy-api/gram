package mcpendpoints_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	endpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
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

func TestCreateMcpEndpoint_EnabledDirectRemoteRequiresAdmission(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	serverID := seedMcpServerWithVisibility(t, ctx, ti.conn, *authCtx.ProjectID, "private")
	seedBlockedDirectRemoteDistribution(t, ctx, ti, serverID)
	plugin, err := pluginsrepo.New(ti.conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	id := serverID.String()

	_, err = ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		McpServerID: &id, Slug: types.McpEndpointSlug(authCtx.OrganizationSlug + "-enabled-direct-remote"),
	})
	require.ErrorIs(t, err, admission.ErrDistributionDisabled)
	requireOopsCode(t, err, oops.CodeConflict)
	endpoints, err := endpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, endpointsrepo.ListMCPEndpointsByMCPServerIDParams{
		ProjectID: *authCtx.ProjectID, McpServerID: serverID,
	})
	require.NoError(t, err)
	require.Empty(t, endpoints)
	servers, err := pluginsrepo.New(ti.conn).ListPluginServers(ctx, plugin.ID)
	require.NoError(t, err)
	require.Empty(t, servers)
}
