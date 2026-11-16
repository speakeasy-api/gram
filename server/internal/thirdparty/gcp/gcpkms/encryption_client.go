package gcpkms

import (
	"context"
	"errors"
	"io"
	"strings"

	"golang.org/x/oauth2"
)

// ErrEncryptionResponseInvalid is returned when a KMS encrypt or decrypt
// response fails an integrity check: a checksum mismatch, an unverified request
// checksum, or a response naming a different key. Retrying may succeed.
var ErrEncryptionResponseInvalid = errors.New("gcp kms encryption response failed integrity check")

// SymmetricAlgorithm is the algorithm recorded for every symmetric encryption
// key: GCP's GOOGLE_SYMMETRIC_ENCRYPTION, which is AES-256-GCM.
const SymmetricAlgorithm = "GOOGLE_SYMMETRIC_ENCRYPTION"

// EncryptResult is a KMS ciphertext and the key version that produced it.
type EncryptResult struct {
	// Ciphertext is the provider's opaque ciphertext. It encodes the key
	// version, so decrypting it never needs KeyVersionName.
	Ciphertext []byte

	// KeyVersionName is the crypto key version KMS encrypted with, recorded so
	// a later primary-version rotation can be detected and re-wrapped.
	KeyVersionName string
}

// EncryptionClient encrypts and decrypts with symmetric KMS keys. Keys are
// addressed at the crypto key level: KMS encrypts with the primary version and
// the ciphertext records which version to decrypt with.
//
// It is the request-path surface and sees only cryptographic quota. Callers
// should still keep it off per-row paths and use it to wrap and unwrap data
// encryption keys.
type EncryptionClient interface {
	// Encrypt encrypts plaintext under the key's primary version, binding aad
	// as additional authenticated data that Decrypt must be given again.
	Encrypt(ctx context.Context, keyName string, plaintext, aad []byte) (*EncryptResult, error)

	// Decrypt decrypts a ciphertext Encrypt produced under keyName with the
	// same aad.
	Decrypt(ctx context.Context, keyName string, ciphertext, aad []byte) ([]byte, error)

	io.Closer
}

// CreateEncryptionKeyParams describes the symmetric key to create.
type CreateEncryptionKeyParams struct {
	// KeyRing is the fully-qualified ring the key is created in.
	KeyRing string

	// KeyID is the crypto key id within the ring, from NewKeyID.
	KeyID string
}

// CreatedEncryptionKey names what CreateEncryptionKey produced.
type CreatedEncryptionKey struct {
	// KeyName is the crypto key resource name: what Speakeasy records, encrypts
	// with, and attaches IAM bindings to.
	KeyName string
}

// EncryptionKeyProvisioner is the management-tier surface for symmetric keys:
// creation, IAM grants, and disabling. Low quota; never call it on a request
// path.
type EncryptionKeyProvisioner interface {
	// CreateEncryptionKey creates a symmetric ENCRYPT_DECRYPT key whose first
	// version is enabled and primary.
	CreateEncryptionKey(ctx context.Context, params CreateEncryptionKeyParams) (*CreatedEncryptionKey, error)

	// GrantEncrypterDecrypter binds EncrypterDecrypterRole for the service
	// account on one crypto key. Re-granting is a no-op.
	GrantEncrypterDecrypter(ctx context.Context, keyName, serviceAccountEmail string) error

	// RevokeEncrypterDecrypter removes the EncrypterDecrypterRole binding for
	// the service account on one crypto key. A missing binding is a no-op.
	RevokeEncrypterDecrypter(ctx context.Context, keyName, serviceAccountEmail string) error

	// DisableEncryptionKey disables every enabled version of the key, so
	// nothing it wrapped can be decrypted. Crypto keys cannot be deleted, so
	// this is the end of a managed key's life.
	DisableEncryptionKey(ctx context.Context, keyName string) error
}

// EncryptionProvisioningClient is an EncryptionClient that can also provision keys.
type EncryptionProvisioningClient interface {
	EncryptionClient
	EncryptionKeyProvisioner
}

// EncryptionClientFactory builds an EncryptionClient authenticated as some
// identity. Services hold one of these rather than a client, because each
// client owns a connection that must be closed after use.
type EncryptionClientFactory func(ctx context.Context, tokenSource oauth2.TokenSource) (EncryptionClient, error)

var _ EncryptionClientFactory = NewEncryptionClient

// EncryptionProvisioningClientFactory builds an EncryptionProvisioningClient
// authenticated as some identity.
type EncryptionProvisioningClientFactory func(ctx context.Context, tokenSource oauth2.TokenSource) (EncryptionProvisioningClient, error)

var _ EncryptionProvisioningClientFactory = NewEncryptionProvisioningClient

// keyVersionOf reports whether versionName is a version of keyName.
func keyVersionOf(keyName, versionName string) bool {
	return strings.HasPrefix(versionName, keyName+"/cryptoKeyVersions/") && ValidateKeyVersionName(versionName) == nil
}
