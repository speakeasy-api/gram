package cliauth_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/cli_auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// A plain org member authorizes and the device agent redeems: the full
// PKCE exchange mints a per-user key carrying the member's identity.
func TestAuthorize_MemberSessionSucceeds(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	verifier, challenge := pkcePair(t)

	authorized, err := ti.service.Authorize(ctx, &gen.AuthorizePayload{
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ProjectSlug:         nil,
		SessionToken:        nil,
	})
	require.NoError(t, err)
	require.NotEmpty(t, authorized.Code)
	require.Positive(t, authorized.ExpiresIn)

	redeemed, err := ti.service.Redeem(ctx, &gen.RedeemPayload{
		Code:         authorized.Code,
		CodeVerifier: verifier,
	})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(redeemed.AccessToken, "gram_local_"))
	require.Equal(t, mockidp.MockUserEmail, redeemed.UserEmail)
	require.NotEmpty(t, redeemed.ProjectSlug)
}

// A Speakeasy admin impersonating an org via the dev-tools override — the
// override targeting the session's active org — is refused enrollment
// outright, even though impersonation grants every RBAC scope (DNO-938):
// enrolling would bind the admin's device to that org's policies and route
// session transcripts there.
func TestAuthorize_ImpersonatingAdminOrgOverrideBlocked(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.IsAdmin = true
	ctx = contextvalues.SetAdminOverrideInContext(ctx, mockidp.MockOrgSlug)

	_, challenge := pkcePair(t)
	_, err := ti.service.Authorize(ctx, &gen.AuthorizePayload{
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ProjectSlug:         nil,
		SessionToken:        nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.ErrorContains(t, err, "impersonating an organization")
}

// A stale gram_admin_override cookie pointing at some OTHER org must not block
// an admin enrolling on their own org: the cookie survives switching back, and
// the refusal only applies when the override targets the active org. The
// membership backstop still guards genuine parked-impersonation sessions.
func TestAuthorize_AdminWithStaleOverrideOnOwnOrgSucceeds(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.IsAdmin = true
	ctx = contextvalues.SetAdminOverrideInContext(ctx, "customer-org")

	_, challenge := pkcePair(t)
	authorized, err := ti.service.Authorize(ctx, &gen.AuthorizePayload{
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ProjectSlug:         nil,
		SessionToken:        nil,
	})
	require.NoError(t, err)
	require.NotEmpty(t, authorized.Code)
}

// A stray admin-override cookie on a NON-admin member's session must not block
// enrollment: the override only ever takes effect for Speakeasy admins.
func TestAuthorize_NonAdminWithOverrideValueSucceeds(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = contextvalues.SetAdminOverrideInContext(ctx, "customer-org")

	_, challenge := pkcePair(t)
	authorized, err := ti.service.Authorize(ctx, &gen.AuthorizePayload{
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ProjectSlug:         nil,
		SessionToken:        nil,
	})
	require.NoError(t, err)
	require.NotEmpty(t, authorized.Code)
}

// A session minted through WorkOS user impersonation is refused enrollment:
// the impersonating admin would otherwise walk away with a durable per-user
// key bearing the impersonated user's identity.
func TestAuthorize_ImpersonatedUserSessionBlocked(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ctx = authenticateSession(t, ctx, ti, sessions.Session{
		SessionID:            uuid.NewString(),
		UserID:               mockidp.MockUserID,
		ActiveOrganizationID: mockidp.MockOrgID,
		WorkOSSessionID:      "",
		ImpersonatorEmail:    "support-admin@speakeasy.com",
	})

	_, challenge := pkcePair(t)
	_, err := ti.service.Authorize(ctx, &gen.AuthorizePayload{
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ProjectSlug:         nil,
		SessionToken:        nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.ErrorContains(t, err, "impersonating a user")
}

// An admin session parked on an org the admin is not a member of — an
// impersonation session that outlived its override cookie — is refused by the
// membership backstop even with no override present on the request.
func TestAuthorize_AdminSessionOnNonMemberOrgBlocked(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	adminID := seedUser(t, ctx, ti.conn, "test-admin-"+uuid.NewString()[:8], "admin@speakeasy.com", true)
	customerOrgID := uuid.NewString()
	seedOrgMetadata(t, ctx, ti.conn, customerOrgID, "Customer Org", "customer-org-"+customerOrgID[:8])

	ctx = authenticateSession(t, ctx, ti, sessions.Session{
		SessionID:            uuid.NewString(),
		UserID:               adminID,
		ActiveOrganizationID: customerOrgID,
		WorkOSSessionID:      "",
		ImpersonatorEmail:    "",
	})

	_, challenge := pkcePair(t)
	_, err := ti.service.Authorize(ctx, &gen.AuthorizePayload{
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ProjectSlug:         nil,
		SessionToken:        nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.ErrorContains(t, err, "requires membership")
}

// The shared demo org has no membership rows by design, and any authenticated
// user may hold a session pointed at it — but a real device must never enroll
// there: its transcripts would land in an org every prospect can read.
func TestAuthorize_DemoOrgSessionBlocked(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	seedOrgMetadata(t, ctx, ti.conn, constants.DemoOrganizationID, "Acme Demo Workspace", "acme-demo")

	ctx = authenticateSession(t, ctx, ti, sessions.Session{
		SessionID:            uuid.NewString(),
		UserID:               mockidp.MockUserID,
		ActiveOrganizationID: constants.DemoOrganizationID,
		WorkOSSessionID:      "",
		ImpersonatorEmail:    "",
	})

	_, challenge := pkcePair(t)
	_, err := ti.service.Authorize(ctx, &gen.AuthorizePayload{
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ProjectSlug:         nil,
		SessionToken:        nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.ErrorContains(t, err, "requires membership")
}
