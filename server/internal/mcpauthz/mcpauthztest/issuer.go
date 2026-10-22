// Package mcpauthztest builds platform signers for tests.
package mcpauthztest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
)

// testKeyBits is the RSA modulus size; 2048 is the smallest the platform
// signer accepts.
const testKeyBits = 2048

// NewIssuer returns a platform signer over a freshly generated key.
func NewIssuer(t *testing.T) *mcpauthz.Issuer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, testKeyBits)
	require.NoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)

	issuer, err := mcpauthz.New(
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Headers: nil, Bytes: private})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Headers: nil, Bytes: public})),
		"https://platform.example.invalid",
		false,
	)
	require.NoError(t, err)
	return issuer
}
