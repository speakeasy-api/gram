package jwks

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
)

// rsaTestKey is an RSA public JWK declaring no alg.
func rsaTestKey(t *testing.T, kid string) jose.JSONWebKey {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return jose.JSONWebKey{
		Key:       key.Public(),
		KeyID:     kid,
		Algorithm: "",
		Use:       "sig",
	}
}

func TestSelectKeyForAlgorithm_SharedKidPicksByType(t *testing.T) {
	t.Parallel()

	ec := testKey(t, "shared")
	ec.Algorithm = ""
	set, err := parseKeySet(keySetJSON(t, rsaTestKey(t, "shared"), ec))
	require.NoError(t, err)

	key, err := selectKeyForAlgorithm(set, "shared", jose.ES256)
	require.NoError(t, err)
	require.IsType(t, &ecdsa.PublicKey{}, key.Key)

	key, err = selectKeyForAlgorithm(set, "shared", jose.PS256)
	require.NoError(t, err)
	require.IsType(t, &rsa.PublicKey{}, key.Key)

	_, err = selectKeyForAlgorithm(set, "shared", jose.ES384)
	require.ErrorIs(t, err, ErrKeyAlgorithmMismatch, "a P-256 key cannot carry ES384")

	_, err = selectKeyForAlgorithm(set, "shared", jose.EdDSA)
	require.ErrorIs(t, err, ErrKeyAlgorithmMismatch)
}

func TestSelectKeyForAlgorithm_DeclaredAlgWins(t *testing.T) {
	t.Parallel()

	undeclared := testKey(t, "shared")
	undeclared.Algorithm = ""
	set, err := parseKeySet(keySetJSON(t, undeclared, testKey(t, "shared")))
	require.NoError(t, err)

	key, err := selectKeyForAlgorithm(set, "shared", jose.ES256)
	require.NoError(t, err)
	require.Equal(t, "ES256", key.Algorithm, "the key declaring the algorithm is preferred to one merely fitting it")
}

func TestSelectKeyForAlgorithm_DeclaredMismatchRejected(t *testing.T) {
	t.Parallel()

	set, err := parseKeySet(keySetJSON(t, testKey(t, "a")))
	require.NoError(t, err)

	_, err = selectKeyForAlgorithm(set, "a", jose.RS256)
	require.ErrorIs(t, err, ErrKeyAlgorithmMismatch)
	require.NotErrorIs(t, err, ErrKeyNotFound, "a known kid owes no refresh")

	_, err = selectKeyForAlgorithm(set, "missing", jose.ES256)
	require.ErrorIs(t, err, ErrKeyNotFound)
}

func TestSelectKeyForAlgorithm_NoKid(t *testing.T) {
	t.Parallel()

	set, err := parseKeySet(keySetJSON(t, rsaTestKey(t, "r"), testKey(t, "e")))
	require.NoError(t, err)

	key, err := selectKeyForAlgorithm(set, "", jose.RS256)
	require.NoError(t, err)
	require.Equal(t, "r", key.KeyID, "without a kid the one key able to carry the algorithm is unambiguous")

	ambiguous, err := parseKeySet(keySetJSON(t, rsaTestKey(t, "r1"), rsaTestKey(t, "r2")))
	require.NoError(t, err)
	_, err = selectKeyForAlgorithm(ambiguous, "", jose.RS256)
	require.ErrorIs(t, err, ErrKeyNotFound)
}

func TestVerificationKeyForAlgorithm_SharedKid(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, rsaTestKey(t, "shared"), testKey(t, "shared")))
	resolver, _ := newTestKeyResolver(t, server, generousRate())
	source := remoteSourceFor(t, server)

	key, err := resolver.VerificationKeyForAlgorithm(t.Context(), source, "shared", jose.ES256)
	require.NoError(t, err)
	require.IsType(t, &ecdsa.PublicKey{}, key.Key)

	key, err = resolver.VerificationKeyForAlgorithm(t.Context(), source, "shared", jose.RS256)
	require.NoError(t, err)
	require.IsType(t, &rsa.PublicKey{}, key.Key)

	_, err = resolver.VerificationKeyForAlgorithm(t.Context(), source, "shared", jose.EdDSA)
	require.ErrorIs(t, err, ErrKeyAlgorithmMismatch)
	require.Equal(t, 1, server.Fetches(), "a known kid that cannot carry the algorithm triggers no refresh")

	plain, err := resolver.VerificationKey(t.Context(), source, "shared")
	require.NoError(t, err)
	require.IsType(t, &rsa.PublicKey{}, plain.Key, "the kid-only lookup keeps first-match selection")
}
