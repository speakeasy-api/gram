package gateway

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// sealLikeServer reproduces server/internal/encryption's Encrypt so the test
// proves format compatibility without importing the internal package.
func sealLikeServer(t *testing.T, key []byte, plaintext string) string {
	t.Helper()

	block, err := aes.NewCipher(key)
	require.NoError(t, err)
	gcm, err := cipher.NewGCM(block)
	require.NoError(t, err)
	nonce := make([]byte, gcm.NonceSize())
	_, err = io.ReadFull(rand.Reader, nonce)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plaintext), nil))
}

func TestDecryptorOpensServerCiphertext(t *testing.T) {
	t.Parallel()

	key := make([]byte, 32)
	_, err := io.ReadFull(rand.Reader, key)
	require.NoError(t, err)

	dec, err := NewDecryptor(base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)

	plaintext, err := dec.Decrypt(sealLikeServer(t, key, "tskey-auth-secret"))
	require.NoError(t, err)
	require.Equal(t, "tskey-auth-secret", plaintext)
}

func TestDecryptorRejectsBadInputs(t *testing.T) {
	t.Parallel()

	_, err := NewDecryptor("not-base64!!")
	require.Error(t, err)

	_, err = NewDecryptor(base64.StdEncoding.EncodeToString(make([]byte, 16)))
	require.Error(t, err)

	key := make([]byte, 32)
	dec, err := NewDecryptor(base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)

	_, err = dec.Decrypt("dG9vLXNob3J0")
	require.Error(t, err)
}
