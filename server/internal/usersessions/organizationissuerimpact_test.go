package usersessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
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

// The incident this preview exists for: a source issuer's only server signs
// in through one client it was never explicitly associated with, and an
// organization issuer already fronts another server with no client at all.
// Merging the source's binding into the organization issuer used to leave
// the unassociated target server requiring that client, because nothing
// claimed it. Consolidation now records the source server's client first, so
// the target server still requires nothing, and the preview says so.
func TestOrganizationUserSessionIssuerMigratePinsSoleConsumerClientBeforeMerging(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	targetID := seedOrganizationTierIssuer(t, ctx, ti.conn, "pin-target")
	sourceID := seedIssuer(t, ctx, ti, "pin-source")
	targetServer := impactSeedServer(t, ctx, ti, targetID)
	sourceServer := impactSeedServer(t, ctx, ti, sourceID)
	sourceClient := leftoverSeedBoundRegistration(t, ctx, ti, sourceID)

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID.String(), TargetID: targetID.String()})
	require.NoError(t, err)
	require.True(t, preflight.ImpactPreviewed)
	require.True(t, preflight.CanMigrate)
	require.Len(t, preflight.ServerImpacts, 1, "only the pinned source server is listed; the target server does not change")
	impact := preflight.ServerImpacts[0]
	require.Equal(t, sourceServer.String(), impact.McpServerID)
	require.Equal(t, orggen.UserSessionIssuerMigrationImpactSeverity("info"), impact.ImpactSeverity)
	require.Equal(t, []orggen.UserSessionIssuerMigrationImpactReason{"pinned"}, impact.Reasons)
	require.Equal(t, []string{sourceClient.String()}, impact.PinnedClientIds)
	require.Empty(t, preflight.DroppedClientLinks)
	require.Empty(t, preflight.WarningsFingerprint, "a pin that changes nothing needs no confirmation")
	require.Empty(t, impactAssociatedClients(t, ctx, ti, sourceServer), "the preview rolls back")

	result, err := ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: targetID.String()})
	require.NoError(t, err)
	require.Len(t, result.ServerImpacts, 1)

	require.Equal(t, []uuid.UUID{sourceClient}, impactAssociatedClients(t, ctx, ti, sourceServer))
	bound, err := remotesessions.BoundUserSessionIssuerClients(ctx, ti.conn, auth.ActiveOrganizationID, *auth.ProjectID, targetID)
	require.NoError(t, err)
	required, err := remotesessions.RequiredServerClients(ctx, ti.conn, auth.ActiveOrganizationID, targetID, targetServer, bound)
	require.NoError(t, err)
	require.Empty(t, required, "the target server must not inherit the source server's client")
	required, err = remotesessions.RequiredServerClients(ctx, ti.conn, auth.ActiveOrganizationID, targetID, sourceServer, bound)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{sourceClient}, required)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerMigrate)
	require.NoError(t, err)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	pinned, ok := metadata["pinned_associations"].([]any)
	require.True(t, ok)
	require.Len(t, pinned, 1)
	impacts, ok := metadata["server_impacts"].([]any)
	require.True(t, ok)
	require.Len(t, impacts, 1)
}

// A server whose clients consolidation cannot decide keeps resolving the
// source's binding through the target, where an unassociated target server
// would start requiring it too. That client's provider is not one the target
// server's upstream advertises, so the preview blocks the consolidation
// instead of letting it hand the server a token for another upstream.
func TestOrganizationUserSessionIssuerMigrateBlocksClientOutsideUpstreamMetadata(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	targetID := seedIssuer(t, ctx, ti, "metadata-target")
	sourceID := seedIssuer(t, ctx, ti, "metadata-source")
	targetServer := impactSeedServer(t, ctx, ti, targetID)
	// Two servers share the source, so its binding proves neither.
	impactSeedServer(t, ctx, ti, sourceID)
	impactSeedServer(t, ctx, ti, sourceID)
	leftoverSeedBoundRegistration(t, ctx, ti, sourceID)
	require.NoError(t, authclientbackfill.New(ti.conn).SeedValidProtectedResourceFixture(ctx, authclientbackfill.SeedValidProtectedResourceFixtureParams{
		ProjectID:            *auth.ProjectID,
		OrganizationID:       auth.ActiveOrganizationID,
		ResourceIdentifier:   "https://upstream.example.com/mcp",
		MetadataUrl:          pgtype.Text{},
		AuthorizationServers: []string{"https://other-provider.example.com"},
	}))

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID.String(), TargetID: targetID.String()})
	require.NoError(t, err)
	require.True(t, preflight.ImpactPreviewed)
	require.False(t, preflight.CanMigrate)
	require.Len(t, preflight.ServerImpacts, 1)
	impact := preflight.ServerImpacts[0]
	require.Equal(t, targetServer.String(), impact.McpServerID)
	require.Equal(t, orggen.UserSessionIssuerMigrationImpactSeverity("blocker"), impact.ImpactSeverity)
	require.Equal(t, []orggen.UserSessionIssuerMigrationImpactReason{"provider_not_in_metadata", "gains_client"}, impact.Reasons)

	_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: targetID.String(), ConfirmedWarningsFingerprint: new(preflight.WarningsFingerprint)})
	requireOopsCode(t, err, oops.CodeConflict)
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

// A hosted server resolves every client its issuer binds, not only the ones
// associated with it. Carrying a source server's client of the same provider
// onto the hosted server's issuer would leave it two clients for one provider,
// which serving refuses, so the preview blocks the consolidation.
func TestOrganizationUserSessionIssuerMigrateBlocksSecondClientForHostedServer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	targetID := seedIssuer(t, ctx, ti, "hosted-target")
	sourceID := seedIssuer(t, ctx, ti, "hosted-source")
	hostedServer := seedHostedServerOnIssuer(t, ctx, ti, *auth.ProjectID, auth.ActiveOrganizationID, targetID, "hosted-impact")

	fixtures := selectionbackfill.New(ti.conn)
	providerID := uuid.New()
	require.NoError(t, fixtures.SeedRemoteSessionIssuerFixture(ctx, selectionbackfill.SeedRemoteSessionIssuerFixtureParams{ID: providerID, ProjectID: conv.ToNullUUID(*auth.ProjectID), Slug: "shared-provider", Issuer: "https://shared-provider.example.com"}))
	seedClient := func(issuerID uuid.UUID) uuid.UUID {
		clientID := uuid.New()
		require.NoError(t, fixtures.SeedRemoteSessionClientFixture(ctx, selectionbackfill.SeedRemoteSessionClientFixtureParams{ID: clientID, ProjectID: conv.ToNullUUID(*auth.ProjectID), OrganizationID: conv.ToPGText(auth.ActiveOrganizationID), RemoteSessionIssuerID: providerID, ClientID: clientID.String()}))
		require.NoError(t, fixtures.SeedClientBindingFixture(ctx, selectionbackfill.SeedClientBindingFixtureParams{RemoteSessionClientID: clientID, UserSessionIssuerID: issuerID}))
		return clientID
	}
	hostedClient := seedClient(targetID)
	sourceClient := seedClient(sourceID)
	sourceServer := impactSeedServer(t, ctx, ti, sourceID)
	require.NoError(t, fixtures.SeedAssociationFixture(ctx, selectionbackfill.SeedAssociationFixtureParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, McpServerID: sourceServer, RemoteSessionClientID: sourceClient}))

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID.String(), TargetID: targetID.String()})
	require.NoError(t, err)
	require.False(t, preflight.CanMigrate)
	var hosted *orggen.UserSessionIssuerMigrationServerImpact
	for _, impact := range preflight.ServerImpacts {
		if impact.McpServerID == hostedServer.String() {
			hosted = impact
		}
	}
	require.NotNil(t, hosted, "the hosted server's change is reported")
	require.Equal(t, orggen.UserSessionIssuerMigrationImpactSeverity("blocker"), hosted.ImpactSeverity)
	require.Contains(t, hosted.Reasons, orggen.UserSessionIssuerMigrationImpactReason("multiple_clients_for_provider"))
	require.Len(t, hosted.ClientsBefore, 1)
	require.Equal(t, hostedClient.String(), hosted.ClientsBefore[0].RemoteSessionClientID)
	require.Len(t, hosted.ClientsAfter, 2)
}
