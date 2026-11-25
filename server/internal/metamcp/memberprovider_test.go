package metamcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
)

func seedServableMember(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: conv.ToPGText("servable member"), Slug: conv.ToPGText("servable-member-" + uuid.NewString()),
		RemoteMcpServerID:   conv.ToNullUUID(seedRemoteBackend(t, ctx, ti.conn, projectID)),
		UserSessionIssuerID: conv.ToNullUUID(seedUserSessionIssuer(t, ctx, ti.conn, projectID)), Visibility: "private",
	})
	require.NoError(t, err)
	return server.ID
}

// Membership exposes exactly the registrations offered by that server;
// neither a registration on the same provider nor a sibling's offer is inherited.
func TestMemberProvider_UsesExplicitTypedOffers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, orgID := *auth.ProjectID, auth.ActiveOrganizationID
	meta := seedMetaMcpServer(t, ctx, ti, "typed offers gateway")
	server := seedServableMember(t, ctx, ti, projectID)
	sibling := seedServableMember(t, ctx, ti, projectID)
	provider := seedRemoteSessionIssuer(t, ctx, ti.conn, projectID, orgID, "member-provider")
	otherProvider := seedRemoteSessionIssuer(t, ctx, ti.conn, projectID, orgID, "other-provider")
	first := createRemoteSessionClient(t, ctx, ti.conn, projectID, orgID, provider, "first-offer")
	second := createRemoteSessionClient(t, ctx, ti.conn, projectID, orgID, provider, "second-offer")
	third := createRemoteSessionClient(t, ctx, ti.conn, projectID, orgID, otherProvider, "third-offer")
	unoffered := createRemoteSessionClient(t, ctx, ti.conn, projectID, orgID, provider, "not-offered")
	firstOffer := seedMemberOffer(t, ctx, ti.conn, projectID, orgID, server, first)
	q := authclientbackfill.New(ti.conn)
	secondOffer, err := q.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{
		OrganizationID: orgID, ProjectID: conv.ToNullUUID(projectID), ClientType: authclientbackfill.TypeEMA,
		Source: "admin", RemoteSessionClientID: conv.ToNullUUID(second),
	})
	require.NoError(t, err)
	require.NoError(t, q.SeedAuthClientEMAFixture(ctx, authclientbackfill.SeedAuthClientEMAFixtureParams{
		AuthClientID: secondOffer, Resource: "https://test.example.com/mcp",
	}))
	require.NoError(t, q.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{
		OrganizationID: orgID, ProjectID: projectID, McpServerID: server, AuthClientID: secondOffer, Source: "admin",
	}))
	thirdOffer := seedMemberOffer(t, ctx, ti.conn, projectID, orgID, sibling, third)

	eligibleMembers := func(registration uuid.UUID) []uuid.UUID {
		rows, err := metamcprepo.New(ti.conn).ListMetaMCPMembersForRemoteSessionClient(ctx, metamcprepo.ListMetaMCPMembersForRemoteSessionClientParams{
			MetaMcpServerID: uuid.MustParse(meta.ID), ProjectID: projectID, TypedOffers: true, RemoteSessionClientID: registration,
		})
		require.NoError(t, err)
		ids := make([]uuid.UUID, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.McpServerID)
		}
		return ids
	}
	add := func(id uuid.UUID) string {
		member, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{MetaMcpServerID: meta.ID, McpServerID: id.String()})
		require.NoError(t, err)
		return member.ID
	}
	require.Empty(t, eligibleMembers(first), "the offer is not reachable before the server joins")
	member := add(server)
	add(sibling)
	require.Equal(t, []uuid.UUID{server}, eligibleMembers(first))
	require.Equal(t, []uuid.UUID{server}, eligibleMembers(second))
	require.Equal(t, []uuid.UUID{sibling}, eligibleMembers(third))
	require.Empty(t, eligibleMembers(unoffered), "sharing a provider does not make a registration eligible")

	require.NoError(t, ti.service.RemoveMetaMcpMember(ctx, &gen.RemoveMetaMcpMemberPayload{ID: member}))
	require.Empty(t, eligibleMembers(first))
	require.Empty(t, eligibleMembers(second))
	require.Equal(t, []uuid.UUID{sibling}, eligibleMembers(third))
	requireMemberOffers(t, ctx, ti, server, firstOffer, secondOffer)
	requireMemberOffers(t, ctx, ti, sibling, thirdOffer)

	add(server)
	require.Equal(t, []uuid.UUID{server}, eligibleMembers(first), "rejoining makes the original offer reachable again")
	require.Equal(t, []uuid.UUID{server}, eligibleMembers(second))
	requireMemberOffers(t, ctx, ti, server, firstOffer, secondOffer)

	require.NoError(t, q.SetAuthClientEnabledFixture(ctx, authclientbackfill.SetAuthClientEnabledFixtureParams{ID: firstOffer, Enabled: false}))
	require.Empty(t, eligibleMembers(first), "a disabled client cannot qualify its member")
	require.Equal(t, []uuid.UUID{server}, eligibleMembers(second), "an enabled EMA offer still qualifies its member")
	require.Equal(t, []uuid.UUID{sibling}, eligibleMembers(third), "the sibling remains eligible until its offer is deleted")

	require.NoError(t, q.SoftDeleteServerAuthClientLinksFixture(ctx, sibling))
	require.Empty(t, eligibleMembers(third), "a soft-deleted offer cannot qualify its member")
}
