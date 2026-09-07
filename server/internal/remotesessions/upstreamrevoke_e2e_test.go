// upstreamrevoke_e2e_test.go drives the RFC 7009 client side against a live
// dev-idp acting as the upstream authorization server.
//
// The httptest-based tests in upstreamrevoke_test.go assert what Gram *sends* —
// a spy records the form and stops there. That leaves the half that actually
// matters unproven: whether a real authorization server accepts the request and
// treats the credential as dead afterwards. A spy will happily record a
// perfectly-shaped POST that every real AS would reject.
//
// So this file asserts the upstream's observable state instead: each test reads
// dev-idp's own tokens table and requires `revoked_at` to go from unset to
// stamped across the call. That before/after delta is proof the revocation
// landed, not proof we formatted it nicely. Each test also reads the revocation
// endpoint out of dev-idp's RFC 8414 metadata document rather than hardcoding a
// path, so drift between what issuers advertise and what Gram calls shows up
// here.

package remotesessions_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
)

// devIdpRevocationEndpoint pulls revocation_endpoint out of the dev-idp's
// OAuth 2.1 metadata document. Reading it rather than composing it is the
// point: this is the same field issuerrefresh discovers in production, so if
// the advertised key ever moves, this test fails instead of silently passing
// against a hardcoded URL.
func devIdpRevocationEndpoint(t *testing.T, inst *devidptest.Instance) string {
	t.Helper()

	var doc struct {
		RevocationEndpoint string `json:"revocation_endpoint"`
	}
	require.NoError(t, json.Unmarshal(inst.OAuth21Metadata(t), &doc))
	require.NotEmpty(t, doc.RevocationEndpoint, "dev-idp must advertise a revocation_endpoint")

	return doc.RevocationEndpoint
}

// tokenRevokedAt reads dev-idp's own view of the token straight from its tokens
// table, via the Instance.DB escape hatch documented for exactly this. Going
// around Instance.Repo is the point: its sqlc surface only offers
// GetActiveToken, which collapses "revoked", "expired", and "never existed"
// into one no-rows result — too coarse here, where telling revocation apart
// from the row simply not being there is the whole assertion.
//
// Counting rather than selecting the column is deliberate on two counts. A pair
// of COUNTs always yields exactly one row, so there is no no-rows case to branch
// on — which keeps the helper clear of database/sql's ErrNoRows, the wrong
// sentinel to reach for in this tree anyway (dev-idp is SQLite; everything else
// here is pgx). And COUNT(revoked_at) counts only non-NULL values, so it answers
// "was this revoked?" as an integer, sidestepping SQLite handing back an
// untyped string for a DATETIME read through an aggregate. The timestamp itself
// is never needed — only whether one was stamped.
func tokenRevokedAt(t *testing.T, ctx context.Context, inst *devidptest.Instance, token string) (found bool, revoked bool) {
	t.Helper()

	var rows, revokedCount int
	err := inst.DB.QueryRowContext(ctx,
		"SELECT COUNT(*), COUNT(revoked_at) FROM tokens WHERE token = ? AND mode = ?",
		token, devidptest.OAuth21Mode,
	).Scan(&rows, &revokedCount)
	require.NoError(t, err, "read token row from dev-idp")

	return rows > 0, revokedCount > 0
}

func requireTokenActive(t *testing.T, ctx context.Context, inst *devidptest.Instance, token string) {
	t.Helper()

	found, revoked := tokenRevokedAt(t, ctx, inst, token)
	require.True(t, found, "token should exist in dev-idp before Gram revokes it")
	require.False(t, revoked, "token should still be live before Gram revokes it")
}

func requireTokenRevoked(t *testing.T, ctx context.Context, inst *devidptest.Instance, token string) {
	t.Helper()

	found, revoked := tokenRevokedAt(t, ctx, inst, token)
	require.True(t, found, "dev-idp must still hold the row — revocation stamps it, it does not delete it")
	require.True(t, revoked, "dev-idp must have stamped revoked_at after Gram's RFC 7009 call")
}

// seedDevIdpRefreshToken inserts a refresh token the dev-idp will honour, and
// returns the opaque string so the Gram-side session can be seeded to wrap it.
func seedDevIdpRefreshToken(t *testing.T, ctx context.Context, inst *devidptest.Instance, token string) {
	t.Helper()

	devidptest.CreateRefreshToken(t, ctx, inst.Repo, devidptest.RefreshTokenOpts{
		Token:     token,
		Mode:      devidptest.OAuth21Mode,
		UserID:    inst.DefaultUser.ID,
		ClientID:  devidptest.DefaultClientID,
		Scope:     "openid",
		ExpiresAt: time.Now().Add(24 * time.Hour),
	})
}

// The load-bearing case: a refresh token is the credential that keeps minting
// replacements, so RFC 7009 §2.1 makes it the one worth destroying. Asserting
// against a real AS is what separates "we sent a revocation" from "the
// credential is dead".
func TestRevokeRemoteSession_E2E_DevIdpDropsRefreshToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	inst := devidptest.Launch(t, devidptest.LaunchOpts{EnableMockWorkos: false, Key: nil})

	// The Gram-side fixture derives its refresh-token string from the slug, so
	// the dev-idp row has to use the same value to be the same credential.
	slug := "e2e-devidp-refresh"
	refreshToken := slug + "-refresh"
	seedDevIdpRefreshToken(t, ctx, inst, refreshToken)
	requireTokenActive(t, ctx, inst, refreshToken)

	fx := seedRevocableSession(t, ctx, ti, slug, devIdpRevocationEndpoint(t, inst), "s3cret", true)
	require.Equal(t, refreshToken, fx.refreshToken, "fixture must wrap the token seeded into dev-idp")

	err := ti.service.RevokeRemoteSession(ctx, &gen.RevokeRemoteSessionPayload{ID: fx.sessionID.String()})
	require.NoError(t, err)

	requireTokenRevoked(t, ctx, inst, refreshToken)
	requireSessionRevoked(t, ctx, ti, fx)
}

// A session that never received a refresh token still holds an access token the
// upstream should drop. This also covers the public-client path, where RFC 6749
// §2.3 leaves the request authenticated by client_id alone — a real AS is the
// only way to confirm that shape is accepted rather than 401'd.
func TestRevokeRemoteSession_E2E_DevIdpDropsAccessTokenForPublicClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	inst := devidptest.Launch(t, devidptest.LaunchOpts{EnableMockWorkos: false, Key: nil})

	slug := "e2e-devidp-access"
	accessToken := slug + "-access"
	seedDevIdpRefreshToken(t, ctx, inst, accessToken)
	requireTokenActive(t, ctx, inst, accessToken)

	fx := seedRevocableSession(t, ctx, ti, slug, devIdpRevocationEndpoint(t, inst), "", false)
	require.Equal(t, accessToken, fx.accessToken, "fixture must wrap the token seeded into dev-idp")
	require.Empty(t, fx.refreshToken)

	err := ti.service.RevokeRemoteSession(ctx, &gen.RevokeRemoteSessionPayload{ID: fx.sessionID.String()})
	require.NoError(t, err)

	requireTokenRevoked(t, ctx, inst, accessToken)
	requireSessionRevoked(t, ctx, ti, fx)
}

// The best-effort contract, proven against a real AS rather than a stub: a
// token the upstream has never heard of still returns 200 per RFC 7009 §2.2,
// and the local revoke commits either way. This is the case that would surface
// a regression where an upstream's response is allowed to fail the caller.
func TestRevokeRemoteSession_E2E_DevIdpUnknownTokenStillRevokesLocally(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	inst := devidptest.Launch(t, devidptest.LaunchOpts{EnableMockWorkos: false, Key: nil})

	// Deliberately seed nothing into dev-idp: the session wraps a credential
	// the AS has no record of.
	fx := seedRevocableSession(t, ctx, ti, "e2e-devidp-unknown-"+uuid.NewString()[:8], devIdpRevocationEndpoint(t, inst), "s3cret", true)

	err := ti.service.RevokeRemoteSession(ctx, &gen.RevokeRemoteSessionPayload{ID: fx.sessionID.String()})
	require.NoError(t, err, "an upstream that does not recognise the token must not fail the caller")

	requireSessionRevoked(t, ctx, ti, fx)
}
