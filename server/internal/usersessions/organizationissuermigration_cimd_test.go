package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestOrganizationUserSessionIssuerMigrateMergesDuplicateCimdClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	repo := usersessionsrepo.New(ti.conn)

	sourceID := seedIssuer(t, ctx, ti, "cimd-merge-source")
	target, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "cimd-merge-target", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	targetID := uuid.MustParse(target.ID)

	const documentURL = "https://cimd-merge.example.com/client-metadata"
	sourceClient, err := seedCimdUserSessionClient(t, ctx, ti.conn, sourceID, documentURL)
	require.NoError(t, err)
	targetClient, err := seedCimdUserSessionClient(t, ctx, ti.conn, targetID, documentURL)
	require.NoError(t, err)

	// One person approved the same upstream set through both issuers.
	shared := urn.NewUserSubject("cimd-merge-shared")
	sourceShared, err := repo.CreateUserSessionConsent(ctx, usersessionsrepo.CreateUserSessionConsentParams{SubjectUrn: shared, RemoteSetHash: "shared-set", UserSessionClientID: sourceClient.ID})
	require.NoError(t, err)
	targetShared, err := repo.CreateUserSessionConsent(ctx, usersessionsrepo.CreateUserSessionConsentParams{SubjectUrn: shared, RemoteSetHash: "shared-set", UserSessionClientID: targetClient.ID})
	require.NoError(t, err)
	for _, seed := range []struct {
		consentID uuid.UUID
		resource  string
	}{
		{consentID: sourceShared.ID, resource: "https://source-only.example.com/mcp"},
		{consentID: sourceShared.ID, resource: "https://both.example.com/mcp"},
		{consentID: targetShared.ID, resource: "https://both.example.com/mcp"},
	} {
		_, err := repo.CreateUserSessionConsentResource(ctx, usersessionsrepo.CreateUserSessionConsentResourceParams{
			ProjectID: projectID, Resource: seed.resource, InventoryHash: "inventory", RemoteSetHash: "shared-set",
			ToolSelection: nil, SessionDurationHours: 24, UserSessionConsentID: seed.consentID, OrganizationID: authCtx.ActiveOrganizationID,
		})
		require.NoError(t, err)
	}

	// Another person only approved through the source issuer.
	sourceOnly := urn.NewUserSubject("cimd-merge-source-only")
	sourceOnlyConsent, err := seedUserSessionConsent(t, ctx, ti.conn, sourceClient.ID, sourceOnly)
	require.NoError(t, err)
	session, err := seedUserSessionForClient(t, ctx, ti.conn, sourceID, sourceClient.ID, shared)
	require.NoError(t, err)

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID.String(), TargetID: target.ID})
	require.NoError(t, err)
	require.True(t, preflight.CanMigrate)
	require.Empty(t, preflight.ConflictingClientIds)

	result, err := ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: target.ID, ConfirmedWarningsFingerprint: &preflight.WarningsFingerprint})
	require.NoError(t, err)
	require.True(t, result.SourceDeleted)
	require.Equal(t, 1, result.ClientsMigrated)
	require.Equal(t, 2, result.ConsentsMigrated)
	require.Equal(t, 1, result.SessionsMigrated)

	_, err = repo.GetUserSessionClientByID(ctx, usersessionsrepo.GetUserSessionClientByIDParams{ID: sourceClient.ID, ProjectID: projectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	targetClientAfter, err := repo.GetUserSessionClientByID(ctx, usersessionsrepo.GetUserSessionClientByIDParams{ID: targetClient.ID, ProjectID: projectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, targetID, targetClientAfter.UserSessionIssuerID)

	_, err = repo.GetUserSessionConsentByID(ctx, usersessionsrepo.GetUserSessionConsentByIDParams{ID: sourceShared.ID, ProjectID: projectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	liveResources, err := repo.CountLiveUserSessionConsentResourcesFixture(ctx, usersessionsrepo.CountLiveUserSessionConsentResourcesFixtureParams{ProjectID: projectID, ConsentIds: []uuid.UUID{targetShared.ID}})
	require.NoError(t, err)
	require.Equal(t, int64(2), liveResources, "the target consent keeps its own resource and gains the source-only one")
	orphanedResources, err := repo.CountLiveUserSessionConsentResourcesFixture(ctx, usersessionsrepo.CountLiveUserSessionConsentResourcesFixtureParams{ProjectID: projectID, ConsentIds: []uuid.UUID{sourceShared.ID}})
	require.NoError(t, err)
	require.Zero(t, orphanedResources, "resources left on the retired source consent are retired with it")

	sourceOnlyAfter, err := repo.GetUserSessionConsentByID(ctx, usersessionsrepo.GetUserSessionConsentByIDParams{ID: sourceOnlyConsent.ID, ProjectID: projectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, targetClient.ID, sourceOnlyAfter.UserSessionClientID)
	require.Equal(t, targetID, sourceOnlyAfter.UserSessionIssuerID)
	require.False(t, sourceOnlyAfter.ProjectID.Valid)
	requireOrganizationID(t, ctx, sourceOnlyAfter.OrganizationID)

	sessionAfter, err := repo.GetUserSessionByID(ctx, usersessionsrepo.GetUserSessionByIDParams{ID: session.ID, ProjectID: projectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, targetID, sessionAfter.UserSessionIssuerID)
	require.True(t, sessionAfter.UserSessionClientID.Valid)
	require.Equal(t, targetClient.ID, sessionAfter.UserSessionClientID.UUID)
	require.Equal(t, session.RefreshTokenHash, sessionAfter.RefreshTokenHash, "the session's refresh token keeps working after the merge")
	require.True(t, session.RefreshExpiresAt.Time.Equal(sessionAfter.RefreshExpiresAt.Time))
}

func TestOrganizationUserSessionIssuerMigratePreflightBlocksCimdClientPinnedByIssuanceProfile(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sourceID := seedIssuer(t, ctx, ti, "cimd-pinned-source")
	target, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "cimd-pinned-target", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)

	const documentURL = "https://cimd-pinned.example.com/client-metadata"
	sourceClient, err := seedCimdUserSessionClient(t, ctx, ti.conn, sourceID, documentURL)
	require.NoError(t, err)
	_, err = seedCimdUserSessionClient(t, ctx, ti.conn, uuid.MustParse(target.ID), documentURL)
	require.NoError(t, err)
	_, err = authclientsrepo.New(ti.conn).CreateIssuanceProfile(ctx, authclientsrepo.CreateIssuanceProfileParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		UserSessionIssuerID: sourceID,
		UserSessionClientID: uuid.NullUUID{UUID: sourceClient.ID, Valid: true},
		WorkloadIssuerID:    uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		WorkloadSubject:     pgtype.Text{String: "", Valid: false},
		Name:                "pinned",
		CreatedByUserID:     pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID.String(), TargetID: target.ID})
	require.NoError(t, err)
	require.False(t, preflight.CanMigrate)
	require.Equal(t, []string{documentURL}, preflight.ConflictingClientIds)

	_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: target.ID, ConfirmedWarningsFingerprint: &preflight.WarningsFingerprint})
	requireOopsCode(t, err, oops.CodeConflict)
}
