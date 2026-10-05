package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

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
	ctx = authz.GrantsToContext(ctx, []authz.Grant{
		authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID),
		authz.NewGrant(authz.ScopeMCPRead, serverID),
		authz.NewGrant(authz.ScopeProjectRead, project.ID.String()),
	})

	got, err := service.Get(ctx, principal, GetMCPConnectionSettingsInput{
		ProjectID: project.ID.String(), TargetKind: MCPConnectionSettingsMCPServer, TargetID: serverID,
	})
	require.NoError(t, err)
	require.Equal(t, project.ID.String(), got.ProjectID)
	require.Equal(t, serverID, got.TargetID)
	require.Equal(t, "public_only", got.NetworkMode)
	require.Len(t, got.Version, 64)
	again, err := service.Get(ctx, principal, GetMCPConnectionSettingsInput{
		ProjectID: project.ID.String(), TargetKind: MCPConnectionSettingsMCPServer, TargetID: serverID,
	})
	require.NoError(t, err)
	require.Equal(t, got.Version, again.Version)

	_, err = service.Get(authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID)}), principal, GetMCPConnectionSettingsInput{
		ProjectID: project.ID.String(), TargetKind: MCPConnectionSettingsMCPServer, TargetID: serverID,
	})
	require.ErrorIs(t, err, ErrMCPConnectionSettingsNotFound)

	_, err = service.Get(ctx, principal, GetMCPConnectionSettingsInput{
		ProjectID: project.ID.String(), TargetKind: MCPConnectionSettingsGateway, TargetID: serverID,
	})
	require.ErrorIs(t, err, ErrMCPConnectionSettingsNotFound)
	_, err = service.Get(authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, project.ID.String())}), principal, GetMCPConnectionSettingsInput{
		ProjectID: project.ID.String(), TargetKind: MCPConnectionSettingsGateway, TargetID: serverID,
	})
	require.ErrorIs(t, err, ErrMCPConnectionSettingsNotFound)

	foreign := principal
	foreign.OrganizationID = "other-org-" + uuid.NewString()
	_, err = service.Get(ctx, foreign, GetMCPConnectionSettingsInput{
		ProjectID: project.ID.String(), TargetKind: MCPConnectionSettingsMCPServer, TargetID: serverID,
	})
	require.ErrorIs(t, err, ErrMCPConnectionSettingsNotFound)

	otherProjectID := uuid.New()
	_, err = testrepo.New(conn).CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{
		ID: otherProjectID, Name: "Another project", Slug: "another-project", OrganizationID: principal.OrganizationID,
	})
	require.NoError(t, err)
	_, err = service.Get(authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID), authz.NewGrant(authz.ScopeMCPRead, serverID)}), principal, GetMCPConnectionSettingsInput{
		ProjectID: otherProjectID.String(), TargetKind: MCPConnectionSettingsMCPServer, TargetID: serverID,
	})
	require.ErrorIs(t, err, ErrMCPConnectionSettingsNotFound)

	for _, input := range []GetMCPConnectionSettingsInput{
		{ProjectID: "not-a-uuid", TargetKind: MCPConnectionSettingsMCPServer, TargetID: serverID},
		{ProjectID: project.ID.String(), TargetKind: MCPConnectionSettingsMCPServer, TargetID: "not-a-uuid"},
		{ProjectID: project.ID.String(), TargetKind: "other", TargetID: serverID},
	} {
		_, err = service.Get(ctx, principal, input)
		require.ErrorIs(t, err, ErrMCPConnectionSettingsInvalid)
	}
}
