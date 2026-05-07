package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestDeleteUserSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "to-delete",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionIssuerDelete)
	require.NoError(t, err)

	err = ti.service.DeleteUserSessionIssuer(ctx, &gen.DeleteUserSessionIssuerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionIssuerDelete)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	// Subsequent get returns not-found.
	id := created.ID
	_, err = ti.service.GetUserSessionIssuer(ctx, &gen.GetUserSessionIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               &id,
		Slug:             nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteUserSessionIssuer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.DeleteUserSessionIssuer(ctx, &gen.DeleteUserSessionIssuerPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteUserSessionIssuer_BadID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.DeleteUserSessionIssuer(ctx, &gen.DeleteUserSessionIssuerPayload{
		ID:               "not-a-uuid",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestDeleteUserSessionIssuer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "rbac-delete",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Read-only grant on the project — delete must be denied.
	ctx = withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	)

	err = ti.service.DeleteUserSessionIssuer(ctx, &gen.DeleteUserSessionIssuerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

// TestDeleteUserSessionIssuer_CascadesToSessionsAndConsents verifies that
// deleting an issuer soft-deletes its dependent user_sessions and
// user_session_consents. Only the parent issuer delete is audit-logged —
// the child cascade is intentionally silent to keep the audit log readable
// when an issuer with many sessions is torn down.
func TestDeleteUserSessionIssuer_CascadesToSessionsAndConsents(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	created, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "cascade-parent",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	issuerID := uuid.MustParse(created.ID)

	// Seed children directly through the SQLc repo (the management API lacks
	// public writers — sessions and clients are written by the OAuth surface
	// in milestone #2).
	clientRow, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, "client-1")
	require.NoError(t, err)
	consent1, err := seedUserSessionConsent(t, ctx, ti.conn, clientRow.ID, urn.NewUserSubject("cascade-1"))
	require.NoError(t, err)
	consent2, err := seedUserSessionConsent(t, ctx, ti.conn, clientRow.ID, urn.NewUserSubject("cascade-2"))
	require.NoError(t, err)
	sess, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewUserSubject("cascade-1"))
	require.NoError(t, err)

	beforeSessions, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionRevoke)
	require.NoError(t, err)
	beforeConsents, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionConsentRevoke)
	require.NoError(t, err)

	err = ti.service.DeleteUserSessionIssuer(ctx, &gen.DeleteUserSessionIssuerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	// Children must be soft-deleted by the cascade — the deleted-row filter
	// on each Get* repo query yields ErrNoRows once the cascade has run.
	r := repo.New(ti.conn)
	_, err = r.GetUserSessionByID(ctx, repo.GetUserSessionByIDParams{ID: sess.ID, ProjectID: projectID})
	require.ErrorIs(t, err, pgx.ErrNoRows, "user_session should be soft-deleted by cascade")
	_, err = r.GetUserSessionConsentByID(ctx, repo.GetUserSessionConsentByIDParams{ID: consent1.ID, ProjectID: projectID})
	require.ErrorIs(t, err, pgx.ErrNoRows, "user_session_consent should be soft-deleted by cascade")
	_, err = r.GetUserSessionConsentByID(ctx, repo.GetUserSessionConsentByIDParams{ID: consent2.ID, ProjectID: projectID})
	require.ErrorIs(t, err, pgx.ErrNoRows, "user_session_consent should be soft-deleted by cascade")

	// The cascade must not emit per-row child audit events.
	afterSessions, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionRevoke)
	require.NoError(t, err)
	afterConsents, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionConsentRevoke)
	require.NoError(t, err)
	require.Equal(t, beforeSessions, afterSessions, "issuer cascade must not emit user_session revoke audit events")
	require.Equal(t, beforeConsents, afterConsents, "issuer cascade must not emit user_session_consent revoke audit events")
}
