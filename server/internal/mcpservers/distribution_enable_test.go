package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
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
