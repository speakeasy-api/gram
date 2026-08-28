package clientauth_test

import (
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions/clientauth"
)

func TestUnverifiedClientID(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)

	got, err := clientauth.UnverifiedClientID(s.sign(t, validClaims()))
	require.NoError(t, err)
	require.Equal(t, testClientID, got)
}

// The unverified read applies the same algorithm allowlist, so it cannot hand
// back an identifier from an assertion that could never have verified.
func TestUnverifiedClientID_RejectsDisallowedAlgorithm(t *testing.T) {
	t.Parallel()

	hmacSigner, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.HS256, Key: []byte("shared-secret-shared-secret-1234")},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	require.NoError(t, err)
	forged, err := jwt.Signed(hmacSigner).Claims(validClaims()).Serialize()
	require.NoError(t, err)

	_, err = clientauth.UnverifiedClientID(forged)
	requireRejected(t, err, clientauth.ReasonMalformed)
}

func TestUnverifiedClientID_RequiresMatchingIssuerAndSubject(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	claims := validClaims()
	claims.Subject = "https://client.example.com/oauth/other.json"

	_, err := clientauth.UnverifiedClientID(s.sign(t, claims))
	requireRejected(t, err, clientauth.ReasonSubjectMismatch)
}
