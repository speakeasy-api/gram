package plugins_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	endpointrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/plugins/roledelivery"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

func roleGatewayFixture(t *testing.T, ctx context.Context, ti *testInstance, visibility string, oauth, endpoint bool) uuid.UUID {
	t.Helper()
	ac, _ := contextvalues.GetAuthContext(ctx)
	issuerID := uuid.NullUUID{}
	if oauth {
		issuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
			ProjectID: *ac.ProjectID, OrganizationID: conv.ToPGText(ac.ActiveOrganizationID), Slug: "role-gateway-issuer",
			AuthnChallengeMode: "interactive", SessionDuration: pgtype.Interval{Microseconds: time.Hour.Microseconds(), Valid: true},
		})
		require.NoError(t, err)
		issuerID = uuid.NullUUID{UUID: issuer.ID, Valid: true}
	}
	gateway, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, Name: "Role gateway", Visibility: visibility,
		UserSessionIssuerID: issuerID, NetworkAccessMode: pgtype.Text{},
	})
	require.NoError(t, err)
	if endpoint {
		_, err = endpointrepo.New(ti.conn).CreateMCPEndpoint(ctx, endpointrepo.CreateMCPEndpointParams{
			ProjectID: *ac.ProjectID, MetaMcpServerID: uuid.NullUUID{UUID: gateway.ID, Valid: true}, Slug: "role-gateway",
		})
		require.NoError(t, err)
	}
	return gateway.ID
}

func grantRoleGateway(t *testing.T, ctx context.Context, ti *testInstance, role string, gatewayID uuid.UUID, scope authz.Scope) {
	t.Helper()
	ac, _ := contextvalues.GetAuthContext(ctx)
	principal, err := urn.ParsePrincipal(role)
	require.NoError(t, err)
	selectors, err := authz.NewSelector(scope, gatewayID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: principal, Scope: string(scope), Selectors: selectors,
	})
	require.NoError(t, err)
}

func TestRoleGatewayDeliveryEligibility(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, visibility         string
		oauth, endpoint, granted bool
		scope                    authz.Scope
		want                     bool
	}{
		{"connect", "private", true, true, true, authz.ScopeMCPConnect, true},
		{"no grant", "private", true, true, false, authz.ScopeMCPConnect, false},
		{"read is not delivery", "private", true, true, true, authz.ScopeMCPRead, false},
		{"disabled", "disabled", true, true, true, authz.ScopeMCPConnect, false},
		{"no OAuth", "private", false, true, true, authz.ScopeMCPConnect, false},
		{"no endpoint", "private", true, false, true, authz.ScopeMCPConnect, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			features := &feature.InMemory{}
			ctx, ti := newTestPluginsServiceWithGitHubAndFeatures(t, &mockGitHubPublisher{}, features)
			ac, _ := contextvalues.GetAuthContext(ctx)
			features.SetFlag(feature.FlagGatewayPluginMembership, ac.ActiveOrganizationID, true)
			ti.service.WithDistributionAdmission(admission.NewGuard().WithGatewayPluginMembershipFeatures(features))
			role := createTestRolePrincipal(t, ctx, ti, "gateway-delivery")
			gatewayID := roleGatewayFixture(t, ctx, ti, tt.visibility, tt.oauth, tt.endpoint)
			if tt.granted {
				grantRoleGateway(t, ctx, ti, role, gatewayID, tt.scope)
			}
			plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Gateway eligibility"})
			require.NoError(t, err)
			_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
			require.NoError(t, err)
			got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
			require.NoError(t, err)
			if !tt.want {
				require.Empty(t, got.Servers)
				return
			}
			require.Len(t, got.Servers, 1)
			require.Equal(t, gatewayID.String(), *got.Servers[0].MetaMcpServerID)
			require.Nil(t, got.Servers[0].McpServerID)
		})
	}
}

func TestRoleGatewayDeliveryExcludesPlatformMembers(t *testing.T) {
	t.Parallel()
	features := &feature.InMemory{}
	ctx, ti := newTestPluginsServiceWithGitHubAndFeatures(t, &mockGitHubPublisher{}, features)
	ac, _ := contextvalues.GetAuthContext(ctx)
	features.SetFlag(feature.FlagGatewayPluginMembership, ac.ActiveOrganizationID, true)
	ti.service.WithDistributionAdmission(admission.NewGuard().WithGatewayPluginMembershipFeatures(features))
	role := createTestRolePrincipal(t, ctx, ti, "gateway-platform")
	gatewayID := roleGatewayFixture(t, ctx, ti, "private", true, true)
	member, toolsetID := platformDistributionServer(t, ctx, ti, true)
	_, err := toolsetsrepo.New(ti.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{
		ToolsetID: toolsetID, Version: 1, ToolUrns: []urn.Tool{urn.NewTool(urn.ToolKindPlatform, "slack", "send_message")}, ResourceUrns: []urn.Resource{},
	})
	require.NoError(t, err)
	_, err = metamcprepo.New(ti.conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{
		ProjectID: *ac.ProjectID, MetaMcpServerID: gatewayID, McpServerID: member.McpServerID.UUID,
	})
	require.NoError(t, err)
	grantRoleGateway(t, ctx, ti, role, gatewayID, authz.ScopeMCPConnect)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Gateway platform"})
	require.NoError(t, err)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
	require.NoError(t, err)
	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Empty(t, got.Servers)
}
func TestRoleGatewayDeliveryExportAndRemovalReplay(t *testing.T) {
	t.Parallel()
	features := &feature.InMemory{}
	publisher := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHubAndFeatures(t, publisher, features)
	ac, _ := contextvalues.GetAuthContext(ctx)
	features.SetFlag(feature.FlagGatewayPluginMembership, ac.ActiveOrganizationID, true)
	ti.service.WithDistributionAdmission(admission.NewGuard().WithGatewayPluginMembershipFeatures(features))
	role := createTestRolePrincipal(t, ctx, ti, "gateway-export")
	gatewayID := roleGatewayFixture(t, ctx, ti, "private", true, true)
	grantRoleGateway(t, ctx, ti, role, gatewayID, authz.ScopeMCPConnect)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Gateway delivery"})
	require.NoError(t, err)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
	require.NoError(t, err)
	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Len(t, got.Servers, 1)
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	for _, path := range []string{"cursor-plugins/gateway-delivery-cursor/mcp.json", "gateway-delivery/.mcp.json", "gateway-delivery-codex/.mcp.json"} {
		require.Contains(t, string(publisher.lastPushedFiles[path]), "role-gateway", path)
	}
	removedID := got.Servers[0].ID
	require.NoError(t, ti.service.RemovePluginServer(ctx, &gen.RemovePluginServerPayload{ID: removedID, PluginID: plugin.ID}))
	// Both setup and endpoint replay preserve an administrator's removal.
	tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: Exercise role delivery within its required transaction.
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	changed, err := roledelivery.Populate(ctx, tx, ac.ActiveOrganizationID, *ac.ProjectID, uuid.MustParse(plugin.ID), role, admission.NewGuard().WithGatewayPluginMembershipFeatures(features))
	require.NoError(t, err)
	require.False(t, changed)
	changed, err = roledelivery.Eligible(ctx, tx, ac.ActiveOrganizationID, *ac.ProjectID, gatewayID, admission.NewGuard().WithGatewayPluginMembershipFeatures(features))
	require.NoError(t, err)
	require.False(t, changed)
	require.NoError(t, tx.Commit(ctx))
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{}})
	require.NoError(t, err)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
	require.NoError(t, err)
	got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Len(t, got.Servers, 1)
	require.NotEqual(t, removedID, got.Servers[0].ID)
	// Turning off rollout never blocks revocation of an existing membership.
	features.SetFlag(feature.FlagGatewayPluginMembership, ac.ActiveOrganizationID, false)
	// Removing the only supplying role removes the gateway too.
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{}})
	require.NoError(t, err)
	got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Empty(t, got.Servers)
}
