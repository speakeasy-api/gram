// Package mcpauthztest builds platform signers for tests.
package mcpauthztest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
)

// testKeyBits is the RSA modulus size; 2048 is the smallest the platform
// signer accepts.
const testKeyBits = 2048

type keyPEM struct{ private, public string }

// testKeyPEM generates one key per test binary. Generating a 2048-bit RSA key
// for every fixture is the slow part; tests never depend on keys differing.
var testKeyPEM = sync.OnceValues(func() (keyPEM, error) {
	key, err := rsa.GenerateKey(rand.Reader, testKeyBits)
	if err != nil {
		return keyPEM{}, err
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return keyPEM{}, err
	}
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return keyPEM{}, err
	}
	return keyPEM{
		private: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Headers: nil, Bytes: private})),
		public:  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Headers: nil, Bytes: public})),
	}, nil
})

// NewIssuer returns a platform signer over the test binary's shared key.
func NewIssuer(t *testing.T) *mcpauthz.Issuer {
	t.Helper()

	keys, err := testKeyPEM()
	require.NoError(t, err)
	issuer, err := mcpauthz.New(keys.private, keys.public, "https://platform.example.invalid", false)
	require.NoError(t, err)
	return issuer
}
