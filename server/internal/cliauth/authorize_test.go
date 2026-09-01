package cliauth_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/hooks/delegation"
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

func TestRedeem_InvalidProofEnrollmentDoesNotCreateOrphanAPIKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	verifier, challenge := pkcePair(t)
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	encodedPublicKey := delegation.EncodePublicKey(publicKey)
	contractVersion := delegation.ContractVersion
	authorized, err := ti.service.Authorize(ctx, &gen.AuthorizePayload{
		CodeChallenge: challenge, CodeChallengeMethod: "S256",
		ProofPublicKey: &encodedPublicKey, DelegationContractVersion: &contractVersion,
	})
	require.NoError(t, err)

	var before int
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM api_keys`).Scan(&before)) //nolint:glint // notestingrawsql: bounded integration assertion verifies no orphan side effect
	key := "cliauth:code:" + authorized.Code
	raw, err := ti.redis.Get(ctx, key).Bytes()
	require.NoError(t, err)
	var record map[string]any
	require.NoError(t, json.Unmarshal(raw, &record))
	record["proof_public_key"] = "invalid"
	raw, err = json.Marshal(record)
	require.NoError(t, err)
	require.NoError(t, ti.redis.Set(ctx, key, raw, 0).Err())

	_, err = ti.service.Redeem(ctx, &gen.RedeemPayload{Code: authorized.Code, CodeVerifier: verifier})
	requireOopsCode(t, err, oops.CodeUnauthorized)
	var after int
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM api_keys`).Scan(&after)) //nolint:glint // notestingrawsql: bounded integration assertion verifies no orphan side effect
	require.Equal(t, before, after, "proof public-key validation must precede the API-key side effect")
}

func TestAuthorize_RejectsStaleSessionMembership(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.UserID = "removed-user-" + uuid.NewString()

	_, challenge := pkcePair(t)
	_, err := ti.service.Authorize(ctx, &gen.AuthorizePayload{CodeChallenge: challenge, CodeChallengeMethod: "S256"})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestRedeem_RevalidatesMembershipAfterAtomicConsume(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	verifier, challenge := pkcePair(t)
	authorized, err := ti.service.Authorize(ctx, &gen.AuthorizePayload{CodeChallenge: challenge, CodeChallengeMethod: "S256"})
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err = ti.conn.Exec(ctx, `UPDATE organization_user_relationships SET deleted_at = clock_timestamp() WHERE organization_id = $1 AND user_id = $2`, authCtx.ActiveOrganizationID, authCtx.UserID) //nolint:glint // notestingrawsql: isolated integration fixture must simulate membership removal
	require.NoError(t, err)
	t.Cleanup(func() {
		_, restoreErr := ti.conn.Exec(context.Background(), `UPDATE organization_user_relationships SET deleted_at = NULL WHERE organization_id = $1 AND user_id = $2`, authCtx.ActiveOrganizationID, authCtx.UserID) //nolint:glint // notestingrawsql: restore isolated membership fixture during cleanup
		require.NoError(t, restoreErr)
	})

	_, err = ti.service.Redeem(ctx, &gen.RedeemPayload{Code: authorized.Code, CodeVerifier: verifier})
	requireOopsCode(t, err, oops.CodeUnauthorized)
	_, err = ti.conn.Exec(ctx, `UPDATE organization_user_relationships SET deleted_at = NULL WHERE organization_id = $1 AND user_id = $2`, authCtx.ActiveOrganizationID, authCtx.UserID) //nolint:glint // notestingrawsql: isolated integration fixture restores membership to test consumed-code replay
	require.NoError(t, err)
	_, err = ti.service.Redeem(ctx, &gen.RedeemPayload{Code: authorized.Code, CodeVerifier: verifier})
	requireOopsCode(t, err, oops.CodeUnauthorized)
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
