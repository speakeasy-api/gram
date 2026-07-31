package usersessions_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

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
	require.Equal(t, subject, session.Subject)
	require.Equal(t, jti, session.JTI)
	require.Equal(t, "client-abc", session.ClientID,
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
	require.Empty(t, session.ClientID)
}
