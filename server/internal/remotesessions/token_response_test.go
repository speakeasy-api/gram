package remotesessions

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestTokenResponseRefreshTokenTimeoutSeconds(t *testing.T) {
	t.Parallel()

	seconds, reported := (tokenResponse{}).RefreshTokenTimeoutSeconds()
	require.False(t, reported)
	require.Zero(t, seconds)

	seconds, reported = (tokenResponse{RefreshExpiresIn: 3600}).RefreshTokenTimeoutSeconds()
	require.True(t, reported)
	require.EqualValues(t, 3600, seconds)

	seconds, reported = (tokenResponse{RefreshTokenExpiresIn: 7200}).RefreshTokenTimeoutSeconds()
	require.True(t, reported)
	require.EqualValues(t, 7200, seconds)

	seconds, reported = (tokenResponse{
		RefreshExpiresIn:      3600,
		RefreshTokenExpiresIn: 7200,
	}).RefreshTokenTimeoutSeconds()
	require.True(t, reported)
	require.EqualValues(t, 3600, seconds)
}

func TestTokenResponseStandardExpirationFields(t *testing.T) {
	t.Parallel()

	zero := int64(0)
	refreshTimeout := int64(3600)
	authorizationLifetime := int64(7200)

	seconds, reported := (tokenResponse{RefreshTokenTimeout: &zero}).RefreshTokenTimeoutSeconds()
	require.True(t, reported)
	require.Zero(t, seconds)

	seconds, reported = (tokenResponse{
		RefreshTokenTimeout: &refreshTimeout,
		RefreshExpiresIn:    1800,
	}).RefreshTokenTimeoutSeconds()
	require.True(t, reported)
	require.EqualValues(t, 3600, seconds)

	seconds, reported = (tokenResponse{
		AuthorizationExpiresIn: &authorizationLifetime,
	}).AuthorizationLifetimeSeconds()
	require.True(t, reported)
	require.EqualValues(t, 7200, seconds)
}

// mintJWT signs claims with a throwaway HMAC key. AccessExpiresAt decodes
// without verification, so the signature only has to be well-formed.
func mintJWT(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-key"))
	require.NoError(t, err)
	return token
}

func TestTokenResponseAccessExpiresAt_ExpiresInGoverns(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	tok := tokenResponse{
		AccessToken: mintJWT(t, jwt.MapClaims{"exp": now.Add(24 * time.Hour).Unix()}),
		ExpiresIn:   3600,
	}

	deadline := tok.AccessExpiresAt(now)
	require.NotNil(t, deadline)
	require.Equal(t, now.Add(time.Hour), *deadline, "expires_in is authoritative even when the JWT carries a different exp")
}

func TestTokenResponseAccessExpiresAt_JWTExpFallback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	exp := now.Add(24 * time.Hour)
	tok := tokenResponse{AccessToken: mintJWT(t, jwt.MapClaims{
		"exp":   exp.Unix(),
		"sub":   "user-123",
		"scope": "read write",
	})}

	deadline := tok.AccessExpiresAt(now)
	require.NotNil(t, deadline)
	require.WithinDuration(t, exp, *deadline, time.Second)
}

func TestTokenResponseAccessExpiresAt_OpaqueToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	require.Nil(t, (tokenResponse{AccessToken: "xoxp-opaque"}).AccessExpiresAt(now))
	require.Nil(t, (tokenResponse{AccessToken: "not.a.jwt"}).AccessExpiresAt(now))
	require.Nil(t, (tokenResponse{}).AccessExpiresAt(now))
}

func TestTokenResponseAccessExpiresAt_JWTWithoutExp(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	tok := tokenResponse{AccessToken: mintJWT(t, jwt.MapClaims{"sub": "user-123"})}
	require.Nil(t, tok.AccessExpiresAt(now))
}

func TestTokenResponseAccessExpiresAt_PastExpIsReported(t *testing.T) {
	t.Parallel()

	// A deadline the provider asserts is reported even when it has already
	// passed. With a refresh grant the request path then refreshes instead of
	// forwarding a token the provider is already rejecting; without one the
	// code exchange declines to record the session at all.
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	exp := now.Add(-time.Minute)
	tok := tokenResponse{AccessToken: mintJWT(t, jwt.MapClaims{"exp": exp.Unix()})}

	deadline := tok.AccessExpiresAt(now)
	require.NotNil(t, deadline)
	require.WithinDuration(t, exp, *deadline, time.Second)
}

func TestTokenResponseAccessExpiresAt_ZeroExpiresInIsUnreported(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	exp := now.Add(24 * time.Hour)
	tok := tokenResponse{
		AccessToken: mintJWT(t, jwt.MapClaims{"exp": exp.Unix()}),
		ExpiresIn:   0,
	}

	deadline := tok.AccessExpiresAt(now)
	require.NotNil(t, deadline, "expires_in: 0 must fall through to the JWT exp rather than expire the token on arrival")
	require.WithinDuration(t, exp, *deadline, time.Second)

	require.Nil(t, (tokenResponse{AccessToken: "xoxp-opaque", ExpiresIn: 0}).AccessExpiresAt(now),
		"expires_in: 0 on an opaque token is no known expiry")
}
