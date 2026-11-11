package admin

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/associationbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// migrationOrg seeds one organization and project for the upstream OAuth
// migration through the selection backfill's fixtures.
type migrationOrg struct {
	t         *testing.T
	q         *selectionbackfill.Queries
	id        string
	projectID uuid.UUID
}

func newMigrationOrg(t *testing.T, ctx context.Context, db *pgxpool.Pool, name string) *migrationOrg {
	t.Helper()
	o := &migrationOrg{t: t, q: selectionbackfill.New(db), id: "org_" + uuid.NewString(), projectID: uuid.New()}
	seedOrg(t, ctx, db, orgFixture{id: o.id, name: name, slug: name, accountType: "enterprise"})
	require.NoError(t, o.q.SeedProjectFixture(ctx, selectionbackfill.SeedProjectFixtureParams{ID: o.projectID, Slug: "p-" + o.projectID.String()[:8], OrganizationID: o.id}))
	return o
}

func (o *migrationOrg) issuer() uuid.UUID {
	o.t.Helper()
	id := uuid.New()
	require.NoError(o.t, o.q.SeedUserSessionIssuerFixture(o.t.Context(), selectionbackfill.SeedUserSessionIssuerFixtureParams{
		ID:             id,
		ProjectID:      uuid.NullUUID{UUID: o.projectID, Valid: true},
		OrganizationID: pgtype.Text{String: o.id, Valid: true},
		Slug:           "usi-" + id.String()[:8],
	}))
	return id
}

// client creates a project client bound to userIssuerID.
func (o *migrationOrg) client(userIssuerID uuid.UUID) uuid.UUID {
	o.t.Helper()
	ctx := o.t.Context()
	issuerID, clientID := uuid.New(), uuid.New()
	require.NoError(o.t, o.q.SeedRemoteSessionIssuerFixture(ctx, selectionbackfill.SeedRemoteSessionIssuerFixtureParams{
		ID:        issuerID,
		ProjectID: uuid.NullUUID{UUID: o.projectID, Valid: true},
		Slug:      "rsi-" + issuerID.String()[:8],
		Issuer:    "https://" + issuerID.String()[:8] + ".example.com",
	}))
	require.NoError(o.t, o.q.SeedRemoteSessionClientFixture(ctx, selectionbackfill.SeedRemoteSessionClientFixtureParams{
		ID:                    clientID,
		ProjectID:             uuid.NullUUID{UUID: o.projectID, Valid: true},
		OrganizationID:        pgtype.Text{String: o.id, Valid: true},
		RemoteSessionIssuerID: issuerID,
		ClientID:              "cid-" + clientID.String()[:8],
	}))
	require.NoError(o.t, o.q.SeedClientBindingFixture(ctx, selectionbackfill.SeedClientBindingFixtureParams{RemoteSessionClientID: clientID, UserSessionIssuerID: userIssuerID}))
	return clientID
}

func (o *migrationOrg) server(userIssuerID uuid.UUID) uuid.UUID {
	o.t.Helper()
	ctx := o.t.Context()
	backendID, serverID := uuid.New(), uuid.New()
	require.NoError(o.t, o.q.SeedRemoteMCPServerFixture(ctx, selectionbackfill.SeedRemoteMCPServerFixtureParams{ID: backendID, ProjectID: o.projectID}))
	require.NoError(o.t, o.q.SeedMCPServerFixture(ctx, selectionbackfill.SeedMCPServerFixtureParams{
		ID:                  serverID,
		ProjectID:           o.projectID,
		Slug:                pgtype.Text{String: "s-" + serverID.String()[:8], Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{UUID: backendID, Valid: true},
		UserSessionIssuerID: uuid.NullUUID{UUID: userIssuerID, Valid: true},
	}))
	return serverID
}

// session seeds a live user session and returns its credential id.
func (o *migrationOrg) session(userIssuerID uuid.UUID) uuid.UUID {
	o.t.Helper()
	id := uuid.New()
	require.NoError(o.t, o.q.SeedUserSessionFixture(o.t.Context(), selectionbackfill.SeedUserSessionFixtureParams{
		ID:                  id,
		ProjectID:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:      pgtype.Text{String: o.id, Valid: true},
		UserSessionIssuerID: userIssuerID,
		SubjectUrn:          urn.NewUserSubject("user-" + uuid.NewString()[:8]),
		Jti:                 id.String(),
		RefreshExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		CredentialID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}))
	return id
}

func TestUpstreamOAuthMigration_MigratesAnOrganizationIdempotently(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})

	org := newMigrationOrg(t, ctx, db, "migrate-sole")
	issuer := org.issuer()
	org.client(issuer)
	org.server(issuer)
	org.session(issuer)

	before, err := svc.GetUpstreamOAuthMigrationStatus(ctx, &gen.GetUpstreamOAuthMigrationStatusPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.False(t, before.Migrated)
	require.Equal(t, int64(1), before.Servers)
	require.Equal(t, int64(1), before.PendingAssociations)
	require.Empty(t, before.UnresolvedServers)
	require.Equal(t, int64(1), before.Credentials)

	run, err := svc.RunUpstreamOAuthMigration(ctx, &gen.RunUpstreamOAuthMigrationPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Equal(t, int64(1), run.AssociationsWritten)
	require.Equal(t, int64(1), run.SelectionsWritten)
	require.True(t, run.Status.Migrated)
	require.Zero(t, run.Status.PendingAssociations)
	require.Zero(t, run.Status.PendingSelections)

	again, err := svc.RunUpstreamOAuthMigration(ctx, &gen.RunUpstreamOAuthMigrationPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Zero(t, again.AssociationsWritten)
	require.Zero(t, again.SelectionsWritten)
	require.True(t, again.Status.Migrated)
}

func TestUpstreamOAuthMigration_ReportsServersNeedingConfiguration(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})

	// Two servers share an issuer whose two clients both hold grants for
	// their upstream, so neither server's client is proven.
	org := newMigrationOrg(t, ctx, db, "migrate-shared")
	issuer := org.issuer()
	for _, clientID := range []uuid.UUID{org.client(issuer), org.client(issuer)} {
		require.NoError(t, associationbackfill.New(db).SeedGrantFixture(ctx, associationbackfill.SeedGrantFixtureParams{
			SubjectUrn:            urn.NewUserSubject("user-" + uuid.NewString()[:8]),
			UserSessionIssuerID:   issuer,
			RemoteSessionClientID: clientID,
			Resource:              pgtype.Text{String: "https://upstream.example.com/mcp", Valid: true},
		}))
	}
	first := org.server(issuer)
	second := org.server(issuer)
	org.session(issuer)

	run, err := svc.RunUpstreamOAuthMigration(ctx, &gen.RunUpstreamOAuthMigrationPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Zero(t, run.AssociationsWritten)
	require.False(t, run.Status.Migrated)
	unresolved := make([]string, 0, len(run.Status.UnresolvedServers))
	for _, server := range run.Status.UnresolvedServers {
		require.Equal(t, org.projectID.String(), server.ProjectID)
		unresolved = append(unresolved, server.McpServerID)
	}
	require.ElementsMatch(t, []string{first.String(), second.String()}, unresolved)
}

func TestUpstreamOAuthMigration_ListsOnlyOrganizationsWithIssuerServers(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)

	withServers := newMigrationOrg(t, ctx, db, "migrate-listed")
	issuer := withServers.issuer()
	withServers.server(issuer)
	withServers.server(issuer)
	newMigrationOrg(t, ctx, db, "migrate-unlisted")

	list, err := svc.ListUpstreamOAuthMigrationOrganizations(ctx, &gen.ListUpstreamOAuthMigrationOrganizationsPayload{})
	require.NoError(t, err)
	require.Len(t, list.Organizations, 1)
	require.Equal(t, withServers.id, list.Organizations[0].ID)
	require.Equal(t, int64(2), list.Organizations[0].Servers)
}

func TestUpstreamOAuthMigration_RunRequiresStaff(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	org := newMigrationOrg(t, ctx, db, "migrate-unauthorized")

	_, err := svc.RunUpstreamOAuthMigration(ctx, &gen.RunUpstreamOAuthMigrationPayload{OrganizationID: org.id})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnauthorized, shareable.Code)
}

func TestUpstreamOAuthMigration_RecordsAuthClientsWithoutGatingMigrated(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})

	org := newMigrationOrg(t, ctx, db, "migrate-auth-clients")
	issuer := org.issuer()
	org.client(issuer)
	server := org.server(issuer)
	org.session(issuer)

	run, err := svc.RunUpstreamOAuthMigration(ctx, &gen.RunUpstreamOAuthMigrationPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Equal(t, int64(1), *run.AuthClientsWritten)
	require.Equal(t, int64(1), *run.AuthClientLinksWritten)
	require.Nil(t, run.AuthClientsError)
	require.True(t, run.Status.Migrated)
	require.NotNil(t, run.Status.AuthClients)
	require.True(t, run.Status.AuthClients.Complete)
	require.Equal(t, int64(1), run.Status.AuthClients.Associations)

	// A missing link leaves auth clients incomplete without touching migrated.
	require.NoError(t, authclientbackfill.New(db).SoftDeleteServerAuthClientLinksFixture(ctx, server))
	status, err := svc.GetUpstreamOAuthMigrationStatus(ctx, &gen.GetUpstreamOAuthMigrationStatusPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.True(t, status.Migrated)
	require.False(t, status.AuthClients.Complete)
	require.Equal(t, int64(1), status.AuthClients.PendingLinks)
	require.Empty(t, status.AuthClients.FailedPhases)
	byKind := map[string]*gen.AdminUpstreamOAuthMigrationAuthClientKind{}
	for _, kind := range status.AuthClients.Kinds {
		byKind[kind.Kind] = kind
	}
	require.Len(t, byKind, 3, "every kind is reported")
	require.Equal(t, int64(1), byKind["oauth"].PendingLinks)
	require.Zero(t, byKind["header"].PendingLinks)
	require.Zero(t, byKind["ema"].PendingLinks)
}

func TestUpstreamOAuthMigration_ReportsAnAuthClientRunInProgress(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
	org := newMigrationOrg(t, ctx, db, "auth-clients-in-progress")

	tx, err := db.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary only; the lock uses a generated SQLc method.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	require.NoError(t, authclientbackfill.New(tx).LockOrganizationBackfill(ctx, org.id))

	status, err := svc.GetUpstreamOAuthMigrationStatus(ctx, &gen.GetUpstreamOAuthMigrationStatusPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.NotNil(t, status.AuthClients)
	require.True(t, status.AuthClients.InProgress)
	require.False(t, status.AuthClients.Complete)

	run, err := svc.RunUpstreamOAuthMigration(ctx, &gen.RunUpstreamOAuthMigrationPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.NotNil(t, run.AuthClientsError)
	require.Contains(t, *run.AuthClientsError, "already in progress")

	require.NoError(t, tx.Rollback(ctx))
	status, err = svc.GetUpstreamOAuthMigrationStatus(ctx, &gen.GetUpstreamOAuthMigrationStatusPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.False(t, status.AuthClients.InProgress)
	require.Empty(t, status.AuthClients.Warnings)
}

// Under enforcement a no-auth choice on a server whose auth client was detached
// is what issuance records, whatever association remains, so the migration
// status does not read it as pending or as a gap.
func TestUpstreamOAuthMigration_EnforcedNoAuthChoiceBesideAnAssociationIsCovered(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
	org := newMigrationOrg(t, ctx, db, "enforced-detached")
	issuer := org.issuer()
	server := org.server(issuer)
	registration := org.client(issuer)
	require.NoError(t, org.q.SeedAssociationFixture(ctx, selectionbackfill.SeedAssociationFixtureParams{ProjectID: org.projectID, OrganizationID: org.id, McpServerID: server, RemoteSessionClientID: registration}))

	_, err := svc.RunUpstreamOAuthMigration(ctx, &gen.RunUpstreamOAuthMigrationPayload{OrganizationID: org.id})
	require.NoError(t, err)
	_, err = authclientsrepo.New(db).DetachAuthClientFromServer(ctx, authclientsrepo.DetachAuthClientFromServerParams{AuthClientID: authclientbackfill.OAuthClientID(org.id, registration, authclientbackfill.TypeRemoteSession), McpServerID: server, OrganizationID: org.id})
	require.NoError(t, err)
	_, err = productfeaturesrepo.New(db).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: org.id, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
	require.NoError(t, err)
	org.session(issuer)

	run, err := svc.RunUpstreamOAuthMigration(ctx, &gen.RunUpstreamOAuthMigrationPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Equal(t, int64(1), run.SelectionsWritten)
	require.Zero(t, run.Status.PendingSelections)
	require.Zero(t, run.Status.ReconnectCredentials)
	require.NotNil(t, run.Status.AuthClients)
	require.Zero(t, *run.Status.AuthClients.SelectionGapCount, run.Status.AuthClients.SelectionGaps)
	require.Zero(t, *run.Status.AuthClients.PendingSelectionMappings, "the no-auth choice needs no mapping")
	require.True(t, *run.Status.AuthClients.SelectionsComplete, "the choices read complete, so enforcement is not blocked")
}

// Under enforcement a choice is recorded from the auth clients a server
// offers, so a run that has to create a server's OAuth-backed client, as when
// an earlier run's auth client phase failed, records the choice after it and
// names that client in one run, rather than an untyped no-auth choice the auth
// client backfill would then report as stale.
func TestUpstreamOAuthMigration_EnforcedRunRecordsChoicesAfterTheAuthClients(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
	org := newMigrationOrg(t, ctx, db, "enforced-recovery")
	issuer := org.issuer()
	server := org.server(issuer)
	registration := org.client(issuer)
	require.NoError(t, org.q.SeedAssociationFixture(ctx, selectionbackfill.SeedAssociationFixtureParams{ProjectID: org.projectID, OrganizationID: org.id, McpServerID: server, RemoteSessionClientID: registration}))
	_, err := productfeaturesrepo.New(db).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: org.id, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
	require.NoError(t, err)
	credential := org.session(issuer)

	run, err := svc.RunUpstreamOAuthMigration(ctx, &gen.RunUpstreamOAuthMigrationPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Nil(t, run.AuthClientsError)
	require.Equal(t, int64(1), *run.AuthClientsWritten)
	require.Equal(t, int64(1), run.SelectionsWritten)

	choice, err := org.q.GetSelectionAuthClientFixture(ctx, selectionbackfill.GetSelectionAuthClientFixtureParams{CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: credential, McpServerID: server})
	require.NoError(t, err)
	require.False(t, choice.NoAuth, "the server's OAuth client was recorded before the choice")
	require.Equal(t, uuid.NullUUID{UUID: authclientbackfill.OAuthClientID(org.id, registration, authclientbackfill.TypeRemoteSession), Valid: true}, choice.AuthClientID)

	require.Zero(t, run.Status.PendingSelections)
	require.Zero(t, run.Status.ReconnectCredentials)
	require.NotNil(t, run.Status.AuthClients)
	require.Zero(t, *run.Status.AuthClients.SelectionGapCount, run.Status.AuthClients.SelectionGaps)
	require.Zero(t, *run.Status.AuthClients.PendingSelectionMappings)
	require.True(t, *run.Status.AuthClients.SelectionsComplete)
}

// Under enforcement a run whose auth client phase failed records no choices,
// since a choice recorded without the server's auth clients would read as
// no-auth.
func TestUpstreamOAuthMigration_EnforcedRunRecordsNoChoicesWhenAuthClientsFail(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
	org := newMigrationOrg(t, ctx, db, "enforced-auth-clients-busy")
	issuer := org.issuer()
	server := org.server(issuer)
	registration := org.client(issuer)
	require.NoError(t, org.q.SeedAssociationFixture(ctx, selectionbackfill.SeedAssociationFixtureParams{ProjectID: org.projectID, OrganizationID: org.id, McpServerID: server, RemoteSessionClientID: registration}))
	_, err := productfeaturesrepo.New(db).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: org.id, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
	require.NoError(t, err)
	credential := org.session(issuer)

	tx, err := db.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary only; the lock uses a generated SQLc method.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	require.NoError(t, authclientbackfill.New(tx).LockOrganizationBackfill(ctx, org.id))

	run, err := svc.RunUpstreamOAuthMigration(ctx, &gen.RunUpstreamOAuthMigrationPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.NotNil(t, run.AuthClientsError)
	require.Contains(t, *run.AuthClientsError, "already in progress")
	require.Zero(t, run.SelectionsWritten)
	selections, err := org.q.ListSelectionsFixture(ctx, selectionbackfill.ListSelectionsFixtureParams{CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: credential})
	require.NoError(t, err)
	require.Empty(t, selections)
	require.NoError(t, tx.Rollback(ctx))

	run, err = svc.RunUpstreamOAuthMigration(ctx, &gen.RunUpstreamOAuthMigrationPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Nil(t, run.AuthClientsError)
	require.Equal(t, int64(1), run.SelectionsWritten)
	choice, err := org.q.GetSelectionAuthClientFixture(ctx, selectionbackfill.GetSelectionAuthClientFixtureParams{CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: credential, McpServerID: server})
	require.NoError(t, err)
	require.False(t, choice.NoAuth)
	require.True(t, choice.AuthClientID.Valid)
}
