package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
)

func TestUpdateMcpServer_EnableEndpointlessDirectRemoteSkipsAdmission(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, remoteID := createDisabledRemoteServer(t, ctx, ti, *authCtx.ProjectID, "Endpointless direct remote")
	seedBlockedDirectRemoteDistribution(t, ctx, ti, uuid.MustParse(created.ID))
	plugin, err := pluginsrepo.New(ti.conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID: created.ID, RemoteMcpServerID: &remoteID, Visibility: types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	require.Equal(t, types.McpServerVisibility("private"), updated.Visibility)
	servers, err := pluginsrepo.New(ti.conn).ListPluginServers(ctx, plugin.ID)
	require.NoError(t, err)
	require.Empty(t, servers)
}

func TestUpdateMcpServer_EnablePublishableDirectRemoteRequiresAdmission(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, remoteID := createDisabledRemoteServer(t, ctx, ti, *authCtx.ProjectID, "Publishable direct remote")
	seedBlockedDirectRemoteDistribution(t, ctx, ti, uuid.MustParse(created.ID))
	seedEndpointFor(t, ctx, ti.conn, *authCtx.ProjectID, created.ID)
	plugin, err := pluginsrepo.New(ti.conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID: created.ID, RemoteMcpServerID: &remoteID, Visibility: types.McpServerVisibility("private"),
	})
	require.ErrorIs(t, err, admission.ErrDistributionDisabled)
	requireOopsCode(t, err, oops.CodeConflict)
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID: uuid.MustParse(created.ID), ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, "disabled", server.Visibility)
	servers, err := pluginsrepo.New(ti.conn).ListPluginServers(ctx, plugin.ID)
	require.NoError(t, err)
	require.Empty(t, servers)
}
