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
	"fmt"

	jose "github.com/go-jose/go-jose/v4"
)

// rsaLocalKeyBits matches the smallest RSA size GCP KMS offers for signing, so
// the stand-in produces signatures the same width as production.
const rsaLocalKeyBits = 2048

var _ SigningClient = (*LocalSigningClient)(nil)

// LocalSigningClient is an in-process SigningClient backed by a key pair generated at
// construction, for use where no GCP network path exists: CI, and local
// development without KMS access.
//
// It is a faithful stand-in for the transport rather than a shortcut around it.
// Signatures come back in the provider's own encodings — PKCS#1 v1.5 for RSA,
// ASN.1 DER for ECDSA — so callers exercise the same parsing, conversion and
// verification code they would against real KMS. The private key is ephemeral
// and never leaves the process.
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
