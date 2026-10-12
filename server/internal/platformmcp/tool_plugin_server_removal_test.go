package platformmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	plugindelivery "github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestRemovePluginServerUnavailableThroughMCP(t *testing.T) {
	t.Parallel()
	server, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			ctx = contextWithPrincipal(ctx, testPrincipal())
			ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, testPrincipal().OrganizationID)})
			return next(ctx, method, req)
		}
	})
	registrar.withExternalAuthorizer(allowExternalCallAuthorizer{})
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer func() { _ = serverSession.Close() }()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "removal-test", Version: "1"}, nil).Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()
	var found *mcp.Tool
	for tool, err := range session.Tools(t.Context(), nil) {
		require.NoError(t, err)
		if tool.Name == "remove_plugin_server" {
			found = tool
		}
	}
	require.NotNil(t, found)
	require.NotNil(t, found.Annotations)
	require.NotNil(t, found.Annotations.DestructiveHint)
	require.True(t, *found.Annotations.DestructiveHint)
	require.True(t, found.Annotations.IdempotentHint)
	refusal := callSkillsRefusal(t, t.Context(), session, "remove_plugin_server", map[string]any{
		"project_id": uuid.NewString(), "plugin_id": uuid.NewString(), "membership_id": uuid.NewString(), "confirmed": true,
	})
	require.Equal(t, "feature_unavailable", refusal.Code)
}

func TestRemovePluginServerThroughMCP(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_exact_plugin_removal")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Manual plugin", "manual-plugin")
	otherPlugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Other plugin", "other-plugin")
	remoteID := uuid.New()
	_, err = remotemcprepo.New(conn).CreateServer(ctx, remotemcprepo.CreateServerParams{ID: remoteID, ProjectID: project.ID, TransportType: "streamable-http", Url: "https://example.test/mcp"})
	require.NoError(t, err)
	serverID := uuid.New()
	_, err = mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{ID: serverID, ProjectID: project.ID, Name: conv.ToPGText("Shared server"), Slug: conv.ToPGText("shared-server"), RemoteMcpServerID: uuid.NullUUID{UUID: remoteID, Valid: true}, Visibility: "private"})
	require.NoError(t, err)
	add := func(pluginID uuid.UUID) string {
		row, err := pluginsrepo.New(conn).AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{PluginID: pluginID, McpServerID: uuid.NullUUID{UUID: serverID, Valid: true}, DisplayName: "Shared server", Policy: "required"})
		require.NoError(t, err)
		return row.ID.String()
	}
	membershipID := add(plugin.ID)
	otherMembershipID := add(otherPlugin.ID)
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	delivery := plugindelivery.NewService(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, &sessions.Manager{}, nil, engine, audit.NewLogger(), nil, "test", "https://example.test", nil, nil)
	service := testPluginTargets(conn).WithAuthorization(engine).WithServerRemoval(delivery)

	admin := newPluginRemovalSession(t, service, principal, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID)}, nil)
	before := callSkillsTool[GetPluginOutput](t, ctx, admin, "get_plugin", map[string]any{"project_id": project.ID.String(), "plugin": plugin.ID.String()})
	require.Len(t, before.Servers, 1)
	require.Equal(t, membershipID, before.Servers[0].MembershipID)
	args := map[string]any{"project_id": project.ID.String(), "plugin_id": plugin.ID.String(), "membership_id": membershipID, "confirmed": false}
	require.Equal(t, "confirmation_required", callSkillsRefusal(t, ctx, admin, "remove_plugin_server", args).Code)
	args["confirmed"] = true

	for _, tc := range []struct {
		name   string
		grants []authz.Grant
	}{
		{"member", []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, principal.OrganizationID)}},
		{"wrong project writer", []authz.Grant{authz.NewGrant(authz.ScopePluginWrite, uuid.NewString())}},
		{"blocked writer", []authz.Grant{authz.NewGrant(authz.ScopePluginWrite, project.ID.String()), authz.NewGrant(authz.ScopePluginBlockedWrite, project.ID.String())}},
	} {
		session := newPluginRemovalSession(t, service, principal, tc.grants, nil)
		require.Equal(t, "permission_denied", callSkillsRefusal(t, ctx, session, "remove_plugin_server", args).Code, tc.name)
	}
	args["plugin_id"] = otherPlugin.ID.String()
	require.Equal(t, "not_found", callSkillsRefusal(t, ctx, admin, "remove_plugin_server", args).Code)
	args["plugin_id"] = plugin.ID.String()
	args["project_id"] = uuid.NewString()
	require.Equal(t, "not_found", callSkillsRefusal(t, ctx, admin, "remove_plugin_server", args).Code)
	args["project_id"] = project.ID.String()
	foreign := principal
	foreign.OrganizationID = "org_other"
	foreignAdmin := newPluginRemovalSession(t, service, foreign, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, foreign.OrganizationID)}, nil)
	require.Equal(t, "not_found", callSkillsRefusal(t, ctx, foreignAdmin, "remove_plugin_server", args).Code)

	writer := newPluginRemovalSession(t, service, principal, []authz.Grant{authz.NewGrant(authz.ScopePluginWrite, project.ID.String())}, nil)
	removed := callSkillsTool[RemovePluginServerOutput](t, ctx, writer, "remove_plugin_server", args)
	require.True(t, removed.Removed)
	after := callSkillsTool[GetPluginOutput](t, ctx, admin, "get_plugin", map[string]any{"project_id": project.ID.String(), "plugin": plugin.ID.String()})
	require.Empty(t, after.Servers)
	other := callSkillsTool[GetPluginOutput](t, ctx, admin, "get_plugin", map[string]any{"project_id": project.ID.String(), "plugin": otherPlugin.ID.String()})
	require.Len(t, other.Servers, 1)
	require.Equal(t, otherMembershipID, other.Servers[0].MembershipID)
	require.Equal(t, "not_found", callSkillsRefusal(t, ctx, writer, "remove_plugin_server", args).Code)
	replacementID := add(plugin.ID)
	require.NotEqual(t, membershipID, replacementID)
	require.Equal(t, "not_found", callSkillsRefusal(t, ctx, admin, "remove_plugin_server", args).Code)
	replacement := callSkillsTool[GetPluginOutput](t, ctx, admin, "get_plugin", map[string]any{"project_id": project.ID.String(), "plugin": plugin.ID.String()})
	require.Len(t, replacement.Servers, 1)
	require.Equal(t, replacementID, replacement.Servers[0].MembershipID)
	args["membership_id"] = replacementID
	require.True(t, callSkillsTool[RemovePluginServerOutput](t, ctx, admin, "remove_plugin_server", args).Removed)
}

func newPluginRemovalSession(t *testing.T, service *PluginsService, principal Principal, grants []authz.Grant, accessReads *AccessReadService) *mcp.ClientSession {
	t.Helper()
	server := newTestMCPServer()
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			ctx = contextWithPrincipal(ctx, principal)
			ctx = authz.GrantsToContext(ctx, grants)
			ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID})
			return next(ctx, method, req)
		}
	})
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerRemovePluginServerTool(reg, service)
	registerPluginTools(reg, service)
	if accessReads != nil {
		registerAccessReadTools(reg, accessReads)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "removal-test", Version: "1"}, nil).Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}
