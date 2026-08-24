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

// A plain org member completes the full authorize→redeem PKCE exchange.
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

// An admin impersonating the active org via a support session is refused.
func TestAuthorize_ImpersonatingAdminSupportSessionBlocked(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.IsAdmin = true
	authCtx.SupportOrganizationID = authCtx.ActiveOrganizationID
	ctx = contextvalues.WithValidatedSupportSession(ctx, authCtx)

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

// A support session opened against another org must not block an admin's
// own-org enrollment.
func TestAuthorize_AdminWithSupportSessionElsewhereOnOwnOrgSucceeds(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.IsAdmin = true
	authCtx.SupportOrganizationID = "customer-org"
	ctx = contextvalues.WithValidatedSupportSession(ctx, authCtx)

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

// Support authority only takes effect for admins; a non-admin member with a
// support org recorded on the session still enrolls fine.
func TestAuthorize_NonAdminWithSupportOrgSucceeds(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.SupportOrganizationID = authCtx.ActiveOrganizationID
	ctx = contextvalues.WithValidatedSupportSession(ctx, authCtx)

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

// A WorkOS user-impersonation session is refused.
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

// An admin parked on a non-member org without a live support session never
// reaches the handler: session authentication itself refuses. Pinned here so a
// relaxation upstream shows up as a cliauth failure — the membership backstop
// in Authorize is the second line of defense, not the first.
func TestAuthorize_AdminSessionOnNonMemberOrgRefusedAtAuth(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	adminID := seedUser(t, ctx, ti.conn, "test-admin-"+uuid.NewString()[:8], "admin@speakeasy.com", true)
	customerOrgID := uuid.NewString()
	seedOrgMetadata(t, ctx, ti.conn, customerOrgID, "Customer Org", "customer-org-"+customerOrgID[:8])

	session := sessions.Session{
		SessionID:            uuid.NewString(),
		UserID:               adminID,
		ActiveOrganizationID: customerOrgID,
		WorkOSSessionID:      "",
		ImpersonatorEmail:    "",
	}
	require.NoError(t, ti.sessionManager.StoreSession(ctx, session))

	_, err := ti.sessionManager.Authenticate(ctx, session.SessionID)
	requireOopsCode(t, err, oops.CodeForbidden)
}

// The shared demo org has no memberships; devices must never enroll there.
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
