package remotesessionsorganization_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/remotesessionsorganization"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestRunFillsSessionsFromTheirClientOrganization(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	signInOrg := seedOrganization(t, ctx, pool)
	signInProject := seedProject(t, ctx, pool, signInOrg)
	userIssuer := seedUserSessionIssuer(t, ctx, pool, signInProject)
	remoteIssuer := seedGlobalRemoteIssuer(t, ctx, pool)
	clientOrg := seedOrganization(t, ctx, pool)

	projectClient := seedClient(t, ctx, pool, remoteIssuer, uuid.NullUUID{UUID: signInProject, Valid: true}, pgtype.Text{})
	projectSession := seedSessionWithoutOrganization(t, ctx, pool, userIssuer, projectClient)
	// The sign-in belongs to another organization; the client decides.
	orgClient := seedClient(t, ctx, pool, remoteIssuer, uuid.NullUUID{}, conv.ToPGText(clientOrg))
	orgSession := seedSessionWithoutOrganization(t, ctx, pool, userIssuer, orgClient)
	revokedClient := seedClient(t, ctx, pool, remoteIssuer, uuid.NullUUID{}, conv.ToPGText(clientOrg))
	revokedSession := seedSessionWithoutOrganization(t, ctx, pool, userIssuer, revokedClient)
	_, err := remotesessionsrepo.New(pool).SoftDeleteRemoteSessionsByClientID(ctx, revokedClient)
	require.NoError(t, err)
	revokedBefore := sessionOrganization(t, ctx, pool, revokedSession)
	// A client with both a project and an organization_id that disagree takes
	// its project's organization.
	mixedClient := seedClient(t, ctx, pool, remoteIssuer, uuid.NullUUID{UUID: signInProject, Valid: true}, conv.ToPGText(clientOrg))
	mixedSession := seedSessionWithoutOrganization(t, ctx, pool, userIssuer, mixedClient)
	// A deleted client's sessions keep tokens for upstream revocation.
	deletedClient := seedClient(t, ctx, pool, remoteIssuer, uuid.NullUUID{}, conv.ToPGText(clientOrg))
	deletedClientSession := seedSessionWithoutOrganization(t, ctx, pool, userIssuer, deletedClient)
	deleted, err := testrepo.New(pool).SoftDeleteRemoteSessionClientFixture(ctx, testrepo.SoftDeleteRemoteSessionClientFixtureParams{ID: deletedClient, ProjectID: uuid.NullUUID{}, OrganizationID: conv.ToPGText(clientOrg)})
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	report, err := remotesessionsorganization.Run(ctx, pool, remotesessionsorganization.Options{Apply: false, BatchSize: 1})
	require.NoError(t, err)
	require.Equal(t, remotesessionsorganization.Report{Filled: 0, Counts: remotesessionsorganization.Counts{Pending: 5}}, report)
	require.False(t, sessionOrganization(t, ctx, pool, projectSession).OrganizationID.Valid, "a dry run writes nothing")

	report, err = remotesessionsorganization.Run(ctx, pool, remotesessionsorganization.Options{Apply: true, BatchSize: 1})
	require.NoError(t, err)
	require.Equal(t, remotesessionsorganization.Report{Filled: 5, Counts: remotesessionsorganization.Counts{}}, report)
	require.Equal(t, conv.ToPGText(signInOrg), sessionOrganization(t, ctx, pool, projectSession).OrganizationID)
	require.Equal(t, conv.ToPGText(clientOrg), sessionOrganization(t, ctx, pool, orgSession).OrganizationID)
	revokedAfter := sessionOrganization(t, ctx, pool, revokedSession)
	require.Equal(t, conv.ToPGText(clientOrg), revokedAfter.OrganizationID, "soft-deleted sessions are filled too")
	require.Equal(t, revokedBefore.UpdatedAt, revokedAfter.UpdatedAt, "updated_at is the refresh compare-and-swap version")
	require.Equal(t, conv.ToPGText(signInOrg), sessionOrganization(t, ctx, pool, mixedSession).OrganizationID)
	require.Equal(t, conv.ToPGText(clientOrg), sessionOrganization(t, ctx, pool, deletedClientSession).OrganizationID)

	report, err = remotesessionsorganization.Run(ctx, pool, remotesessionsorganization.Options{Apply: true, BatchSize: 1})
	require.NoError(t, err)
	require.Equal(t, remotesessionsorganization.Report{Filled: 0, Counts: remotesessionsorganization.Counts{}}, report)
}

func TestRunLeavesSessionsWithoutAnOrganizationNull(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	org := seedOrganization(t, ctx, pool)
	userIssuer := seedUserSessionIssuer(t, ctx, pool, seedProject(t, ctx, pool, org))
	remoteIssuer := seedGlobalRemoteIssuer(t, ctx, pool)

	globalClient := seedClient(t, ctx, pool, remoteIssuer, uuid.NullUUID{}, pgtype.Text{})
	globalSession := seedSessionWithoutOrganization(t, ctx, pool, userIssuer, globalClient)
	// projects.organization_id has no foreign key, so a project can name an
	// organization with no metadata row.
	orphanProject := seedProject(t, ctx, pool, "org_"+uuid.NewString())
	orphanClient := seedClient(t, ctx, pool, remoteIssuer, uuid.NullUUID{UUID: orphanProject, Valid: true}, pgtype.Text{})
	orphanSession := seedSessionWithoutOrganization(t, ctx, pool, userIssuer, orphanClient)

	want := remotesessionsorganization.Report{Filled: 0, Counts: remotesessionsorganization.Counts{GlobalClient: 1, MissingOrganization: 1}}
	report, err := remotesessionsorganization.Run(ctx, pool, remotesessionsorganization.Options{Apply: false, BatchSize: 10})
	require.NoError(t, err)
	require.Equal(t, want, report)

	report, err = remotesessionsorganization.Run(ctx, pool, remotesessionsorganization.Options{Apply: true, BatchSize: 10})
	require.NoError(t, err)
	require.Equal(t, want, report)
	require.False(t, sessionOrganization(t, ctx, pool, globalSession).OrganizationID.Valid)
	require.False(t, sessionOrganization(t, ctx, pool, orphanSession).OrganizationID.Valid)
}

func TestRunReportsSessionsThatDisagreeWithTheirClient(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	org := seedOrganization(t, ctx, pool)
	otherOrg := seedOrganization(t, ctx, pool)
	project := seedProject(t, ctx, pool, org)
	client := seedClient(t, ctx, pool, seedGlobalRemoteIssuer(t, ctx, pool), uuid.NullUUID{UUID: project, Valid: true}, pgtype.Text{})
	session := seedSessionWithoutOrganization(t, ctx, pool, seedUserSessionIssuer(t, ctx, pool, project), client)
	require.NoError(t, testrepo.New(pool).SetRemoteSessionOrganizationFixture(ctx, testrepo.SetRemoteSessionOrganizationFixtureParams{OrganizationID: conv.ToPGText(otherOrg), ID: session}))

	report, err := remotesessionsorganization.Run(ctx, pool, remotesessionsorganization.Options{Apply: true, BatchSize: 10})
	require.NoError(t, err)
	require.Equal(t, remotesessionsorganization.Report{Filled: 0, Counts: remotesessionsorganization.Counts{Mismatched: 1}}, report)
	require.Equal(t, conv.ToPGText(otherOrg), sessionOrganization(t, ctx, pool, session).OrganizationID, "the backfill only fills NULL")
}

func seedOrganization(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()

	orgID := "org_" + uuid.NewString()
	_, err := orgrepo.New(pool).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:       orgID,
		Name:     "Test Org",
		Slug:     orgID,
		WorkosID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	return orgID
}

func seedProject(t *testing.T, ctx context.Context, pool *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	// Project names and slugs are capped at 40 characters.
	slug := "p-" + uuid.NewString()[:8]
	project, err := projectsrepo.New(pool).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: slug, Slug: slug, OrganizationID: organizationID})
	require.NoError(t, err)
	return project.ID
}

func seedUserSessionIssuer(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	issuer, err := usersessionsrepo.New(pool).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          projectID,
		Slug:               "sign-in-" + uuid.NewString(),
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: time.Hour.Microseconds(), Valid: true},
	})
	require.NoError(t, err)
	return issuer.ID
}

func seedGlobalRemoteIssuer(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()

	slug := "upstream-" + uuid.NewString()
	issuer, err := remotesessionsrepo.New(pool).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		Slug:                              slug,
		Issuer:                            "https://" + slug + ".example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://" + slug + ".example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://" + slug + ".example.com/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	})
	require.NoError(t, err)
	return issuer.ID
}

func seedClient(t *testing.T, ctx context.Context, pool *pgxpool.Pool, remoteIssuerID uuid.UUID, projectID uuid.NullUUID, organizationID pgtype.Text) uuid.UUID {
	t.Helper()

	client, err := remotesessionsrepo.New(pool).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:             projectID,
		OrganizationID:        organizationID,
		RemoteSessionIssuerID: remoteIssuerID,
		ClientID:              "client-" + uuid.NewString(),
	})
	require.NoError(t, err)
	return client.ID
}

// seedSessionWithoutOrganization writes a session through the production
// writer, then clears organization_id to reproduce a row written before the
// column existed.
func seedSessionWithoutOrganization(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userIssuerID, clientID uuid.UUID) uuid.UUID {
	t.Helper()

	session, err := remotesessionsrepo.New(pool).UpsertRemoteSession(ctx, remotesessionsrepo.UpsertRemoteSessionParams{
		SubjectUrn:            urn.NewUserSubject("user-" + uuid.NewString()),
		UserSessionIssuerID:   userIssuerID,
		RemoteSessionClientID: clientID,
		AccessTokenEncrypted:  "fixture-not-a-token",
		Scopes:                []string{},
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(pool).SetRemoteSessionOrganizationFixture(ctx, testrepo.SetRemoteSessionOrganizationFixtureParams{OrganizationID: pgtype.Text{}, ID: session.ID}))
	return session.ID
}

func sessionOrganization(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sessionID uuid.UUID) testrepo.GetRemoteSessionOrganizationFixtureRow {
	t.Helper()

	row, err := testrepo.New(pool).GetRemoteSessionOrganizationFixture(ctx, sessionID)
	require.NoError(t, err)
	return row
}
