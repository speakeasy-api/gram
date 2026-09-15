package usersessions_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

// neverRevoked is a RevocationChecker that admits every token, so signer tests
// exercise claim handling without a Redis round trip.
type neverRevoked struct{}

func (neverRevoked) IsTokenRevoked(context.Context, string) (bool, error) { return false, nil }

// decodeJWTPayload returns the raw claim set of a signed token, so tests can
// assert on the wire representation rather than the parsed struct.
func decodeJWTPayload(t *testing.T, token string) map[string]any {
	t.Helper()

	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)

	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)

	var claims map[string]any
	require.NoError(t, json.Unmarshal(raw, &claims))

	return claims
}

func TestSigner_MintCarriesOAuthClientID(t *testing.T) {
	t.Parallel()

	signer := usersessions.NewSigner("test-jwt-secret")
	subject := urn.NewUserSubject("user-1")

	token, jti, err := signer.Mint(usersessions.MintParams{
		Subject:  subject,
		Audience: "aud",
		Issuer:   "https://example.test/mcp/slug",
		Lifetime: time.Hour,
		ClientID: "client-abc",
	})
	require.NoError(t, err)

	session, err := signer.ValidateBearer(t.Context(), token, "aud", neverRevoked{})
	require.NoError(t, err)
	require.Equal(t, subject, session.Subject())
	require.Equal(t, jti, session.JTI())
	require.Equal(t, "client-abc", session.ClientID(),
		"the OAuth client must survive the round trip; it is the only verified caller identity a tool call gets")

	require.Equal(t, "client-abc", decodeJWTPayload(t, token)["client_id"],
		"the claim name is client_id per RFC 9068 §2.2")
}

// TestSigner_MintWithoutClientIDOmitsClaim covers the mint paths that have no
// OAuth client (API-key exchange) and, equally, tokens minted before the claim
// existed: readers must see an absent claim, not an empty string on the wire.
func TestSigner_MintWithoutClientIDOmitsClaim(t *testing.T) {
	t.Parallel()

	signer := usersessions.NewSigner("test-jwt-secret")

	token, _, err := signer.Mint(usersessions.MintParams{
		Subject:  urn.NewUserSubject("user-1"),
		Audience: "aud",
		Issuer:   "https://example.test/mcp/slug",
		Lifetime: time.Hour,
		ClientID: "",
	})
	require.NoError(t, err)

	require.NotContains(t, decodeJWTPayload(t, token), "client_id")

	session, err := signer.ValidateBearer(t.Context(), token, "aud", neverRevoked{})
	require.NoError(t, err)
	require.Empty(t, session.ClientID())
}

// forgeHSToken hand-mints a token with the given signing method, key, and jti,
// bypassing Signer.Mint so tests can produce tokens an attacker would try
// (wrong key, alg confusion, alg=none).
func forgeHSToken(t *testing.T, method jwt.SigningMethod, key []byte, jti string) string {
	t.Helper()
	tok := jwt.NewWithClaims(method, jwt.RegisteredClaims{ID: jti})
	signed, err := tok.SignedString(key)
	require.NoError(t, err)
	return signed
}

// TestSigner_VerifiedJTI is the security contract behind token revocation:
// only a token bearing our valid HS256 signature yields its jti. A forged or
// wrong-key token must be rejected, so a public client (which presents no
// client_secret) cannot revoke another session using a guessed/leaked jti.
func TestSigner_VerifiedJTI(t *testing.T) {
	t.Parallel()

	const secret = "test-jwt-secret"
	signer := usersessions.NewSigner(secret)

	mint := func(lifetime time.Duration) (token, jti string) {
		token, jti, err := signer.Mint(usersessions.MintParams{
			Subject:  urn.NewUserSubject("user-1"),
			Audience: "aud",
			Issuer:   "https://example.test/mcp/slug",
			Lifetime: lifetime,
			ClientID: "client-abc",
		})
		require.NoError(t, err)
		return token, jti
	}

	t.Run("valid token yields jti", func(t *testing.T) {
		t.Parallel()
		token, jti := mint(time.Hour)
		got, err := signer.VerifiedJTI(token)
		require.NoError(t, err)
		require.Equal(t, jti, got)
	})

	t.Run("expired token still yields jti", func(t *testing.T) {
		t.Parallel()
		// Revocation must work on stale tokens — that is the whole point of
		// skipping claims validation while keeping signature verification.
		token, jti := mint(-time.Hour)
		got, err := signer.VerifiedJTI(token)
		require.NoError(t, err)
		require.Equal(t, jti, got)
	})

	t.Run("wrong signing key is rejected", func(t *testing.T) {
		t.Parallel()
		forged := forgeHSToken(t, jwt.SigningMethodHS256, []byte("attacker-secret"), "victim-jti")
		_, err := signer.VerifiedJTI(forged)
		require.Error(t, err)
	})

	t.Run("alg=none is rejected", func(t *testing.T) {
		t.Parallel()
		tok := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{ID: "victim-jti"})
		unsigned, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
		require.NoError(t, err)
		_, err = signer.VerifiedJTI(unsigned)
		require.Error(t, err)
	})

	t.Run("HS384 with the signing key is rejected (exact alg pinning)", func(t *testing.T) {
		t.Parallel()
		// Even signed with our real secret, a non-HS256 algorithm must not pass:
		// method pinning is what stops an attacker steering the verifier.
		forged := forgeHSToken(t, jwt.SigningMethodHS384, []byte(secret), "victim-jti")
		_, err := signer.VerifiedJTI(forged)
		require.Error(t, err)
	})

	t.Run("token missing jti is rejected", func(t *testing.T) {
		t.Parallel()
		forged := forgeHSToken(t, jwt.SigningMethodHS256, []byte(secret), "")
		_, err := signer.VerifiedJTI(forged)
		require.Error(t, err)
	})

	t.Run("garbage is rejected", func(t *testing.T) {
		t.Parallel()
		_, err := signer.VerifiedJTI("not-a-jwt")
		require.Error(t, err)
	})
}
