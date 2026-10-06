package remotesessions

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestTokenResponseExpiresIn(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	for _, value := range []string{`3600`, `"3600"`, `"03600"`, `0`, `"0"`, `"000"`, `null`} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			var response tokenResponse
			require.NoError(t, json.Unmarshal([]byte(`{"access_token":"a","scope":"read","expires_in":`+value+`}`), &response))
			require.Equal(t, "a", response.AccessToken)
			require.True(t, response.ScopeReported())
			if value == `3600` || value == `"3600"` || value == `"03600"` {
				require.Equal(t, 3600, response.ExpiresIn)
				require.Equal(t, now.Add(time.Hour), *response.AccessExpiresAt(now))
			} else {
				require.Zero(t, response.ExpiresIn)
				require.Nil(t, response.AccessExpiresAt(now))
			}
		})
	}
	var omitted tokenResponse
	require.NoError(t, json.Unmarshal([]byte(`{"access_token":"a"}`), &omitted))
	require.Zero(t, omitted.ExpiresIn)
	for _, value := range []string{`-1`, `"-1"`, `9223372037`, `"9223372037"`, `""`, `"never"`, `1.5`, `"1.5"`, `true`, `{}`, `[]`, `"999999999999999999999999"`} {
		t.Run("invalid_"+value, func(t *testing.T) {
			t.Parallel()
			var response tokenResponse
			require.Error(t, json.Unmarshal([]byte(`{"expires_in":`+value+`}`), &response))
		})
	}
}

func TestTokenResponseWireExpiresIn(t *testing.T) {
	t.Parallel()
	maxSeconds := int(min(int64(^uint(0)>>1), int64((1<<63-1)/time.Second)))
	for _, want := range []int{0, 60, 7200, maxSeconds} {
		number := strconv.Itoa(want)
		for _, raw := range []string{number, strconv.Quote(number)} {
			t.Run(raw, func(t *testing.T) {
				t.Parallel()
				var wire tokenResponseWire
				require.NoError(t, json.Unmarshal([]byte(`{"access_token":"a","expires_in":`+raw+`}`), &wire))
				require.Equal(t, want, wire.ExpiresIn)
				require.Equal(t, "a", wire.AccessToken)
				encoded, err := json.Marshal(wire)
				require.NoError(t, err)
				var members map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(encoded, &members))
				require.JSONEq(t, number, string(members["expires_in"]))
			})
		}
	}
	for _, raw := range []string{`-1`, `"-1"`, `9223372037`, `"9223372037"`, `1e3`, `"1e3"`, `999999999999999999999999`, `"999999999999999999999999"`} {
		t.Run("invalid_"+raw, func(t *testing.T) {
			t.Parallel()
			wire := tokenResponseWire{ExpiresIn: 42}
			require.Error(t, json.Unmarshal([]byte(`{"expires_in":`+raw+`}`), &wire))
			require.Equal(t, 42, wire.ExpiresIn)
		})
	}
}

func TestTokenResponseRejectsMalformedScope(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`{"access_token":"a","scope":["read"]}`, `{"access_token":"a","scope":1}`, `{"access_token":"a","scope":{}}`} {
		var response tokenResponse
		require.Error(t, json.Unmarshal([]byte(raw), &response))
	}
}

func TestTokenResponseScopeReported(t *testing.T) {
	t.Parallel()

	for raw, reported := range map[string]bool{
		`{"access_token":"a"}`:                false,
		`{"access_token":"a","scope":null}`:   false,
		`{"access_token":"a","scope":""}`:     true,
		`{"access_token":"a","scope":"read"}`: true,
	} {
		var response tokenResponse
		require.NoError(t, json.Unmarshal([]byte(raw), &response), raw)
		require.Equal(t, reported, response.ScopeReported(), raw)
	}
}

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
