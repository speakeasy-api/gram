package usersessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

// Lock discovery intentionally includes more than coverage: hosted consumers
// and recorded unmapped choices need locks even outside current issuer reach.
func TestOrganizationUserSessionIssuerMigrateLockScopeCoversSelectionReach(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *auth.ProjectID
	sourceID := seedIssuer(t, ctx, ti, "scope-source")
	projectIssuer := seedIssuer(t, ctx, ti, "scope-project")
	otherID := seedIssuer(t, ctx, ti, "scope-other")
	orgIssuer, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "scope-org", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	orgID := uuid.MustParse(orgIssuer.ID)
	direct, transferredRegistration := seedIssuerMigrationOAuthServer(t, ctx, ti, projectID, sourceID)
	member, _ := seedIssuerMigrationOAuthServer(t, ctx, ti, projectID, otherID)
	recorded, registration := seedIssuerMigrationOAuthServer(t, ctx, ti, projectID, otherID)
	hosted := seedHostedServerOnIssuer(t, ctx, ti, projectID, auth.ActiveOrganizationID, projectIssuer, "scope-hosted")
	sibling := createSiblingProject(t, ctx, ti.conn, "scope-sibling")
	siblingIssuer := seedIssuerInProject(t, ctx, ti.conn, sibling, "scope-sibling-issuer")
	crossProject, _ := seedIssuerMigrationOAuthServer(t, ctx, ti, sibling, siblingIssuer)
	typedTx := testenv.BeginTx(t, ctx, ti.conn)
	defer func() { _ = typedTx.Rollback(context.WithoutCancel(ctx)) }()
	require.NoError(t, authclientbackfill.LockEMAWriterScope(ctx, typedTx, auth.ActiveOrganizationID, []uuid.UUID{sibling}))
	require.NoError(t, authclientbackfill.SyncServerAssociations(ctx, typedTx, auth.ActiveOrganizationID, crossProject))
	require.NoError(t, typedTx.Commit(ctx))
	fixtures := selectionbackfill.New(ti.conn)
	unowned, unownedBackend := uuid.New(), uuid.New()
	require.NoError(t, fixtures.SeedRemoteMCPServerFixture(ctx, selectionbackfill.SeedRemoteMCPServerFixtureParams{ID: unownedBackend, ProjectID: projectID}))
	require.NoError(t, fixtures.SeedMCPServerFixture(ctx, selectionbackfill.SeedMCPServerFixtureParams{ID: unowned, ProjectID: projectID, Slug: conv.ToPGText("scope-unowned"), RemoteMcpServerID: conv.ToNullUUID(unownedBackend), UserSessionIssuerID: uuid.NullUUID{}}))
	targetOnly, targetRegistration := seedIssuerMigrationOAuthServer(t, ctx, ti, projectID, orgID)
	typedFixtures := authclientbackfill.New(ti.conn)
	emaClient, err := typedFixtures.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{
		OrganizationID: auth.ActiveOrganizationID, ProjectID: conv.ToNullUUID(projectID),
		ClientType: "ema", Source: "admin", RemoteSessionClientID: conv.ToNullUUID(targetRegistration),
	})
	require.NoError(t, err)
	require.NoError(t, typedFixtures.SeedAuthClientEMAFixture(ctx, authclientbackfill.SeedAuthClientEMAFixtureParams{AuthClientID: emaClient, Resource: "https://upstream.example.com/resource"}))
	require.NoError(t, typedFixtures.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{
		OrganizationID: auth.ActiveOrganizationID, ProjectID: projectID, McpServerID: targetOnly, AuthClientID: emaClient, Source: "admin",
	}))
	require.NoError(t, fixtures.SeedAssociationFixture(ctx, selectionbackfill.SeedAssociationFixtureParams{
		ProjectID: projectID, OrganizationID: auth.ActiveOrganizationID,
		McpServerID: member, RemoteSessionClientID: transferredRegistration,
	}))
	gateway := uuid.New()
	require.NoError(t, fixtures.SeedMetaMCPServerFixture(ctx, selectionbackfill.SeedMetaMCPServerFixtureParams{ID: gateway, OrganizationID: auth.ActiveOrganizationID, ProjectID: projectID, Name: "scope-gateway", UserSessionIssuerID: uuid.NullUUID{UUID: projectIssuer, Valid: true}}))
	require.NoError(t, fixtures.SeedMetaMCPServerMemberFixture(ctx, selectionbackfill.SeedMetaMCPServerMemberFixtureParams{ProjectID: projectID, MetaMcpServerID: gateway, McpServerID: member}))
	session, err := seedUserSession(t, ctx, ti.conn, sourceID, urn.NewUserSubject("scope-user"))
	require.NoError(t, err)
	require.NoError(t, authclientbackfill.New(ti.conn).SeedOAuthSelectionFixture(ctx, authclientbackfill.SeedOAuthSelectionFixtureParams{ProjectID: projectID, OrganizationID: auth.ActiveOrganizationID, CredentialID: migrationCredentialID(session), McpServerID: recorded, RemoteSessionClientID: registration}))
	for _, target := range []uuid.UUID{projectIssuer, orgID} {
		rows, err := usersessionsrepo.New(ti.conn).ListMCPServersForUserSessionIssuerMigration(ctx, usersessionsrepo.ListMCPServersForUserSessionIssuerMigrationParams{OrganizationID: auth.ActiveOrganizationID, IssuerIds: []uuid.UUID{sourceID, target}, SourceIssuerID: sourceID})
		require.NoError(t, err)
		locked := make(map[uuid.UUID]bool, len(rows))
		needsSync := make(map[uuid.UUID]bool, len(rows))
		for _, row := range rows {
			locked[row.ID] = true
			needsSync[row.ID] = row.NeedsSync
		}
		for _, issuer := range []uuid.UUID{sourceID, target} {
			coverage, err := fixtures.ListUpstreamClientSelectionCoverage(ctx, selectionbackfill.ListUpstreamClientSelectionCoverageParams{UserSessionIssuerID: issuer, CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: migrationCredentialID(session)})
			require.NoError(t, err)
			for _, pair := range coverage {
				require.True(t, locked[pair.McpServerID], "coverage server %s must be locked for issuer %s", pair.McpServerID, issuer)
			}
		}
		require.True(t, locked[direct])
		require.True(t, needsSync[direct], "moved consumer needs typed maintenance")
		require.True(t, needsSync[member], "a transferred registration affects other associated servers")
		require.True(t, needsSync[recorded], "unmapped recorded choice needs typed maintenance")
		require.False(t, needsSync[crossProject], "unrelated org reach is locked but not synchronized")
		require.False(t, needsSync[hosted], "target-only hosted consumer is not changed")
		require.True(t, locked[recorded], "unmapped recorded choice must be locked outside current reach")
		if target == projectIssuer {
			require.True(t, locked[member])
			require.True(t, locked[hosted])
			require.False(t, locked[crossProject])
		} else {
			require.True(t, locked[crossProject])
			require.True(t, locked[unowned], "org reach includes servers without direct issuers")
			require.False(t, needsSync[unowned], "unowned unrelated server has non-null false sync flag")
			require.True(t, needsSync[targetOnly], "EMA-only offers do not satisfy a new legacy OAuth choice's typed state")
		}
	}
	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID.String(), TargetID: orgIssuer.ID})
	require.NoError(t, err)
	_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: orgIssuer.ID, ConfirmedWarningsFingerprint: &preflight.WarningsFingerprint})
	require.NoError(t, err)
	choice, err := fixtures.GetSelectionAuthClientFixture(ctx, selectionbackfill.GetSelectionAuthClientFixtureParams{CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: migrationCredentialID(session), McpServerID: targetOnly})
	require.NoError(t, err)
	require.True(t, choice.AuthClientID.Valid, "a missing choice for an existing target consumer must gain typed state")
}

// A binding writer's shared reach gate precedes its issuer locks. Consolidation
// takes the exclusive counterpart before discovering registrations, so no new
// binding can appear in the proposed discovery-to-issuer-lock window.
func TestOrganizationUserSessionIssuerMigrateSerializesBindingReach(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	source := seedIssuer(t, ctx, ti, "reach-source")
	target := seedIssuer(t, ctx, ti, "reach-target")
	bindingTx := testenv.BeginTx(t, ctx, ti.conn)
	defer func() { _ = bindingTx.Rollback(context.WithoutCancel(ctx)) }()
	require.NoError(t, authclientbackfill.LockEMAWriterScope(ctx, bindingTx, auth.ActiveOrganizationID, []uuid.UUID{*auth.ProjectID}))
	blocked, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	_, err := ti.service.MigrateIssuer(blocked, &orggen.MigrateIssuerPayload{SourceID: source.String(), TargetID: target.String()})
	require.Error(t, err, "migration must wait for the binding writer before discovery")
	require.ErrorIs(t, blocked.Err(), context.DeadlineExceeded)
	require.NoError(t, bindingTx.Commit(ctx))
	_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: source.String(), TargetID: target.String()})
	require.NoError(t, err, "migration can proceed after the binding writer releases its reach gate")
	// The reverse order also blocks attachment preparation before its row locks.
	migrationTx := testenv.BeginTx(t, ctx, ti.conn)
	defer func() { _ = migrationTx.Rollback(context.WithoutCancel(ctx)) }()
	require.NoError(t, authclientbackfill.New(migrationTx).LockEMAGatewayReachWriter(ctx, auth.ActiveOrganizationID))
	attachmentTx := testenv.BeginTx(t, ctx, ti.conn)
	defer func() { _ = attachmentTx.Rollback(context.WithoutCancel(ctx)) }()
	attachmentCtx, attachmentCancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer attachmentCancel()
	err = authclientbackfill.LockEMAWriterScope(attachmentCtx, attachmentTx, auth.ActiveOrganizationID, []uuid.UUID{*auth.ProjectID})
	require.Error(t, err, "attachment preparation must wait before it can add an ungated registration")
	require.ErrorIs(t, attachmentCtx.Err(), context.DeadlineExceeded)
}
