package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestMCPConnectionSettingsToolHasSameInputSchemaWhenUnavailable(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "settings-live", Version: "0.0.1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "settings-unavailable", Version: "0.0.1"}, nil))
	registerMCPConnectionSettingsTool(live, nil)
	registerUnavailableMCPConnectionSettingsTool(unavailable)
	require.JSONEq(t, string(live.Descriptors()[0].InputSchema), string(unavailable.Descriptors()[0].InputSchema))
	require.Equal(t, live.Descriptors()[0].Meta, unavailable.Descriptors()[0].Meta)

	server := mcp.NewServer(&mcp.Implementation{Name: "settings-fallback", Version: "0.0.1"}, nil)
	bindExternalTestPrincipal(server)
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerUnavailableMCPConnectionSettingsTool(reg)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer func() { _ = serverSession.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "settings-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: getMCPConnectionSettingsToolName, Arguments: map[string]any{
		"project_id": uuid.NewString(), "target_kind": "mcp_server", "target_id": uuid.NewString(),
	}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, `"code":"feature_unavailable"`)
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
