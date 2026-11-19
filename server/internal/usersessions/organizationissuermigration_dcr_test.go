package usersessions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestOrganizationUserSessionIssuerMigrateMergesUnusedDuplicateDcrClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	repo := usersessionsrepo.New(ti.conn)

	sourceID := seedIssuer(t, ctx, ti, "dcr-merge-source")
	target, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "dcr-merge-target", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	targetID := uuid.MustParse(target.ID)

	// One registration copied onto both issuers, as the removed legacy OAuth
	// proxy registration lift did, plus a client only the source holds.
	duplicateClientID := "client_" + uuid.NewString()
	sourceCopy, err := seedUserSessionClient(t, ctx, ti.conn, sourceID, duplicateClientID)
	require.NoError(t, err)
	targetCopy, err := seedUserSessionClient(t, ctx, ti.conn, targetID, duplicateClientID)
	require.NoError(t, err)
	sourceOnly, err := seedUserSessionClient(t, ctx, ti.conn, sourceID, "client_"+uuid.NewString())
	require.NoError(t, err)

	// The target's copy is in use; only the source's copy must be unused. A
	// revoked sign-in on the source copy does not count as a use.
	targetSession, err := seedUserSessionForClient(t, ctx, ti.conn, targetID, targetCopy.ID, urn.NewUserSubject("dcr-merge-user"))
	require.NoError(t, err)
	revokedSession, err := seedUserSessionForClient(t, ctx, ti.conn, sourceID, sourceCopy.ID, urn.NewUserSubject("dcr-merge-revoked-user"))
	require.NoError(t, err)
	_, err = repo.RevokeUserSession(ctx, usersessionsrepo.RevokeUserSessionParams{ID: revokedSession.ID, ProjectID: projectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID.String(), TargetID: target.ID})
	require.NoError(t, err)
	require.True(t, preflight.CanMigrate)
	require.Empty(t, preflight.ConflictingClientIds)
	require.Equal(t, []string{duplicateClientID}, preflight.MergedClientIds)

	result, err := ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: target.ID, ConfirmedWarningsFingerprint: &preflight.WarningsFingerprint})
	require.NoError(t, err)
	require.True(t, result.SourceDeleted)
	require.Equal(t, 2, result.ClientsMigrated, "the merged copy and the moved client both count as migrated")

	_, err = repo.GetUserSessionClientByID(ctx, usersessionsrepo.GetUserSessionClientByIDParams{ID: sourceCopy.ID, ProjectID: projectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.ErrorIs(t, err, pgx.ErrNoRows, "the source copy is retired")
	targetCopyAfter, err := repo.GetUserSessionClientByID(ctx, usersessionsrepo.GetUserSessionClientByIDParams{ID: targetCopy.ID, ProjectID: projectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, targetID, targetCopyAfter.UserSessionIssuerID)
	require.Equal(t, targetCopy.ClientSecretHash, targetCopyAfter.ClientSecretHash, "the target copy keeps its own credential")
	sourceOnlyAfter, err := repo.GetUserSessionClientByID(ctx, usersessionsrepo.GetUserSessionClientByIDParams{ID: sourceOnly.ID, ProjectID: projectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, targetID, sourceOnlyAfter.UserSessionIssuerID, "a client only the source holds moves as before")

	targetSessionAfter, err := repo.GetUserSessionByID(ctx, usersessionsrepo.GetUserSessionByIDParams{ID: targetSession.ID, ProjectID: projectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.True(t, targetSessionAfter.UserSessionClientID.Valid)
	require.Equal(t, targetCopy.ID, targetSessionAfter.UserSessionClientID.UUID)

	migrateAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerMigrate)
	require.NoError(t, err)
	metadata, err := audittest.DecodeAuditData(migrateAudit.Metadata)
	require.NoError(t, err)
	require.Equal(t, []any{duplicateClientID}, metadata["merged_client_ids"])
}

func TestOrganizationUserSessionIssuerMigratePreflightSeparatesMergedAndConflictingDcrClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	source, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "dcr-mixed-source", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	target, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "dcr-mixed-target", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	sourceID, targetID := uuid.MustParse(source.ID), uuid.MustParse(target.ID)

	// Fixed ids so the expected order of each list is unambiguous.
	const usedClientID, unusedClientID = "client_00000000-0000-0000-0000-00000000000a", "client_00000000-0000-0000-0000-00000000000b"
	usedCopy, err := seedUserSessionClient(t, ctx, ti.conn, sourceID, usedClientID)
	require.NoError(t, err)
	_, err = seedUserSessionClient(t, ctx, ti.conn, targetID, usedClientID)
	require.NoError(t, err)
	_, err = seedUserSessionClient(t, ctx, ti.conn, sourceID, unusedClientID)
	require.NoError(t, err)
	_, err = seedUserSessionClient(t, ctx, ti.conn, targetID, unusedClientID)
	require.NoError(t, err)
	_, err = seedUserSessionForClient(t, ctx, ti.conn, sourceID, usedCopy.ID, urn.NewUserSubject("dcr-mixed-user"))
	require.NoError(t, err)

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: source.ID, TargetID: target.ID})
	require.NoError(t, err)
	require.False(t, preflight.CanMigrate)
	require.Equal(t, []string{usedClientID}, preflight.ConflictingClientIds)
	require.Equal(t, []string{unusedClientID}, preflight.MergedClientIds)
}

func TestOrganizationUserSessionIssuerMigratePreflightBlocksDuplicateDcrClientWithConsent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	source, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "dcr-consent-source", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	target, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "dcr-consent-target", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)

	duplicateClientID := "client_" + uuid.NewString()
	sourceCopy, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(source.ID), duplicateClientID)
	require.NoError(t, err)
	_, err = seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(target.ID), duplicateClientID)
	require.NoError(t, err)
	_, err = seedUserSessionConsent(t, ctx, ti.conn, sourceCopy.ID, urn.NewUserSubject("dcr-consent-user"))
	require.NoError(t, err)

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: source.ID, TargetID: target.ID})
	require.NoError(t, err)
	require.False(t, preflight.CanMigrate)
	require.Equal(t, []string{duplicateClientID}, preflight.ConflictingClientIds)
	require.Empty(t, preflight.MergedClientIds)

	_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: source.ID, TargetID: target.ID, ConfirmedWarningsFingerprint: &preflight.WarningsFingerprint})
	requireOopsCode(t, err, oops.CodeConflict)
}

// A session whose refresh token has expired still blocks: nothing soft-deletes
// expired sessions, so the row is the only record that the copy was used.
func TestOrganizationUserSessionIssuerMigratePreflightBlocksDuplicateDcrClientWithExpiredSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	source, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "dcr-expired-source", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	target, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "dcr-expired-target", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	sourceID := uuid.MustParse(source.ID)

	duplicateClientID := "client_" + uuid.NewString()
	sourceCopy, err := seedUserSessionClient(t, ctx, ti.conn, sourceID, duplicateClientID)
	require.NoError(t, err)
	_, err = seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(target.ID), duplicateClientID)
	require.NoError(t, err)
	past := time.Now().Add(-48 * time.Hour)
	_, err = seedUserSessionFull(t, ctx, ti.conn, sourceID, uuid.NullUUID{UUID: sourceCopy.ID, Valid: true}, urn.NewUserSubject("dcr-expired-user"), past, past)
	require.NoError(t, err)

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: source.ID, TargetID: target.ID})
	require.NoError(t, err)
	require.False(t, preflight.CanMigrate)
	require.Equal(t, []string{duplicateClientID}, preflight.ConflictingClientIds)
	require.Empty(t, preflight.MergedClientIds)
}

func TestOrganizationUserSessionIssuerMigratePreflightBlocksDcrClientPinnedByIssuanceProfile(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sourceID := seedIssuer(t, ctx, ti, "dcr-pinned-source")
	target, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "dcr-pinned-target", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)

	duplicateClientID := "client_" + uuid.NewString()
	sourceCopy, err := seedUserSessionClient(t, ctx, ti.conn, sourceID, duplicateClientID)
	require.NoError(t, err)
	_, err = seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(target.ID), duplicateClientID)
	require.NoError(t, err)
	_, err = authclientsrepo.New(ti.conn).CreateIssuanceProfile(ctx, authclientsrepo.CreateIssuanceProfileParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		UserSessionIssuerID: sourceID,
		UserSessionClientID: uuid.NullUUID{UUID: sourceCopy.ID, Valid: true},
		WorkloadIssuerID:    uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		WorkloadSubject:     pgtype.Text{String: "", Valid: false},
		Name:                "pinned",
		CreatedByUserID:     pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID.String(), TargetID: target.ID})
	require.NoError(t, err)
	require.False(t, preflight.CanMigrate)
	require.Equal(t, []string{duplicateClientID}, preflight.ConflictingClientIds)
	require.Empty(t, preflight.MergedClientIds)
}
