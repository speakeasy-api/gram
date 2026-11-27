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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/executionprofiles"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

// client creates a project registration without granting server access.
func (o *migrationOrg) client() uuid.UUID {
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
		CredentialID:        uuid.NullUUID{UUID: id, Valid: true},
	}))
	return id
}

// A profile no turn has run under has no version. A run records it before
// enforcement, so it is pending then. Under enforcement a turn records its own
// profile, so it is reported apart and no longer blocks.
func TestUpstreamOAuthMigration_EnforcedOrganizationReportsUnusedProfilesApart(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})

	org := newMigrationOrg(t, ctx, db, "migrate-unused-profiles")
	profiles := executionprofiles.New(db)
	assistant, err := profiles.SeedAssistantFixture(ctx, executionprofiles.SeedAssistantFixtureParams{OrganizationID: org.id, ProjectID: org.projectID, Name: "assistant"})
	require.NoError(t, err)
	trigger, err := profiles.SeedTriggerFixture(ctx, executionprofiles.SeedTriggerFixtureParams{OrganizationID: org.id, ProjectID: org.projectID, Name: "trigger"})
	require.NoError(t, err)
	require.NoError(t, profiles.SeedWorkloadBindingFixture(ctx, executionprofiles.SeedWorkloadBindingFixtureParams{OrganizationID: org.id, ProjectID: org.projectID, AssistantID: assistant, TriggerID: trigger}))
	contexts := int64(len(executionprofiles.Contexts))

	before, err := svc.GetUpstreamOAuthMigrationStatus(ctx, &gen.GetUpstreamOAuthMigrationStatusPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Equal(t, contexts, before.ExecutionProfiles.PendingVersions)
	require.Equal(t, new(int64(0)), before.ExecutionProfiles.UnexercisedVersions)
	require.False(t, before.ExecutionProfiles.Complete)
	require.Contains(t, before.Enforcement.Blockers, "Assistant profiles are not all recorded.")

	_, err = productfeaturesrepo.New(db).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: org.id, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
	require.NoError(t, err)

	enforced, err := svc.GetUpstreamOAuthMigrationStatus(ctx, &gen.GetUpstreamOAuthMigrationStatusPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Zero(t, enforced.ExecutionProfiles.PendingVersions)
	require.Equal(t, new(contexts), enforced.ExecutionProfiles.UnexercisedVersions)
	require.True(t, enforced.ExecutionProfiles.Complete)
	require.NotContains(t, enforced.Enforcement.Blockers, "Assistant profiles are not all recorded.")
}

// A project registration not offered by any server is not a readiness gap.
func TestUpstreamOAuthMigration_UnplacedRegistrationIsNotAReadinessGap(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})

	org := newMigrationOrg(t, ctx, db, "migrate-enforced")
	issuer := org.issuer()
	org.client()
	org.server(issuer)

	before, err := svc.GetUpstreamOAuthMigrationStatus(ctx, &gen.GetUpstreamOAuthMigrationStatusPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Zero(t, before.PendingAssociations, "an unplaced registration is not server configuration")

	_, err = productfeaturesrepo.New(db).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: org.id, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
	require.NoError(t, err)

	enforced, err := svc.GetUpstreamOAuthMigrationStatus(ctx, &gen.GetUpstreamOAuthMigrationStatusPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Zero(t, enforced.PendingAssociations)
	require.Empty(t, enforced.UnresolvedServers)
	require.True(t, enforced.Migrated)
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

	require.NoError(t, tx.Rollback(ctx))
	status, err = svc.GetUpstreamOAuthMigrationStatus(ctx, &gen.GetUpstreamOAuthMigrationStatusPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.False(t, status.AuthClients.InProgress)
	require.Empty(t, status.AuthClients.Warnings)
}

// seedCoveredMigrationFixture explicitly offers a registration and records
// the credential's typed choice without reconstructing legacy links.
func seedCoveredMigrationFixture(t *testing.T, ctx context.Context, db *pgxpool.Pool, org *migrationOrg, server, registration, credential uuid.UUID) {
	t.Helper()
	tx := testenv.BeginTx(t, ctx, db)
	q := authclientbackfill.New(tx)
	client, err := q.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{
		OrganizationID: org.id, ProjectID: uuid.NullUUID{UUID: org.projectID, Valid: true}, ClientType: "remote_session", Source: "admin",
		RemoteSessionClientID: uuid.NullUUID{UUID: registration, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, q.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{
		OrganizationID: org.id, ProjectID: org.projectID, McpServerID: server, AuthClientID: client, Source: "admin",
	}))
	require.NoError(t, q.SeedOAuthSelectionFixture(ctx, authclientbackfill.SeedOAuthSelectionFixtureParams{
		OrganizationID: org.id, ProjectID: org.projectID, McpServerID: server, RemoteSessionClientID: registration, CredentialID: credential,
	}))
	require.NoError(t, q.SwapUpstreamAuthClientFixture(ctx, authclientbackfill.SwapUpstreamAuthClientFixtureParams{
		McpServerID: server, CredentialID: credential, AuthClientID: uuid.NullUUID{UUID: client, Valid: true},
	}))
	require.NoError(t, authclientbackfill.ProvisionFromTypedOffers(ctx, tx, org.id))
	require.NoError(t, tx.Commit(ctx))
}

func TestCountReconnectCredentials(t *testing.T) {
	t.Parallel()
	duplicate := uuid.New()
	gaps := []authclientbackfill.SelectionGap{
		{CredentialID: duplicate, Outcome: authclientbackfill.SelectionAmbiguous},
		{CredentialID: duplicate, Outcome: authclientbackfill.SelectionUnusable},
		{CredentialID: uuid.New(), Outcome: authclientbackfill.SelectionStaleNoAuth},
		{CredentialID: uuid.New(), Outcome: authclientbackfill.SelectionNoClient},
		{CredentialID: uuid.New(), Outcome: authclientbackfill.SelectionSkippedRace},
	}
	require.EqualValues(t, 2, countReconnectCredentials(gaps))
}

func TestUpstreamOAuthMigration_StaffCannotRunRetiredMigration(t *testing.T) {
	t.Parallel()
	ctx := contextvalues.SetAdminAuthContext(t.Context(), &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
	_, err := (&Service{logger: testenv.NewLogger(t)}).RunUpstreamOAuthMigration(ctx, &gen.RunUpstreamOAuthMigrationPayload{})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeNotImplemented, shareable.Code)
}
