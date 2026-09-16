package gcpkms

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"cloud.google.com/go/iam"
	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
	jose "github.com/go-jose/go-jose/v4"
)

// ErrInvalidKeyRingName is returned when a configured key ring is not a
// fully-qualified GCP KMS key ring resource name.
var ErrInvalidKeyRingName = errors.New("invalid gcp kms key ring name")

// ErrInvalidKeyID is returned when a crypto key id does not match the grammar
// GCP KMS accepts.
var ErrInvalidKeyID = errors.New("invalid gcp kms crypto key id")

// ErrSigningKeyExists is returned when a key with the requested id already
// exists in the ring. Ids are random, so this indicates a collision or a retry
// against a ring the caller does not own.
var ErrSigningKeyExists = errors.New("gcp kms crypto key already exists")

// SignerVerifierRole is the IAM role that permits signing with a key and
// reading its public half, and nothing else on the ring.
const SignerVerifierRole iam.RoleName = "roles/cloudkms.signerVerifier"

// keyRingNamePattern matches a key ring resource name, the parent every
// crypto key is created under.
var keyRingNamePattern = regexp.MustCompile(
	`^projects/[^/\s]+/locations/[^/\s]+/keyRings/[^/\s]+$`,
)

// keyNamePattern matches a crypto key resource name (no version suffix), the
// resource IAM bindings are attached to.
var keyNamePattern = regexp.MustCompile(
	`^projects/[^/\s]+/locations/[^/\s]+/keyRings/[^/\s]+/cryptoKeys/[^/\s]+$`,
)

// keyIDPattern is the grammar GCP documents for crypto key ids.
var keyIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,63}$`)

// ValidateKeyRingName reports whether a resource name identifies a key ring.
func ValidateKeyRingName(name string) error {
	if !keyRingNamePattern.MatchString(name) {
		return fmt.Errorf("%w: %q is not a projects/<p>/locations/<l>/keyRings/<r> path", ErrInvalidKeyRingName, name)
	}

	return nil
}

// ValidateKeyName reports whether a resource name identifies a crypto key
// rather than a ring or a version.
func ValidateKeyName(name string) error {
	if !keyNamePattern.MatchString(name) {
		return fmt.Errorf("%w: %q is not a projects/<p>/locations/<l>/keyRings/<r>/cryptoKeys/<k> path", ErrInvalidResourceName, name)
	}

	return nil
}

// signingKeyIDRandomBytes is the entropy behind a generated key id: 128 bits,
// so two provisioning runs can never collide within a ring.
const signingKeyIDRandomBytes = 16

// NewSigningKeyID returns a random crypto key id under prefix. The id is what
// names the key in GCP, so it must never be derived from a row id that appears
// in a public URL: a reader of the JWKS URL must not be able to name the key.
func NewSigningKeyID(prefix string) (string, error) {
	raw := make([]byte, signingKeyIDRandomBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate signing key id: %w", err)
	}

	id := strings.TrimSpace(prefix) + "-" + hex.EncodeToString(raw)
	if !keyIDPattern.MatchString(id) {
		return "", fmt.Errorf("%w: %q", ErrInvalidKeyID, id)
	}

	return id, nil
}

// CreateSigningKeyParams describes the key to create.
type CreateSigningKeyParams struct {
	// KeyRing is the fully-qualified ring the key is created in.
	KeyRing string

	// KeyID is the crypto key id within the ring, from NewSigningKeyID.
	KeyID string

	// Algorithm is the JOSE algorithm the key signs with. Only RS256 and ES256
	// are supported.
	Algorithm jose.SignatureAlgorithm
}

// CreatedSigningKey names what CreateSigningKey produced.
type CreatedSigningKey struct {
	// KeyName is the crypto key resource name, the target for IAM bindings.
	KeyName string

	// KeyVersionName is the first version's resource name, the value Gram
	// records and signs with.
	KeyVersionName string
}

// kmsAlgorithmForJOSE picks the KMS algorithm a new key is created with. RSA
// keys use 2048 bits, the size every verifier accepts and the smallest GCP
// offers, which also keeps generation time short.
func kmsAlgorithmForJOSE(alg jose.SignatureAlgorithm) (kmspb.CryptoKeyVersion_CryptoKeyVersionAlgorithm, error) {
	switch alg {
	case jose.RS256:
		return kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_2048_SHA256, nil
	case jose.ES256:
		return kmspb.CryptoKeyVersion_EC_SIGN_P256_SHA256, nil
	default:
		return kmspb.CryptoKeyVersion_CRYPTO_KEY_VERSION_ALGORITHM_UNSPECIFIED, fmt.Errorf("%w: %s", ErrUnsupportedAlgorithm, alg)
	}
}

// validateCreateSigningKeyParams checks everything that can be checked before
// a request leaves the process.
func validateCreateSigningKeyParams(params CreateSigningKeyParams) error {
	if err := ValidateKeyRingName(params.KeyRing); err != nil {
		return err
	}
	if !keyIDPattern.MatchString(params.KeyID) {
		return fmt.Errorf("%w: %q", ErrInvalidKeyID, params.KeyID)
	}
	if _, err := kmsAlgorithmForJOSE(params.Algorithm); err != nil {
		return err
	}

	return nil
}

// signingKeyVersionName is the resource name of a key's first version.
func signingKeyVersionName(keyName string) string {
	return keyName + "/cryptoKeyVersions/1"
}
