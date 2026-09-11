package gcpkms

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	jose "github.com/go-jose/go-jose/v4"
)

// rsaLocalKeyBits matches the smallest RSA size GCP KMS offers for signing, so
// the stand-in produces signatures the same width as production.
const rsaLocalKeyBits = 2048

var _ SigningClient = (*LocalSigningClient)(nil)

// LocalSigningClient is an in-process SigningClient backed by a private key, for
// use where no GCP network path exists: CI, and local development without KMS
// access.
//
// It is a faithful stand-in for the transport rather than a shortcut around it.
// Signatures come back in the provider's own encodings — PKCS#1 v1.5 for RSA,
// ASN.1 DER for ECDSA — so callers exercise the same parsing, conversion and
// verification code they would against real KMS.
type LocalSigningClient struct {
	alg jose.SignatureAlgorithm
	key crypto.Signer
}

// NewLocalSigningClient generates a key pair for the given algorithm.
func NewLocalSigningClient(alg jose.SignatureAlgorithm) (*LocalSigningClient, error) {
	switch alg {
	case jose.RS256:
		key, err := rsa.GenerateKey(rand.Reader, rsaLocalKeyBits)
		if err != nil {
			return nil, fmt.Errorf("generate local rsa key: %w", err)
		}
		return &LocalSigningClient{alg: alg, key: key}, nil

	case jose.ES256:
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generate local ecdsa key: %w", err)
		}
		return &LocalSigningClient{alg: alg, key: key}, nil

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedAlgorithm, alg)
	}
}

// NewPersistentLocalSigningClient loads a local signing key from path, creating
// it with mode 0600 when absent. The atomic link makes concurrent server and
// worker startups converge on one key without replacing a key another process
// has already loaded.
func NewPersistentLocalSigningClient(alg jose.SignatureAlgorithm, path string) (*LocalSigningClient, error) {
	client, err := readLocalSigningClient(alg, path)
	if err == nil {
		return client, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	client, err = NewLocalSigningClient(alg)
	if err != nil {
		return nil, err
	}

	der, err := x509.MarshalPKCS8PrivateKey(client.key)
	if err != nil {
		return nil, fmt.Errorf("marshal persistent local %s key: %w", alg, err)
	}
	doc := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Headers: nil, Bytes: der})

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create persistent local key directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".local-kms-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create persistent local key temporary file: %w", err)
	}
	tmpName := tmp.Name()
	removeTemp := func() error {
		return errors.Join(tmp.Close(), os.Remove(tmpName))
	}
	if err := tmp.Chmod(0o600); err != nil {
		return nil, errors.Join(fmt.Errorf("secure persistent local key temporary file: %w", err), removeTemp())
	}
	if _, err := tmp.Write(doc); err != nil {
		return nil, errors.Join(fmt.Errorf("write persistent local key temporary file: %w", err), removeTemp())
	}
	if err := tmp.Sync(); err != nil {
		return nil, errors.Join(fmt.Errorf("sync persistent local key temporary file: %w", err), removeTemp())
	}
	if err := tmp.Close(); err != nil {
		return nil, errors.Join(fmt.Errorf("close persistent local key temporary file: %w", err), os.Remove(tmpName))
	}

	if err := os.Link(tmpName, path); err != nil {
		removeErr := os.Remove(tmpName)
		if errors.Is(err, fs.ErrExist) {
			client, readErr := readLocalSigningClient(alg, path)
			return client, errors.Join(readErr, removeErr)
		}
		return nil, errors.Join(fmt.Errorf("publish persistent local key: %w", err), removeErr)
	}
	if err := os.Remove(tmpName); err != nil {
		return nil, fmt.Errorf("remove persistent local key temporary file: %w", err)
	}

	return client, nil
}

func readLocalSigningClient(alg jose.SignatureAlgorithm, path string) (*LocalSigningClient, error) {
	doc, err := os.ReadFile(path) //nolint:gosec // path is an application-selected local-development cache file, not request input.
	if err != nil {
		return nil, fmt.Errorf("read persistent local %s key: %w", alg, err)
	}
	block, rest := pem.Decode(doc)
	if block == nil || block.Type != "PRIVATE KEY" || len(rest) != 0 {
		return nil, fmt.Errorf("decode persistent local %s key: expected one PKCS#8 PEM block", alg)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse persistent local %s key: %w", alg, err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("parse persistent local %s key: %T is not a signer", alg, key)
	}
	if err := checkKeyMatchesAlgorithm(PublicKey{Algorithm: alg, Key: signer.Public()}); err != nil {
		return nil, fmt.Errorf("validate persistent local %s key: %w", alg, err)
	}
	return &LocalSigningClient{alg: alg, key: signer}, nil
}

// GetPublicKey returns the generated key pair's public half and the algorithm
// the client was built for. The resource name is validated exactly as the real
// client validates it, so a malformed name fails here too rather than passing in
// tests and failing in production; beyond that the name is not otherwise used,
// since this client holds exactly one key.
//
// A canceled or expired context fails the call, as it would against real KMS. A
// stand-in that kept working after the caller gave up would report a key usable
// under conditions where production reports nothing at all.
func (c *LocalSigningClient) GetPublicKey(ctx context.Context, resourceName string) (*PublicKey, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("get local %s public key: %w", c.alg, err)
	}

	if err := ValidateKeyVersionName(resourceName); err != nil {
		return nil, err
	}

	// Round-trip through PKIX/PEM rather than handing back the in-memory key.
	// GCP exports a PEM that the real client decodes and parses, and that parsing
	// is otherwise exercised nowhere — returning the key directly would leave the
	// one step where the two implementations genuinely differ untested.
	der, err := x509.MarshalPKIXPublicKey(c.key.Public())
	if err != nil {
		return nil, fmt.Errorf("marshal local %s public key: %w", c.alg, err)
	}

	key, err := parsePublicKeyPEM(string(pem.EncodeToMemory(&pem.Block{
		Type:    "PUBLIC KEY",
		Headers: nil,
		Bytes:   der,
	})))
	if err != nil {
		return nil, err
	}

	return &PublicKey{Algorithm: c.alg, Key: key}, nil
}

// AsymmetricSign signs a digest with the in-process key, returning the
// signature in the same provider encoding real KMS would: PKCS#1 v1.5 for RSA,
// ASN.1 DER for ECDSA. Callers therefore exercise the same conversion and
// verification code they would against GCP.
//
// The digest width and the algorithm are checked as strictly as the real client
// checks them, so a caller cannot pass something here that KMS would reject, and
// a canceled or expired context fails the call as it would against real KMS.
// There are no checksums to verify: nothing crosses a wire.
func (c *LocalSigningClient) AsymmetricSign(ctx context.Context, resourceName string, alg jose.SignatureAlgorithm, digest []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sign digest with local %s key: %w", c.alg, err)
	}

	if err := ValidateKeyVersionName(resourceName); err != nil {
		return nil, err
	}

	if alg != c.alg {
		return nil, fmt.Errorf("%w: key signs %s, caller asked for %s", ErrUnsupportedAlgorithm, c.alg, alg)
	}

	hash, err := digestHash(alg)
	if err != nil {
		return nil, err
	}

	if len(digest) != hash.Size() {
		return nil, fmt.Errorf("digest is %d bytes, expected %d for %s", len(digest), hash.Size(), alg)
	}

	// crypto.Signer yields exactly the encodings GCP KMS returns: PKCS#1 v1.5 for
	// RSA and ASN.1 DER for ECDSA.
	signature, err := c.key.Sign(rand.Reader, digest, hash)
	if err != nil {
		return nil, fmt.Errorf("sign digest with local %s key: %w", alg, err)
	}

	return signature, nil
}

// Close is a no-op: the key lives in memory and there is no connection to
// release. It exists so this client satisfies SigningClient, which lets tests
// exercise the same caller-owns-the-lifetime pattern production uses.
func (c *LocalSigningClient) Close() error {
	return nil
}
