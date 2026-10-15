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
	"github.com/speakeasy-api/gram/server/internal/conv"
	endpointrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
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
	beforeEvents, err = testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	for _, event := range beforeEvents {
		beforeIDs[event.ID] = true
	}
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
	require.NoError(t, err)
	got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Len(t, got.Servers, 1)
	require.NotEqual(t, membershipID, got.Servers[0].ID)
	events, err = testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	refreshRequested = false
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
	require.True(t, refreshRequested, "explicit re-add requests a new refresh of the affected marketplace")
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
	// The public manual-add path also rejects the typed alias of this legacy entry.
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, McpServerID: conv.PtrEmpty(wrapper.String()), DisplayName: conv.PtrEmpty("Typed alias"), Policy: "required"})
	var duplicate *oops.ShareableError
	require.ErrorAs(t, err, &duplicate)
	require.Equal(t, oops.CodeConflict, duplicate.Code)
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

func TestAddPluginServerRejectsLegacyDuplicateOfRoleDeliveredServer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	role := createTestRolePrincipal(t, ctx, ti, "legacy-delivery")
	server := createTestToolset(t, ctx, ti.conn, "Legacy server")
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Legacy delivery"})
	require.NoError(t, err)
	id := server.ID.String()

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
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: &id, DisplayName: conv.PtrEmpty("Manual alias"), Policy: "optional"})
	var duplicate *oops.ShareableError
	require.ErrorAs(t, err, &duplicate)
	require.Equal(t, oops.CodeConflict, duplicate.Code)
	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Len(t, got.Servers, 1)
	require.Equal(t, "required", got.Servers[0].Policy, "duplicate does not overwrite automatic membership configuration")
	removedID := got.Servers[0].ID
	require.NoError(t, ti.service.RemovePluginServer(ctx, &gen.RemovePluginServerPayload{PluginID: plugin.ID, ID: removedID}))
	membership, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: &id, Policy: "optional"})
	require.NoError(t, err, "deleted typed membership does not prevent an explicit legacy re-add")
	require.NotEqual(t, removedID, membership.ID)
	got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
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

func TestRoleAudienceDeliversCanonicalRowOnceBesideGatewayMember(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	role := createTestRolePrincipal(t, ctx, ti, "canonical-delivery")
	toolset := createTestToolset(t, ctx, ti.conn, "Hosted")
	fixtures := testrepo.New(ti.conn)
	canonical, err := fixtures.CreateRemoteMCPServerFixture(ctx, testrepo.CreateRemoteMCPServerFixtureParams{ID: toolset.ID, ProjectID: *ac.ProjectID, ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "private"})
	require.NoError(t, err)
	member, err := fixtures.CreateRemoteMCPServerFixture(ctx, testrepo.CreateRemoteMCPServerFixtureParams{ID: uuid.New(), ProjectID: *ac.ProjectID, ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "private"})
	require.NoError(t, err)
	for _, id := range []uuid.UUID{canonical, member} {
		_, err = endpointrepo.New(ti.conn).CreateMCPEndpoint(ctx, endpointrepo.CreateMCPEndpointParams{ProjectID: *ac.ProjectID, McpServerID: uuid.NullUUID{UUID: id, Valid: true}, Slug: "endpoint-" + id.String()[:8]})
		require.NoError(t, err)
	}
	principal, err := urn.ParsePrincipal(role)
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, toolset.ID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
	require.NoError(t, err)

	fresh, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Canonical delivery"})
	require.NoError(t, err)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: fresh.ID, PrincipalUrns: []string{role}})
	require.NoError(t, err)
	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: fresh.ID})
	require.NoError(t, err)
	require.Len(t, got.Servers, 1)
	require.Equal(t, canonical.String(), *got.Servers[0].McpServerID)

	// An entry already delivered through the member stays and is not doubled.
	existing, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Member delivery"})
	require.NoError(t, err)
	delivered, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: existing.ID, McpServerID: conv.PtrEmpty(member.String()), Policy: "required"})
	require.NoError(t, err)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: existing.ID, PrincipalUrns: []string{role}})
	require.NoError(t, err)
	got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: existing.ID})
	require.NoError(t, err)
	require.Len(t, got.Servers, 1)
	require.Equal(t, delivered.ID, got.Servers[0].ID)
}

func TestRoleAudienceCanonicalEndpointFallback(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		visibility string
		endpoint   bool
	}{
		{name: "enabled endpointless", visibility: "private"},
		{name: "disabled endpointless", visibility: "disabled"},
		{name: "disabled with endpoint", visibility: "disabled", endpoint: true},
		{name: "enabled with endpoint", visibility: "private", endpoint: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestPluginsService(t)
			ac, _ := contextvalues.GetAuthContext(ctx)
			role := createTestRolePrincipal(t, ctx, ti, "endpoint-fallback")
			toolset := createTestToolset(t, ctx, ti.conn, "Hosted fallback")
			fixtures := testrepo.New(ti.conn)
			canonical, err := fixtures.CreateRemoteMCPServerFixture(ctx, testrepo.CreateRemoteMCPServerFixtureParams{ID: toolset.ID, ProjectID: *ac.ProjectID, ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: tt.visibility})
			require.NoError(t, err)
			var alternates []string
			for range 2 {
				id, err := fixtures.CreateRemoteMCPServerFixture(ctx, testrepo.CreateRemoteMCPServerFixtureParams{ID: uuid.New(), ProjectID: *ac.ProjectID, ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "private"})
				require.NoError(t, err)
				alternates = append(alternates, id.String())
				_, err = endpointrepo.New(ti.conn).CreateMCPEndpoint(ctx, endpointrepo.CreateMCPEndpointParams{ProjectID: *ac.ProjectID, McpServerID: uuid.NullUUID{UUID: id, Valid: true}, Slug: "endpoint-" + id.String()[:8]})
				require.NoError(t, err)
			}
			if tt.endpoint {
				_, err = endpointrepo.New(ti.conn).CreateMCPEndpoint(ctx, endpointrepo.CreateMCPEndpointParams{ProjectID: *ac.ProjectID, McpServerID: uuid.NullUUID{UUID: canonical, Valid: true}, Slug: "endpoint-" + canonical.String()[:8]})
				require.NoError(t, err)
			}
			principal, err := urn.ParsePrincipal(role)
			require.NoError(t, err)
			selectors, err := authz.NewSelector(authz.ScopeMCPConnect, toolset.ID.String()).MarshalJSON()
			require.NoError(t, err)
			_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
			require.NoError(t, err)
			plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Endpoint fallback"})
			require.NoError(t, err)
			// Fresh delivery and replay both keep at most one wrapper for the toolset.
			for range 2 {
				_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
				require.NoError(t, err)
				got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
				require.NoError(t, err)
				if tt.visibility == "disabled" {
					require.Empty(t, got.Servers)
					continue
				}
				require.Len(t, got.Servers, 1)
				require.NotNil(t, got.Servers[0].McpServerID)
				if tt.endpoint {
					require.Equal(t, canonical.String(), *got.Servers[0].McpServerID)
				} else {
					require.Contains(t, alternates, *got.Servers[0].McpServerID)
				}
			}
			if tt.visibility == "disabled" || tt.endpoint {
				return
			}

			got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
			require.NoError(t, err)
			require.Len(t, got.Servers, 1)
			removedID := got.Servers[0].ID
			require.NoError(t, ti.service.RemovePluginServer(ctx, &gen.RemovePluginServerPayload{PluginID: plugin.ID, ID: removedID}))
			// Setup invokes Populate with preserveRemoval, unlike unchanged audience replay.
			// The other live alternate must not bypass this toolset's removal history.
			require.NoError(t, processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{}))
			got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
			require.NoError(t, err)
			require.Empty(t, got.Servers, "setup replay must not resurrect a sibling wrapper")

			// A new explicit audience grant ignores deleted history, but still deduplicates siblings.
			_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{}})
			require.NoError(t, err)
			_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
			require.NoError(t, err)
			got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
			require.NoError(t, err)
			require.Len(t, got.Servers, 1)
			require.NotEqual(t, removedID, got.Servers[0].ID)
			require.NotNil(t, got.Servers[0].McpServerID)
			require.Contains(t, alternates, *got.Servers[0].McpServerID)
			fallback := got.Servers[0]

			// A newly live canonical endpoint must not replace or duplicate the existing fallback.
			_, err = endpointrepo.New(ti.conn).CreateMCPEndpoint(ctx, endpointrepo.CreateMCPEndpointParams{ProjectID: *ac.ProjectID, McpServerID: uuid.NullUUID{UUID: canonical, Valid: true}, Slug: "endpoint-" + canonical.String()[:8]})
			require.NoError(t, err)
			require.NoError(t, processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{}))
			got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
			require.NoError(t, err)
			require.Len(t, got.Servers, 1)
			require.Equal(t, fallback.ID, got.Servers[0].ID)
			require.Equal(t, fallback.McpServerID, got.Servers[0].McpServerID)
		})
	}
}
