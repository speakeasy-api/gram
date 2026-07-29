package gcpkms

import (
	"crypto/sha256"
	"testing"

	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
)

func TestJoseAlgorithm_SupportedRSA(t *testing.T) {
	t.Parallel()

	for _, kmsAlg := range []kmspb.CryptoKeyVersion_CryptoKeyVersionAlgorithm{
		kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_2048_SHA256,
		kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_3072_SHA256,
		kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_4096_SHA256,
	} {
		alg, err := joseAlgorithm(kmsAlg)
		require.NoError(t, err, "%s should map to RS256", kmsAlg)
		require.Equal(t, jose.RS256, alg)
	}
}

func TestJoseAlgorithm_SupportedECDSA(t *testing.T) {
	t.Parallel()

	alg, err := joseAlgorithm(kmspb.CryptoKeyVersion_EC_SIGN_P256_SHA256)
	require.NoError(t, err)
	require.Equal(t, jose.ES256, alg)
}

// RSA-PSS is the dangerous near-miss: same RSA key, same SHA-256, but PS256 not
// RS256. Accepting it would silently mint tokens no verifier accepts, so the
// rejection must name both the KMS algorithm and its real JOSE identity.
func TestJoseAlgorithm_RejectsRSAPSSByName(t *testing.T) {
	t.Parallel()

	_, err := joseAlgorithm(kmspb.CryptoKeyVersion_RSA_SIGN_PSS_2048_SHA256)
	require.ErrorIs(t, err, ErrUnsupportedAlgorithm)
	require.ErrorContains(t, err, "RSA_SIGN_PSS_2048_SHA256")
	require.ErrorContains(t, err, "PS256")
}

func TestJoseAlgorithm_RejectsKnownButUnsupported(t *testing.T) {
	t.Parallel()

	for _, kmsAlg := range []kmspb.CryptoKeyVersion_CryptoKeyVersionAlgorithm{
		kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_4096_SHA512,
		kmspb.CryptoKeyVersion_RSA_SIGN_PSS_4096_SHA512,
		kmspb.CryptoKeyVersion_EC_SIGN_P384_SHA384,
	} {
		_, err := joseAlgorithm(kmsAlg)
		require.ErrorIs(t, err, ErrUnsupportedAlgorithm, "%s must not be accepted", kmsAlg)
	}
}

func TestJoseAlgorithm_RejectsNonSigningAlgorithm(t *testing.T) {
	t.Parallel()

	_, err := joseAlgorithm(kmspb.CryptoKeyVersion_GOOGLE_SYMMETRIC_ENCRYPTION)
	require.ErrorIs(t, err, ErrUnsupportedAlgorithm)
	require.ErrorContains(t, err, "no JOSE equivalent")
}

// Ed25519 signing keys DO have a JOSE equivalent (EdDSA), so they must be
// rejected for being outside the supported set rather than for lacking a
// mapping, which would send an operator looking for something that exists.
func TestJoseAlgorithm_RejectsEd25519WithAccurateReason(t *testing.T) {
	t.Parallel()

	_, err := joseAlgorithm(kmspb.CryptoKeyVersion_EC_SIGN_ED25519)
	require.ErrorIs(t, err, ErrUnsupportedAlgorithm)
	require.ErrorContains(t, err, "EdDSA")
	require.NotContains(t, err.Error(), "no JOSE equivalent")
}

func TestDigestPayload(t *testing.T) {
	t.Parallel()

	want := sha256.Sum256([]byte(ProbePayload))

	for _, alg := range []jose.SignatureAlgorithm{jose.RS256, jose.ES256} {
		digest, err := digestPayload(alg, []byte(ProbePayload))
		require.NoError(t, err)
		require.Equal(t, want[:], digest)
	}
}

func TestDigestPayload_RejectsUnsupportedAlgorithm(t *testing.T) {
	t.Parallel()

	_, err := digestPayload(jose.PS256, []byte(ProbePayload))
	require.ErrorIs(t, err, ErrUnsupportedAlgorithm)
}
