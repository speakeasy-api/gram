package devidptest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

const sharedKeyBits = 2048

var (
	sharedKeyOnce sync.Once
	sharedKeyVal  *rsa.PrivateKey
	sharedKeyPEM  []byte
	sharedKeyErr  error
)

// sharedKey returns a process-wide RSA-2048 keypair generated once per test
// binary, plus its PKCS#8 PEM encoding ready to feed to keystore.New. RSA
// generation runs ~150-300ms per call; sharing keeps per-test setup at
// ~10-20ms. JWTs issued by separate Instances stay scoped by their distinct
// httptest issuer URLs, so reusing the key across instances is safe.
//
// Tests that need a distinct key (JWKS rotation, kid-mismatch scenarios) can
// pass LaunchOpts.Key.
func sharedKey(tb testing.TB) (*rsa.PrivateKey, []byte) {
	tb.Helper()
	sharedKeyOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, sharedKeyBits)
		if err != nil {
			sharedKeyErr = fmt.Errorf("generate shared rsa key: %w", err)
			return
		}
		pemBytes, err := encodeRSAPrivateKey(key)
		if err != nil {
			sharedKeyErr = err
			return
		}
		sharedKeyVal = key
		sharedKeyPEM = pemBytes
	})
	require.NoError(tb, sharedKeyErr)
	return sharedKeyVal, sharedKeyPEM
}

// encodeRSAPrivateKey serializes an RSA private key as PKCS#8 PEM. The
// dev-idp keystore accepts either PKCS#8 or PKCS#1; PKCS#8 is the modern
// default.
func encodeRSAPrivateKey(key *rsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal pkcs8 private key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}
