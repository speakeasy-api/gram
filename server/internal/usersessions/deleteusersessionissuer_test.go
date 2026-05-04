package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestDeleteUserSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Slug:               "to-delete",
		AuthnChallengeMode: "chain",
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
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Slug:               "rbac-delete",
		AuthnChallengeMode: "chain",
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

// TestDeleteUserSessionIssuer_CascadesToSessionsAndConsents covers the
// "audit a cascading delete" pattern: the parent delete must produce one
// child audit event per soft-deleted user_session and user_session_consent.
func TestDeleteUserSessionIssuer_CascadesToSessionsAndConsents(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Slug:               "cascade-parent",
		AuthnChallengeMode: "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	issuerID := uuid.MustParse(created.ID)

	// Seed children directly through the SQLc repo (the management API lacks
	// public writers — sessions and clients are written by the OAuth surface
	// in milestone #2).
	clientRow, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, "client-1")
	require.NoError(t, err)
	_, err = seedUserSessionConsent(t, ctx, ti.conn, clientRow.ID, urn.NewUserSubject("cascade-1"))
	require.NoError(t, err)
	_, err = seedUserSessionConsent(t, ctx, ti.conn, clientRow.ID, urn.NewUserSubject("cascade-2"))
	require.NoError(t, err)
	_, err = seedUserSession(t, ctx, ti.conn, issuerID, urn.NewUserSubject("cascade-1"))
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

	afterSessions, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionRevoke)
	require.NoError(t, err)
	afterConsents, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionConsentRevoke)
	require.NoError(t, err)
	require.Equal(t, beforeSessions+1, afterSessions, "one revoke audit per cascaded user_session")
	require.Equal(t, beforeConsents+2, afterConsents, "one revoke audit per cascaded user_session_consent")
}
