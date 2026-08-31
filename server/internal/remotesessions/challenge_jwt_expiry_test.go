// challenge_jwt_expiry_test.go covers providers that omit expires_in but issue
// JWT access tokens: the token's exp claim must become access_expires_at on
// both the code exchange and the refresh grant, so the lazy request path
// refreshes instead of forwarding a token the provider already rejects. The
// mock upstream answers both grants with a JWT access token, a refresh token,
// a non-standard issued_at, and no expires_in.

package remotesessions_test

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestRemoteLoginCallback_NoExpiresIn_JWTAccessToken_StoresExp(t *testing.T) {
	t.Parallel()

	exp := time.Now().Add(24 * time.Hour)
	access := mintJWTAccessToken(t, exp)
	ctx, env := newSyntheticExpiryEnv(t, "jwt-exp", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(jwtProviderTokenBody(access, "refresh-initial")))
	})

	require.True(t, env.session.AccessExpiresAt.Valid, "JWT exp must populate access_expires_at when expires_in is absent")
	require.WithinDuration(t, exp, env.session.AccessExpiresAt.Time, time.Second)
	require.True(t, env.session.RefreshTokenEncrypted.Valid, "refresh token must be persisted")

	resolved, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, access, resolved, "a token before its exp is served as-is")
}

func TestRemoteLoginCallback_JWTAccessToken_RefreshesOnceExpPasses(t *testing.T) {
	t.Parallel()

	initial := mintJWTAccessToken(t, time.Now().Add(24*time.Hour))
	rotatedExp := time.Now().Add(48 * time.Hour)
	rotated := mintJWTAccessToken(t, rotatedExp)
	var refreshCount atomic.Int64
	ctx, env := newSyntheticExpiryEnv(t, "jwt-refresh", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") == "refresh_token" {
			refreshCount.Add(1)
			_, _ = w.Write([]byte(jwtProviderTokenBody(rotated, "refresh-rotated")))
			return
		}
		_, _ = w.Write([]byte(jwtProviderTokenBody(initial, "refresh-initial")))
	})

	// Stand in for the stored JWT reaching its exp.
	require.NoError(t, env.q.SetRemoteSessionAccessExpiresAt(ctx, repo.SetRemoteSessionAccessExpiresAtParams{
		ID:              env.session.ID,
		ProjectID:       conv.ToNullUUID(env.projectID),
		AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Minute)),
	}))

	resolved, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, rotated, resolved)
	require.Equal(t, int64(1), refreshCount.Load())

	// The refresh grant response omits expires_in too, so the rotated token's
	// exp must be the new deadline rather than NULL.
	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	require.True(t, session.AccessExpiresAt.Valid)
	require.WithinDuration(t, rotatedExp, session.AccessExpiresAt.Time, time.Second)
}

func TestRemoteLoginCallback_JWTAccessToken_PastExpWithRefreshToken_RefreshesOnFirstUse(t *testing.T) {
	t.Parallel()

	// A provider that pins exp to its own session can hand out a token that
	// is already past it. With a refresh grant the exchange is recorded and
	// the first resolution recovers by refreshing.
	expired := mintJWTAccessToken(t, time.Now().Add(-time.Minute))
	rotated := mintJWTAccessToken(t, time.Now().Add(24*time.Hour))
	var refreshCount atomic.Int64
	ctx, env := newSyntheticExpiryEnv(t, "jwt-past-exp-refresh", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") == "refresh_token" {
			refreshCount.Add(1)
			_, _ = w.Write([]byte(jwtProviderTokenBody(rotated, "refresh-rotated")))
			return
		}
		_, _ = w.Write([]byte(jwtProviderTokenBody(expired, "refresh-initial")))
	})
	require.True(t, env.session.AccessExpiresAt.Valid)
	require.False(t, env.session.AccessExpiresAt.Time.After(time.Now()), "the persisted deadline is the past exp")

	resolved, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, rotated, resolved)
	require.Equal(t, int64(1), refreshCount.Load())
}

func TestRemoteLoginCallback_JWTAccessToken_PastExpWithoutRefreshToken_IsRejected(t *testing.T) {
	t.Parallel()

	// With no refresh grant nothing can recover an already-expired token.
	// Recording the row would show the session as connected while every
	// resolution answered with a reconnect prompt, so the exchange is
	// declined instead.
	expired := mintJWTAccessToken(t, time.Now().Add(-time.Minute))
	ctx, env, _, err := driveSyntheticLogin(t, "jwt-past-exp-no-refresh", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + expired + `","scope":"read write","token_type":"Bearer"}`))
	})
	var rejected *oops.ShareableError
	require.ErrorAs(t, err, &rejected)
	require.Equal(t, oops.CodeUnauthorized, rejected.Code)

	_, err = env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "a session that cannot be used is never recorded as connected")
}

func TestRemoteLoginCallback_JWTAccessToken_ExpInsideSkewWithoutRefreshToken_IsRejected(t *testing.T) {
	t.Parallel()

	// The bar is the same margin the request path refreshes at: a token with
	// less than the skew left and nothing to renew it is not a usable session.
	expiring := mintJWTAccessToken(t, time.Now().Add(remotesessions.AccessTokenExpirySkew/2))
	ctx, env, _, err := driveSyntheticLogin(t, "jwt-skew-exp-no-refresh", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + expiring + `","scope":"read write","token_type":"Bearer"}`))
	})
	var rejected *oops.ShareableError
	require.ErrorAs(t, err, &rejected)
	require.Equal(t, oops.CodeUnauthorized, rejected.Code)

	_, err = env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

// jwtProviderTokenBody is a token response with no expires_in: the only expiry
// signal is the JWT's own exp claim.
func jwtProviderTokenBody(accessToken, refreshToken string) string {
	return `{"access_token":"` + accessToken + `","refresh_token":"` + refreshToken + `",` +
		`"scope":"read write","token_type":"Bearer","issued_at":"1787000000000"}`
}

// mintJWTAccessToken signs a minimal access-token claim set with a throwaway
// HMAC key. The server decodes exp without verifying the signature, so the
// token only has to be well-formed.
func mintJWTAccessToken(t *testing.T, exp time.Time) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp":   exp.Unix(),
		"sub":   "user-123",
		"scope": "read write",
	}).SignedString([]byte("test-key"))
	require.NoError(t, err)
	return token
}
