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
)

func TestUpdateUserSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "to-update",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionIssuerUpdate)
	require.NoError(t, err)

	newMode := "interactive"
	newDur := 12
	updated, err := ti.service.UpdateUserSessionIssuer(ctx, &gen.UpdateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		ID:                   created.ID,
		Slug:                 nil,
		AuthnChallengeMode:   &newMode,
		SessionDurationHours: &newDur,
	})
	require.NoError(t, err)
	require.Equal(t, "to-update", updated.Slug, "unset slug must be preserved")
	require.Equal(t, "interactive", updated.AuthnChallengeMode)
	require.Equal(t, 12, updated.SessionDurationHours)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestUpdateUserSessionIssuer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	mode := "chain"
	_, err := ti.service.UpdateUserSessionIssuer(ctx, &gen.UpdateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		ID:                   uuid.NewString(),
		Slug:                 nil,
		AuthnChallengeMode:   &mode,
		SessionDurationHours: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateUserSessionIssuer_BadID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.UpdateUserSessionIssuer(ctx, &gen.UpdateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		ID:                   "not-a-uuid",
		Slug:                 nil,
		AuthnChallengeMode:   nil,
		SessionDurationHours: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateUserSessionIssuer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "rbac-update",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Read-only grant on the project — write must be denied.
	ctx = withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	)

	mode := "interactive"
	_, err = ti.service.UpdateUserSessionIssuer(ctx, &gen.UpdateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		ID:                   created.ID,
		Slug:                 nil,
		AuthnChallengeMode:   &mode,
		SessionDurationHours: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
