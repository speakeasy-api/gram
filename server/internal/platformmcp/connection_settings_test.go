package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

func TestMCPConnectionSettingsToolHasSameInputSchemaWhenUnavailable(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "settings-live", Version: "0.0.1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "settings-unavailable", Version: "0.0.1"}, nil))
	registerMCPConnectionSettingsTool(live, nil)
	registerUnavailableMCPConnectionSettingsTool(unavailable)
	require.JSONEq(t, string(live.Descriptors()[0].InputSchema), string(unavailable.Descriptors()[0].InputSchema))
	require.Equal(t, live.Descriptors()[0].Meta, unavailable.Descriptors()[0].Meta)
}

func TestGetMCPConnectionSettingsScopesExactTargetToProjectAndOrganization(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_connection_settings")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := NewMCPConnectionSettingsService(conn)

	servers, err := platformrepo.New(conn).ListPlatformMCPServers(ctx, platformrepo.ListPlatformMCPServersParams{
		ProjectID: project.ID, OrganizationID: principal.OrganizationID, LimitValue: 10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, servers)
	serverID := servers[0].ID.String()

	got, err := service.Get(ctx, principal, GetMCPConnectionSettingsInput{
		ProjectID: project.ID.String(), TargetKind: MCPConnectionSettingsMCPServer, TargetID: serverID,
	})
	require.NoError(t, err)
	require.Equal(t, project.ID.String(), got.ProjectID)
	require.Equal(t, serverID, got.TargetID)
	require.Equal(t, "public_only", got.NetworkMode)

	_, err = service.Get(ctx, principal, GetMCPConnectionSettingsInput{
		ProjectID: project.ID.String(), TargetKind: MCPConnectionSettingsGateway, TargetID: serverID,
	})
	require.ErrorIs(t, err, ErrMCPConnectionSettingsNotFound)

	foreign := principal
	foreign.OrganizationID = "other-org-" + uuid.NewString()
	_, err = service.Get(ctx, foreign, GetMCPConnectionSettingsInput{
		ProjectID: project.ID.String(), TargetKind: MCPConnectionSettingsMCPServer, TargetID: serverID,
	})
	require.ErrorIs(t, err, ErrMCPConnectionSettingsNotFound)
}
