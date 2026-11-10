package gcpkms

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// localEncryptionRootKeyBytes is the size of the local stand-in's root secret,
// and of each per-key AES-256 key derived from it.
const localEncryptionRootKeyBytes = 32

// localEncryptionPEMType labels the persisted root secret.
const localEncryptionPEMType = "LOCAL KMS ENCRYPTION ROOT KEY"

var _ EncryptionProvisioningClient = (*LocalEncryptionClient)(nil)

// LocalEncryptionClient is an in-process EncryptionClient for CI and local
// development without KMS access.
//
// Every crypto key name maps to its own AES-256-GCM key, derived from one root
// secret with HKDF, so any well-formed key name behaves like an existing key
// and ciphertexts do not decrypt under a different key name. There is one
// version per key. Failures come back as the gRPC statuses real KMS returns, so
// callers exercise the same classification code.
type LocalEncryptionClient struct {
	root []byte

	// disabled records keys DisableEncryptionKey was called for; they refuse
	// to encrypt or decrypt, as a key with no enabled version does in KMS. It
	// is per process and not persisted, like LocalSigningClient's: a key
	// disabled by one local process stays usable in another and after a
	// restart. IAM is not modelled.
	mu       sync.Mutex
	disabled map[string]bool
}

func newLocalEncryptionClient(root []byte) *LocalEncryptionClient {
	return &LocalEncryptionClient{root: root, mu: sync.Mutex{}, disabled: map[string]bool{}}
}

// NewLocalEncryptionClient generates a fresh root secret, for tests.
func NewLocalEncryptionClient() (*LocalEncryptionClient, error) {
	root := make([]byte, localEncryptionRootKeyBytes)
	if _, err := rand.Read(root); err != nil {
		return nil, fmt.Errorf("generate local encryption root key: %w", err)
	}

	return newLocalEncryptionClient(root), nil
}

// NewPersistentLocalEncryptionClient loads the root secret from path, creating
// it with mode 0600 when absent, so data wrapped in one local run stays
// readable in the next. Concurrent startups converge on one secret.
func NewPersistentLocalEncryptionClient(path string) (*LocalEncryptionClient, error) {
	client, err := readLocalEncryptionClient(path)
	if err == nil {
		return client, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	client, err = NewLocalEncryptionClient()
	if err != nil {
		return nil, err
	}

	doc := pem.EncodeToMemory(&pem.Block{Type: localEncryptionPEMType, Headers: nil, Bytes: client.root})
	if err := publishLocalKeyFile(path, doc); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return readLocalEncryptionClient(path)
		}
		return nil, err
	}

	return client, nil
}

func readLocalEncryptionClient(path string) (*LocalEncryptionClient, error) {
	doc, err := readLocalKeyFile(path)
	if err != nil {
		return nil, fmt.Errorf("persistent local encryption key: %w", err)
	}

	block, rest := pem.Decode(doc)
	if block == nil || block.Type != localEncryptionPEMType || len(rest) != 0 || len(block.Bytes) != localEncryptionRootKeyBytes {
		return nil, fmt.Errorf("decode persistent local encryption key: expected one %d-byte %s PEM block", localEncryptionRootKeyBytes, localEncryptionPEMType)
	}

	return newLocalEncryptionClient(block.Bytes), nil
}

// Encrypt seals plaintext with the key derived for keyName, binding aad.
func (c *LocalEncryptionClient) Encrypt(ctx context.Context, keyName string, plaintext, aad []byte) (*EncryptResult, error) {
	aead, err := c.aeadFor(ctx, keyName)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate local encryption nonce: %w", err)
	}

	return &EncryptResult{
		Ciphertext:     aead.Seal(nonce, nonce, plaintext, aad),
		KeyVersionName: firstKeyVersionName(keyName),
	}, nil
}

// Decrypt opens a ciphertext Encrypt produced under keyName with the same aad.
// A wrong key, wrong aad or tampered ciphertext fails with InvalidArgument, as
// it does against real KMS.
func (c *LocalEncryptionClient) Decrypt(ctx context.Context, keyName string, ciphertext, aad []byte) ([]byte, error) {
	aead, err := c.aeadFor(ctx, keyName)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < aead.NonceSize() {
		return nil, status.Error(codes.InvalidArgument, "local kms decrypt: ciphertext too short") //nolint:wrapcheck // the stand-in speaks gRPC status codes, as GCP does
	}
	nonce, sealed := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, sealed, aad)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "local kms decrypt: ciphertext or associated data does not match") //nolint:wrapcheck // the stand-in speaks gRPC status codes, as GCP does
	}

	return plaintext, nil
}

// aeadFor validates the call and returns the AES-GCM instance for keyName.
func (c *LocalEncryptionClient) aeadFor(ctx context.Context, keyName string) (cipher.AEAD, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("local kms encryption: %w", err)
	}
	if err := ValidateKeyName(keyName); err != nil {
		return nil, err
	}

	c.mu.Lock()
	disabled := c.disabled[keyName]
	c.mu.Unlock()
	if disabled {
		return nil, status.Errorf(codes.FailedPrecondition, "local kms: %s has no enabled version", keyName) //nolint:wrapcheck // the stand-in speaks gRPC status codes, as GCP does
	}

	key, err := hkdf.Key(sha256.New, c.root, nil, keyName, localEncryptionRootKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("derive local encryption key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("build local encryption cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build local encryption aead: %w", err)
	}

	return aead, nil
}

// CreateEncryptionKey names a key; its material is derived on first use.
func (c *LocalEncryptionClient) CreateEncryptionKey(ctx context.Context, params CreateEncryptionKeyParams) (*CreatedEncryptionKey, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("create local encryption key: %w", err)
	}
	if err := validateCreateEncryptionKeyParams(params); err != nil {
		return nil, err
	}

	return &CreatedEncryptionKey{KeyName: params.KeyRing + "/cryptoKeys/" + params.KeyID}, nil
}

// GrantEncrypterDecrypter validates its inputs; there is no IAM in-process.
func (c *LocalEncryptionClient) GrantEncrypterDecrypter(ctx context.Context, keyName, serviceAccountEmail string) error {
	return validateLocalGrant(ctx, keyName, serviceAccountEmail)
}

// RevokeEncrypterDecrypter validates its inputs; there is no IAM in-process.
func (c *LocalEncryptionClient) RevokeEncrypterDecrypter(ctx context.Context, keyName, serviceAccountEmail string) error {
	return validateLocalGrant(ctx, keyName, serviceAccountEmail)
}

func validateLocalGrant(ctx context.Context, keyName, serviceAccountEmail string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("local kms grant: %w", err)
	}
	if err := ValidateKeyName(keyName); err != nil {
		return err
	}
	if serviceAccountEmail == "" {
		return errors.New("local kms grant: service account email is required")
	}

	return nil
}

// DisableEncryptionKey marks the key disabled: later encrypt and decrypt calls
// fail with FailedPrecondition, as they would against real KMS.
func (c *LocalEncryptionClient) DisableEncryptionKey(ctx context.Context, keyName string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("disable local encryption key: %w", err)
	}
	if err := ValidateKeyName(keyName); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.disabled[keyName] = true

	return nil
}

// Close is a no-op: there is no connection to release.
func (c *LocalEncryptionClient) Close() error {
	return nil
}
