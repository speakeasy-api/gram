package gcpkms

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"

	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
)

// PublicKey is the public half of a KMS signing key.
type PublicKey struct {
	// Algorithm is the JOSE algorithm the key version actually signs with,
	// derived from the provider rather than from anything Gram stored. Comparing
	// it against the stored algorithm is what catches a key pointed at the wrong
	// row.
	Algorithm jose.SignatureAlgorithm

	// Key is the parsed public half: *rsa.PublicKey or *ecdsa.PublicKey.
	Key crypto.PublicKey
}

// SigningClient is the transport for keys whose purpose is ASYMMETRIC_SIGN. It
// is an interface so consumers can substitute LocalSigningClient (or their own
// stand-in) in tests, where no GCP network path is reachable.
//
// The name is deliberate: a KMS key's purpose is fixed at creation, so a key
// that encrypts cannot sign and vice versa. An encryption counterpart would be a
// separate interface over Encrypt/Decrypt rather than more methods here.
//
// Signatures are returned in the provider's own encoding — PKCS#1 v1.5 for RSA,
// ASN.1 DER for ECDSA — not the JOSE encoding. Converting is the signer's job,
// which keeps this layer a faithful transport and lets verification use the
// standard library directly.
type SigningClient interface {
	// GetPublicKey fetches the key version's public half and algorithm.
	GetPublicKey(ctx context.Context, resourceName string) (*PublicKey, error)

	// AsymmetricSign signs a digest produced by the algorithm's hash.
	AsymmetricSign(ctx context.Context, resourceName string, alg jose.SignatureAlgorithm, digest []byte) ([]byte, error)

	// Close releases the underlying connection. Every client must be closed.
	io.Closer
}

// SigningClientFactory builds a SigningClient authenticated as some identity.
// Services hold one of these rather than a SigningClient, because the identity
// varies per credential and each client owns a connection that must be closed
// after use.
type SigningClientFactory func(ctx context.Context, tokenSource oauth2.TokenSource) (SigningClient, error)

var _ SigningClientFactory = NewSigningClient

// parsePublicKeyPEM decodes the SubjectPublicKeyInfo PEM a KMS key version
// exports into a usable public key. Both implementations of SigningClient go
// through it, so the stand-in exercises the same parsing the real client does.
func parsePublicKeyPEM(pemData string) (crypto.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, errors.New("parse gcp kms public key: pem contained no block")
	}

	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse gcp kms public key: %w", err)
	}

	return key, nil
}
