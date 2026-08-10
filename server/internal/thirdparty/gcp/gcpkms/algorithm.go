package gcpkms

import (
	"crypto"
	_ "crypto/sha256" // registers SHA-256 so crypto.SHA256.New() is available
	"errors"
	"fmt"
	"slices"

	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
	jose "github.com/go-jose/go-jose/v4"
)

// ErrUnsupportedAlgorithm is returned when a key signs with an algorithm Gram
// does not publish. It is a misconfiguration rather than a fault, so callers
// should surface it as a reportable outcome.
var ErrUnsupportedAlgorithm = errors.New("kms key algorithm not supported")

// supportedAlgorithms are the JOSE signing algorithms Gram publishes and signs
// with. Widening this set is an interoperability decision, not a mechanical one:
// PS256 is optional in RFC 7518 and plenty of verifiers do not implement it.
var supportedAlgorithms = []jose.SignatureAlgorithm{jose.RS256, jose.ES256}

// joseAlgorithmByKMS maps GCP KMS asymmetric-sign algorithms onto their JOSE
// equivalents. Algorithms Gram does not support are listed deliberately: naming
// what a key actually is makes a misconfiguration self-explanatory. RSA-PSS
// matters most here — PS256 and RS256 are both RSA over SHA-256, but PSS and
// PKCS#1 v1.5 produce signatures no shared verifier accepts, so a PSS key
// silently mounted as RS256 would mint unverifiable tokens.
//
// Only signing algorithms appear. Encryption, HMAC, KEM and post-quantum
// algorithms have no JOSE signature equivalent, and joseAlgorithm rejects any
// key whose algorithm is absent from this map.
//
//nolint:exhaustive // signing algorithms only, per the note above
var joseAlgorithmByKMS = map[kmspb.CryptoKeyVersion_CryptoKeyVersionAlgorithm]jose.SignatureAlgorithm{
	kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_2048_SHA256: jose.RS256,
	kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_3072_SHA256: jose.RS256,
	kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_4096_SHA256: jose.RS256,
	kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_4096_SHA512: jose.RS512,
	kmspb.CryptoKeyVersion_RSA_SIGN_PSS_2048_SHA256:   jose.PS256,
	kmspb.CryptoKeyVersion_RSA_SIGN_PSS_3072_SHA256:   jose.PS256,
	kmspb.CryptoKeyVersion_RSA_SIGN_PSS_4096_SHA256:   jose.PS256,
	kmspb.CryptoKeyVersion_RSA_SIGN_PSS_4096_SHA512:   jose.PS512,
	kmspb.CryptoKeyVersion_EC_SIGN_P256_SHA256:        jose.ES256,
	kmspb.CryptoKeyVersion_EC_SIGN_P384_SHA384:        jose.ES384,
	kmspb.CryptoKeyVersion_EC_SIGN_ED25519:            jose.EdDSA,
}

// joseAlgorithm maps a KMS key-version algorithm onto the JOSE algorithm Gram
// records, rejecting anything outside the supported set.
func joseAlgorithm(kmsAlg kmspb.CryptoKeyVersion_CryptoKeyVersionAlgorithm) (jose.SignatureAlgorithm, error) {
	alg, known := joseAlgorithmByKMS[kmsAlg]
	if !known {
		return "", fmt.Errorf("%w: key version signs with %s, which has no JOSE equivalent", ErrUnsupportedAlgorithm, kmsAlg)
	}

	if !slices.Contains(supportedAlgorithms, alg) {
		return "", fmt.Errorf("%w: key version signs with %s (JOSE %s); Gram supports %v", ErrUnsupportedAlgorithm, kmsAlg, alg, supportedAlgorithms)
	}

	return alg, nil
}

// digestHash is the hash a JOSE algorithm digests its payload with before the
// signature is computed.
func digestHash(alg jose.SignatureAlgorithm) (crypto.Hash, error) {
	switch alg {
	case jose.RS256, jose.ES256:
		return crypto.SHA256, nil
	default:
		return 0, fmt.Errorf("%w: no digest defined for %s", ErrUnsupportedAlgorithm, alg)
	}
}

// digestPayload hashes a payload with the algorithm's digest. GCP KMS signs a
// digest rather than the payload itself, so this runs on the caller's side.
func digestPayload(alg jose.SignatureAlgorithm, payload []byte) ([]byte, error) {
	hash, err := digestHash(alg)
	if err != nil {
		return nil, err
	}

	if !hash.Available() {
		return nil, fmt.Errorf("%w: hash %s is not linked into the binary", ErrUnsupportedAlgorithm, hash)
	}

	h := hash.New()
	if _, err := h.Write(payload); err != nil {
		return nil, fmt.Errorf("hash payload with %s: %w", hash, err)
	}

	return h.Sum(nil), nil
}
