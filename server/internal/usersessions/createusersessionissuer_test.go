package usersessions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateUserSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionIssuerCreate)
	require.NoError(t, err)

	created, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "issuer-one",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	require.Equal(t, "issuer-one", created.Slug)
	require.Equal(t, "chain", created.AuthnChallengeMode)
	require.Equal(t, 24, created.SessionDurationHours)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestCreateUserSessionIssuer_BadDuration(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "bad-duration",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 0,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateUserSessionIssuer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Hold only project:read; create requires project:write.
	ctx = withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	)

	_, err := ti.service.CreateUserSessionIssuer(ctx, &gen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "rbac-denied",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
