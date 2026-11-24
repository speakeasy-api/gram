package usersessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// confirmedMigratePayload previews consolidating sourceID onto targetID and
// returns a migrate payload confirming the preview's fingerprint, as the
// dashboard does once an administrator has reviewed it.
func confirmedMigratePayload(t *testing.T, ctx context.Context, ti *testInstance, sourceID, targetID string) *orggen.MigrateIssuerPayload {
	t.Helper()
	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID, TargetID: targetID})
	require.NoError(t, err)
	return &orggen.MigrateIssuerPayload{SourceID: sourceID, TargetID: targetID, ConfirmedWarningsFingerprint: new(preflight.WarningsFingerprint)}
}

// impactSeedServer seeds a proxied server signing in through the issuer with
// no client association. Every server it seeds in a project shares one
// upstream URL.
func impactSeedServer(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID) uuid.UUID {
	t.Helper()
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	fixtures := selectionbackfill.New(ti.conn)
	backendID, serverID := uuid.New(), uuid.New()
	require.NoError(t, fixtures.SeedRemoteMCPServerFixture(ctx, selectionbackfill.SeedRemoteMCPServerFixtureParams{ID: backendID, ProjectID: *auth.ProjectID}))
	require.NoError(t, fixtures.SeedMCPServerFixture(ctx, selectionbackfill.SeedMCPServerFixtureParams{ID: serverID, ProjectID: *auth.ProjectID, Slug: conv.ToPGText("server-" + serverID.String()), RemoteMcpServerID: conv.ToNullUUID(backendID), UserSessionIssuerID: conv.ToNullUUID(issuerID)}))
	return serverID
}

// impactAssociatedClients lists the clients associated with a server.
func impactAssociatedClients(t *testing.T, ctx context.Context, ti *testInstance, serverID uuid.UUID) []uuid.UUID {
	t.Helper()
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	rows, err := upstreamclientsrepo.New(ti.conn).ListMCPServerRemoteSessionClients(ctx, upstreamclientsrepo.ListMCPServerRemoteSessionClientsParams{McpServerID: serverID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.RemoteSessionClientID)
	}
	return ids
}

func TestOrganizationUserSessionIssuerMigratePreservesOffersWithoutInheritingLinks(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	target := seedOrganizationTierIssuer(t, ctx, ti.conn, "offer-target")
	source := seedIssuer(t, ctx, ti, "offer-source")
	registration := seedMigrationRemoteClient(t, ctx, ti, "offered-without-link")
	hostedRegistration := seedMigrationRemoteClient(t, ctx, ti, "hosted-with-source-link", source)
	linkOnly := leftoverSeedBoundRegistration(t, ctx, ti, source)
	leftoverSeedBoundRegistration(t, ctx, ti, target)
	member := impactSeedServer(t, ctx, ti, source)
	hosted := seedHostedServerOnIssuer(t, ctx, ti, *auth.ProjectID, auth.ActiveOrganizationID, source, "offer-hosted")
	targetServer := impactSeedServer(t, ctx, ti, target)
	clientID := seedMigrationTypedOffer(t, ctx, ti, *auth.ProjectID, member, registration)
	hostedOffer := seedMigrationTypedOffer(t, ctx, ti, *auth.ProjectID, hosted, hostedRegistration)
	fixtures := selectionbackfill.New(ti.conn)
	require.NoError(t, fixtures.SeedAssociationFixture(ctx, selectionbackfill.SeedAssociationFixtureParams{
		ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, McpServerID: hosted, RemoteSessionClientID: hostedRegistration,
	}))
	gateway := uuid.New()
	require.NoError(t, fixtures.SeedMetaMCPServerFixture(ctx, selectionbackfill.SeedMetaMCPServerFixtureParams{ID: gateway, OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, Name: "typed-gateway", UserSessionIssuerID: conv.ToNullUUID(source)}))
	require.NoError(t, fixtures.SeedMetaMCPServerMemberFixture(ctx, selectionbackfill.SeedMetaMCPServerMemberFixtureParams{ProjectID: *auth.ProjectID, MetaMcpServerID: gateway, McpServerID: member}))
	session, err := seedUserSession(t, ctx, ti.conn, source, urn.NewUserSubject("typed-choice"))
	require.NoError(t, err)
	for range 2 {
		preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: source.String(), TargetID: target.String()})
		require.NoError(t, err)
		require.True(t, preflight.CanMigrate)
		require.Empty(t, preflight.ServerImpacts)
		require.Empty(t, preflight.DroppedClientLinks)
		choices, err := fixtures.ListSelectionsFixture(ctx, selectionbackfill.ListSelectionsFixtureParams{CredentialKind: "user_session", CredentialID: migrationCredentialID(session)})
		require.NoError(t, err)
		require.Empty(t, choices, "preview rolls back selection and issuer writes")
	}
	result, err := ti.service.MigrateIssuer(ctx, confirmedMigratePayload(t, ctx, ti, source.String(), target.String()))
	require.NoError(t, err)
	require.Empty(t, result.ServerImpacts)
	require.Empty(t, result.DroppedClientLinks)
	choice, err := fixtures.GetSelectionAuthClientFixture(ctx, selectionbackfill.GetSelectionAuthClientFixtureParams{CredentialKind: "user_session", CredentialID: migrationCredentialID(session), McpServerID: member})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(clientID), choice.AuthClientID)
	for _, reg := range []uuid.UUID{registration, hostedRegistration, linkOnly} {
		parents, err := remotesessionsrepo.New(ti.conn).CountRemoteSessionClientUserSessionIssuerBindings(ctx, remotesessionsrepo.CountRemoteSessionClientUserSessionIssuerBindingsParams{RemoteSessionClientID: reg, UserSessionIssuerID: target})
		require.NoError(t, err)
		require.Zero(t, parents, "only actual attachment rekeys may write target parents")
	}
	hostedOffers, err := authclientbackfill.New(ti.conn).ListServerAuthClientsFixture(ctx, hosted)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{hostedOffer}, hostedOffers, "the source-linked registration survives as the exact hosted offer, not as a target link")
	require.Equal(t, []uuid.UUID{hostedRegistration}, impactAssociatedClients(t, ctx, ti, hosted), "the exact hosted placement remains available to per-server readers")
	for _, id := range []uuid.UUID{member, targetServer} {
		require.Empty(t, impactAssociatedClients(t, ctx, ti, id))
	}
	_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: source.String(), TargetID: target.String()})
	require.Error(t, err, "retry with a retired source does not repeat writes")
}

func TestIssuerMigrationPreviewReadsDirectAssociationWithoutTypedHistory(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer := seedIssuer(t, ctx, ti, "association-preview")
	server, registration := seedIssuerMigrationOAuthServer(t, ctx, ti, *auth.ProjectID, issuer)
	params := usersessionsrepo.ListIssuerMigrationOAuthOffersParams{ServerIds: []uuid.UUID{server}, OrganizationID: auth.ActiveOrganizationID}
	offers, err := usersessionsrepo.New(ti.conn).ListIssuerMigrationOAuthOffers(ctx, params)
	require.NoError(t, err)
	require.Equal(t, []usersessionsrepo.ListIssuerMigrationOAuthOffersRow{{McpServerID: server, RemoteSessionClientID: registration}}, offers)
	typedClient := seedMigrationTypedOffer(t, ctx, ti, *auth.ProjectID, server, registration)
	require.NoError(t, authclientbackfill.New(ti.conn).SetAuthClientEnabledFixture(ctx, authclientbackfill.SetAuthClientEnabledFixtureParams{ID: typedClient, Enabled: false}))
	offers, err = usersessionsrepo.New(ti.conn).ListIssuerMigrationOAuthOffers(ctx, params)
	require.NoError(t, err)
	require.Empty(t, offers, "disabled typed history suppresses association fallback")
}

func seedMigrationTypedOffer(t *testing.T, ctx context.Context, ti *testInstance, projectID, serverID, registration uuid.UUID) uuid.UUID {
	t.Helper()
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	q := authclientbackfill.New(ti.conn)
	id, err := q.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: conv.ToNullUUID(projectID), ClientType: "remote_session", Source: "admin", RemoteSessionClientID: conv.ToNullUUID(registration)})
	require.NoError(t, err)
	require.NoError(t, q.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: projectID, McpServerID: serverID, AuthClientID: id, Source: "admin"}))
	return id
}

// A deleted client's binding is never carried to the target. It changes
// nothing about any server, so the preview neither lists it nor asks for it
// to be confirmed.
func TestOrganizationUserSessionIssuerMigrateNeverCarriesDeletedClient(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	targetID := seedIssuer(t, ctx, ti, "deleted-target")
	sourceID := seedIssuer(t, ctx, ti, "deleted-source")
	leftoverSeedServer(t, ctx, ti, sourceID)
	deleted := leftoverSeedBoundRegistration(t, ctx, ti, sourceID)
	require.NoError(t, authclientbackfill.New(ti.conn).SoftDeleteRemoteSessionClientFixture(ctx, deleted))

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID.String(), TargetID: targetID.String()})
	require.NoError(t, err)
	require.Empty(t, preflight.DroppedClientLinks)
	require.Empty(t, preflight.WarningsFingerprint)

	_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: targetID.String()})
	require.NoError(t, err)

	bound, err := remotesessionsrepo.New(ti.conn).CountRemoteSessionClientUserSessionIssuerBindings(ctx, remotesessionsrepo.CountRemoteSessionClientUserSessionIssuerBindingsParams{RemoteSessionClientID: deleted, UserSessionIssuerID: targetID})
	require.NoError(t, err)
	require.Zero(t, bound)
}

// The preview reports how far the source's sign-ins reach once they move,
// without making those counts part of what is confirmed.
func TestOrganizationUserSessionIssuerMigratePreflightReportsSignInReach(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	targetID := seedIssuer(t, ctx, ti, "reach-target")
	sourceID := seedIssuer(t, ctx, ti, "reach-source")
	impactSeedServer(t, ctx, ti, targetID)
	impactSeedServer(t, ctx, ti, targetID)
	_, err := seedUserSession(t, ctx, ti.conn, sourceID, urn.NewUserSubject("reach-user"))
	require.NoError(t, err)

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID.String(), TargetID: targetID.String()})
	require.NoError(t, err)
	require.Equal(t, 1, preflight.UnscopedSessionCount)
	require.Equal(t, 2, preflight.NewlyReachableServerCount)
	require.Empty(t, preflight.WarningsFingerprint, "reach counts are not confirmed")
}

// The preview takes the consolidation's locks, so a writer holding one makes
// it fail fast with a conflict to retry instead of waiting on it.
func TestOrganizationUserSessionIssuerMigratePreflightTimesOutOnHeldLock(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	targetID := seedIssuer(t, ctx, ti, "locked-target")
	sourceID := seedIssuer(t, ctx, ti, "locked-source")

	holder := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, usersessionsrepo.New(holder).LockUserSessionIssuerForOwnerBinding(ctx, sourceID))

	_, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID.String(), TargetID: targetID.String()})
	requireOopsCode(t, err, oops.CodeConflict)
}
