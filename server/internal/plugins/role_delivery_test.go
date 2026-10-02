package plugins_test

import (
	publicationv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"
	"google.golang.org/protobuf/proto"
	"testing"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	endpointrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestRoleAudienceDeliversExistingUseGrant(t *testing.T) {
	t.Parallel()
	publisher := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, publisher)
	ti.service.WithPublicationRequests(true)
	ac, _ := contextvalues.GetAuthContext(ctx)
	role := createTestRolePrincipal(t, ctx, ti, "delivery")
	server := createTestToolset(t, ctx, ti.conn, "Delivery server")
	principal, err := urn.ParsePrincipal(role)
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, server.ID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
	require.NoError(t, err)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Delivery"})
	require.NoError(t, err)
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	beforeEvents, err := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	beforeIDs := make(map[int64]bool, len(beforeEvents))
	for _, event := range beforeEvents {
		beforeIDs[event.ID] = true
	}
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
	require.NoError(t, err)
	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Len(t, got.Servers, 1)
	require.Equal(t, server.ID.String(), *got.Servers[0].ToolsetID)
	events, err := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	refreshRequested := false
	for _, event := range events {
		if beforeIDs[event.ID] || event.Topic != "gram.plugins.v1.PublicationRequested" {
			continue
		}
		request := &publicationv1.PublicationRequested{}
		require.NoError(t, proto.Unmarshal(event.Message, request))
		if request.GetOrganizationId() == ac.ActiveOrganizationID && request.GetProjectId() == ac.ProjectID.String() {
			refreshRequested = true
		}
	}
	require.True(t, refreshRequested, "content delivery requests a refresh of the affected marketplace")
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	require.Contains(t, string(publisher.lastPushedFiles["cursor-plugins/delivery-cursor/mcp.json"]), server.McpSlug.String)
	membershipID := got.Servers[0].ID
	require.NoError(t, ti.service.RemovePluginServer(ctx, &gen.RemovePluginServerPayload{ID: membershipID, PluginID: plugin.ID}))
	// Replaying the same assignment and publishing do not recompute contents.
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
	require.NoError(t, err)
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Empty(t, got.Servers)
	require.NotContains(t, string(publisher.lastPushedFiles["cursor-plugins/delivery-cursor/mcp.json"]), server.McpSlug.String)
	// A new explicit assignment can add it again; removal history is not an exclusion.
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{}})
	require.NoError(t, err)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
	require.NoError(t, err)
	got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Len(t, got.Servers, 1)
	require.NotEqual(t, membershipID, got.Servers[0].ID)

}

func TestRoleAudiencePreservesLegacyToolsetMembershipIdentity(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	role := createTestRolePrincipal(t, ctx, ti, "legacy-delivery")
	server := createTestToolset(t, ctx, ti.conn, "Legacy server")
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Legacy delivery"})
	require.NoError(t, err)
	id := server.ID.String()
	membership, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: &id, Policy: "optional"})
	require.NoError(t, err)
	principal, err := urn.ParsePrincipal(role)
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, id).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
	require.NoError(t, err)
	wrapper, err := testrepo.New(ti.conn).CreateRemoteMCPServerFixture(ctx, testrepo.CreateRemoteMCPServerFixtureParams{ID: uuid.New(), ProjectID: *ac.ProjectID, ToolsetID: uuid.NullUUID{UUID: server.ID, Valid: true}, Visibility: "private"})
	require.NoError(t, err)
	_, err = endpointrepo.New(ti.conn).CreateMCPEndpoint(ctx, endpointrepo.CreateMCPEndpointParams{ProjectID: *ac.ProjectID, McpServerID: uuid.NullUUID{UUID: wrapper, Valid: true}, Slug: "legacy-endpoint"})
	require.NoError(t, err)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
	require.NoError(t, err)
	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Len(t, got.Servers, 1)
	require.Equal(t, membership.ID, got.Servers[0].ID)
	require.Equal(t, "optional", got.Servers[0].Policy)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{}})
	require.NoError(t, err)
	got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Empty(t, got.Servers)
}

func TestRoleAudienceDeliveryAvoidsOccupiedFallbackDisplayName(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	role := createTestRolePrincipal(t, ctx, ti, "name-collision")
	target := createTestToolset(t, ctx, ti.conn, "Example")
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Name collision"})
	require.NoError(t, err)
	expectedIDs := []string{target.ID.String()}
	for _, name := range []string{"Example", "Example (" + target.ID.String() + ")"} {
		other := createTestToolset(t, ctx, ti.conn, "Unrelated")
		id := other.ID.String()
		expectedIDs = append(expectedIDs, id)
		_, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: &id, DisplayName: &name, Policy: "optional"})
		require.NoError(t, err)
	}
	principal, err := urn.ParsePrincipal(role)
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, target.ID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
	require.NoError(t, err)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
	require.NoError(t, err, "delivery must not abort when both preferred names are occupied")
	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Len(t, got.Servers, 3)
	actualIDs := make([]string, 0, len(got.Servers))
	for _, entry := range got.Servers {
		require.NotNil(t, entry.ToolsetID)
		actualIDs = append(actualIDs, *entry.ToolsetID)
	}
	require.ElementsMatch(t, expectedIDs, actualIDs, "delivery preserves both original servers and adds the granted target")
}
