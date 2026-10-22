package platformmcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestSetGatewayMembersDeliversNewlyEligibleGatewayAndPreservesRemoval(t *testing.T) {
	t.Parallel()
	ctx, f := seedGatewayFixture(t, "platform_mcp_gateway_role_delivery")
	features := &feature.InMemory{}
	features.SetFlag(feature.FlagGatewayPluginMembership, f.principal.OrganizationID, true)
	f.gateways.writer.Admission = admission.NewGuard().WithGatewayPluginMembershipFeatures(features)
	alpha := f.seedRemoteMember(t, ctx, "Alpha")
	bravo := f.seedRemoteMember(t, ctx, "Bravo")
	created, err := f.create(ctx, "Role delivery", alpha)
	require.NoError(t, err)
	gatewayID := uuid.MustParse(created.GatewayID)
	ctx = f.grantMCP(ctx, f.project.ID, gatewayID)

	// Seed an existing Platform member directly: new hosted members are refused
	// by the gateway writer, but legacy gateways can still contain them.
	_, err = toolsetsrepo.New(f.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{
		ToolsetID: f.toolsetID, Version: 1,
		ToolUrns: []urn.Tool{urn.NewTool(urn.ToolKindPlatform, "slack", "send_message")}, ResourceUrns: []urn.Resource{},
	})
	require.NoError(t, err)
	_, err = metamcprepo.New(f.conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{
		ProjectID: f.project.ID, MetaMcpServerID: gatewayID, McpServerID: f.toolsetID, SortOrder: 1,
	})
	require.NoError(t, err)
	role, err := accessrepo.New(f.conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID: f.principal.OrganizationID, WorkosSlug: "gateway-delivery", WorkosName: "Gateway delivery",
		WorkosCreatedAt: conv.ToPGTimestamptz(time.Now()), WorkosUpdatedAt: conv.ToPGTimestamptz(time.Now()),
	})
	require.NoError(t, err)
	principal, err := urn.ParsePrincipal(role.RoleUrn)
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, gatewayID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(f.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: f.principal.OrganizationID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors,
	})
	require.NoError(t, err)
	queries := pluginsrepo.New(f.conn)
	plugin, err := queries.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: f.principal.OrganizationID, ProjectID: f.project.ID, Name: "Gateway delivery", Slug: "gateway-delivery",
	})
	require.NoError(t, err)
	_, err = queries.AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{
		OrganizationID: f.principal.OrganizationID, PluginID: plugin.ID, PrincipalUrn: role.RoleUrn,
	})
	require.NoError(t, err)

	// A changed member set still containing Platform tools stays excluded.
	_, err = f.set(ctx, created.GatewayID, alpha, f.toolsetID, bravo)
	require.NoError(t, err)
	members, err := queries.ListPluginServers(ctx, plugin.ID)
	require.NoError(t, err)
	require.Empty(t, members)

	updated, err := f.set(ctx, created.GatewayID, alpha, bravo)
	require.NoError(t, err)
	require.Equal(t, gatewayOutcomeUpdated, updated.Outcome)
	members, err = queries.ListPluginServers(ctx, plugin.ID)
	require.NoError(t, err)
	require.Len(t, members, 1, "removing the Platform member makes the gateway eligible for existing role delivery")
	require.Equal(t, gatewayID, members[0].MetaMcpServerID.UUID)

	// Model explicit removal through the same soft-delete used by plugin writes.
	_, err = queries.RemovePluginServer(ctx, pluginsrepo.RemovePluginServerParams{ID: members[0].ID, PluginID: plugin.ID})
	require.NoError(t, err)
	_, err = f.set(ctx, created.GatewayID, alpha)
	require.NoError(t, err)
	members, err = queries.ListPluginServers(ctx, plugin.ID)
	require.NoError(t, err)
	require.Empty(t, members, "another real member change must not restore explicitly removed delivery")
	_, err = f.set(ctx, created.GatewayID, alpha)
	require.NoError(t, err)
	members, err = queries.ListPluginServers(ctx, plugin.ID)
	require.NoError(t, err)
	require.Empty(t, members, "unchanged replay must also preserve removal")
}
