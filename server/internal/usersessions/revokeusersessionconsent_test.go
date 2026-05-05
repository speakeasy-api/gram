package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_consents"
	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRevokeUserSessionConsent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Slug:               "revoke-consent-issuer",
		AuthnChallengeMode: "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	client, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "revoke-consent-client")
	require.NoError(t, err)

	consent, err := seedUserSessionConsent(t, ctx, ti.conn, client.ID, urn.NewUserSubject("revoke-target"))
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionConsentRevoke)
	require.NoError(t, err)

	err = ti.service.RevokeUserSessionConsent(ctx, &gen.RevokeUserSessionConsentPayload{
		ID:               consent.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionConsentRevoke)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	// A second revoke is a no-op against an already soft-deleted row, so the
	// row is no longer visible — the same id reads as not-found.
	err = ti.service.RevokeUserSessionConsent(ctx, &gen.RevokeUserSessionConsentPayload{
		ID:               consent.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestRevokeUserSessionConsent_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.RevokeUserSessionConsent(ctx, &gen.RevokeUserSessionConsentPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestRevokeUserSessionConsent_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Slug:               "rbac-revoke-consent",
		AuthnChallengeMode: "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	client, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "rbac-revoke-client")
	require.NoError(t, err)

	consent, err := seedUserSessionConsent(t, ctx, ti.conn, client.ID, urn.NewUserSubject("rbac-target"))
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Read-only on the project; revoke needs write on the owning issuer.
	ctx = withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	)

	err = ti.service.RevokeUserSessionConsent(ctx, &gen.RevokeUserSessionConsentPayload{
		ID:               consent.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
