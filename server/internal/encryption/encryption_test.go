package encryption_test

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/encryption"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return key
}

func newClient(t *testing.T) *encryption.Client {
	t.Helper()
	c, err := encryption.NewWithBytes(newKey(t))
	require.NoError(t, err)
	return c
}

func TestNew_Success(t *testing.T) {
	t.Parallel()

	encoded := base64.StdEncoding.EncodeToString(newKey(t))
	c, err := encryption.New(encoded)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNew_InvalidBase64(t *testing.T) {
	t.Parallel()

	c, err := encryption.New("!!!not-base64!!!")
	require.Error(t, err)
	require.Nil(t, c)
	require.Contains(t, err.Error(), "decode base64 key")
}

func TestNew_WrongKeySize(t *testing.T) {
	t.Parallel()

	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	c, err := encryption.New(short)
	require.Error(t, err)
	require.Nil(t, c)
	require.Contains(t, err.Error(), "invalid AES-256 key size")
}

func TestNew_EmptyKey(t *testing.T) {
	t.Parallel()

	c, err := encryption.New("")
	require.Error(t, err)
	require.Nil(t, c)
}

func TestNewWithBytes_Success(t *testing.T) {
	t.Parallel()

	c, err := encryption.NewWithBytes(newKey(t))
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewWithBytes_TooShort(t *testing.T) {
	t.Parallel()

	for _, size := range []int{0, 1, 15, 16, 24, 31} {
		c, err := encryption.NewWithBytes(make([]byte, size))
		require.Error(t, err, "size=%d", size)
		require.Nil(t, c)
		require.Contains(t, err.Error(), "invalid AES-256 key size")
	}
}

func TestNewWithBytes_TooLong(t *testing.T) {
	t.Parallel()

	for _, size := range []int{33, 48, 64} {
		c, err := encryption.NewWithBytes(make([]byte, size))
		require.Error(t, err, "size=%d", size)
		require.Nil(t, c)
	}
}

func TestNewWithBytes_NilKey(t *testing.T) {
	t.Parallel()

	c, err := encryption.NewWithBytes(nil)
	require.Error(t, err)
	require.Nil(t, c)
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	t.Parallel()

	c := newClient(t)
	plaintext := "hello, gram"

	ct, err := c.Encrypt([]byte(plaintext))
	require.NoError(t, err)
	require.NotEmpty(t, ct)
	require.NotEqual(t, plaintext, ct)

	pt, err := c.Decrypt(ct)
	require.NoError(t, err)
	require.Equal(t, plaintext, pt)
}

func TestEncryptDecrypt_EmptyPlaintext(t *testing.T) {
	t.Parallel()

	c := newClient(t)

	ct, err := c.Encrypt([]byte{})
	require.NoError(t, err)
	require.NotEmpty(t, ct)

	pt, err := c.Decrypt(ct)
	require.NoError(t, err)
	require.Equal(t, "", pt)
}

func TestEncryptDecrypt_NilPlaintext(t *testing.T) {
	t.Parallel()

	c := newClient(t)

	ct, err := c.Encrypt(nil)
	require.NoError(t, err)

	pt, err := c.Decrypt(ct)
	require.NoError(t, err)
	require.Equal(t, "", pt)
}

func TestEncryptDecrypt_BinaryPlaintext(t *testing.T) {
	t.Parallel()

	c := newClient(t)
	plaintext := []byte{0x00, 0x01, 0xff, 0xfe, 0x80, 0x7f, 0x00}

	ct, err := c.Encrypt(plaintext)
	require.NoError(t, err)

	pt, err := c.Decrypt(ct)
	require.NoError(t, err)
	require.Equal(t, string(plaintext), pt)
}

func TestEncryptDecrypt_Unicode(t *testing.T) {
	t.Parallel()

	c := newClient(t)
	plaintext := "héllo 🔐 世界"

	ct, err := c.Encrypt([]byte(plaintext))
	require.NoError(t, err)

	pt, err := c.Decrypt(ct)
	require.NoError(t, err)
	require.Equal(t, plaintext, pt)
}

func TestEncryptDecrypt_LargePlaintext(t *testing.T) {
	t.Parallel()

	c := newClient(t)
	plaintext := strings.Repeat("A", 1<<20) // 1 MiB

	ct, err := c.Encrypt([]byte(plaintext))
	require.NoError(t, err)

	pt, err := c.Decrypt(ct)
	require.NoError(t, err)
	require.Equal(t, plaintext, pt)
}

func TestEncrypt_NonceIsRandom(t *testing.T) {
	t.Parallel()

	c := newClient(t)
	plaintext := []byte("same input")

	ct1, err := c.Encrypt(plaintext)
	require.NoError(t, err)

	ct2, err := c.Encrypt(plaintext)
	require.NoError(t, err)

	require.NotEqual(t, ct1, ct2, "two encryptions of the same plaintext must differ (random nonce)")

	pt1, err := c.Decrypt(ct1)
	require.NoError(t, err)
	pt2, err := c.Decrypt(ct2)
	require.NoError(t, err)
	require.Equal(t, string(plaintext), pt1)
	require.Equal(t, string(plaintext), pt2)
}

func TestEncrypt_OutputIsValidBase64(t *testing.T) {
	t.Parallel()

	c := newClient(t)
	ct, err := c.Encrypt([]byte("payload"))
	require.NoError(t, err)

	_, err = base64.StdEncoding.DecodeString(ct)
	require.NoError(t, err)
}

func TestDecrypt_WrongKeyFails(t *testing.T) {
	t.Parallel()

	a := newClient(t)
	b := newClient(t)

	ct, err := a.Encrypt([]byte("secret"))
	require.NoError(t, err)

	pt, err := b.Decrypt(ct)
	require.Error(t, err)
	require.Empty(t, pt)
	require.Contains(t, err.Error(), "decryption error")
}

func TestDecrypt_TamperedCiphertextFails(t *testing.T) {
	t.Parallel()

	c := newClient(t)
	ct, err := c.Encrypt([]byte("authenticated payload"))
	require.NoError(t, err)

	raw, err := base64.StdEncoding.DecodeString(ct)
	require.NoError(t, err)

	// Flip a bit in the ciphertext body (past the 12-byte GCM nonce).
	require.Greater(t, len(raw), 12)
	raw[len(raw)-1] ^= 0x01
	tampered := base64.StdEncoding.EncodeToString(raw)

	pt, err := c.Decrypt(tampered)
	require.Error(t, err)
	require.Empty(t, pt)
	require.Contains(t, err.Error(), "decryption error")
}

func TestDecrypt_TamperedNonceFails(t *testing.T) {
	t.Parallel()

	c := newClient(t)
	ct, err := c.Encrypt([]byte("authenticated payload"))
	require.NoError(t, err)

	raw, err := base64.StdEncoding.DecodeString(ct)
	require.NoError(t, err)
	raw[0] ^= 0x01
	tampered := base64.StdEncoding.EncodeToString(raw)

	pt, err := c.Decrypt(tampered)
	require.Error(t, err)
	require.Empty(t, pt)
}

func TestDecrypt_TruncatedCiphertextFails(t *testing.T) {
	t.Parallel()

	c := newClient(t)
	short := base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03})

	pt, err := c.Decrypt(short)
	require.Error(t, err)
	require.Empty(t, pt)
	require.Contains(t, err.Error(), "ciphertext too short")
}

func TestDecrypt_InvalidBase64Fails(t *testing.T) {
	t.Parallel()

	c := newClient(t)
	pt, err := c.Decrypt("!!!not-base64!!!")
	require.Error(t, err)
	require.Empty(t, pt)
	require.Contains(t, err.Error(), "decryption error")
}

func TestDecrypt_EmptyStringFails(t *testing.T) {
	t.Parallel()

	c := newClient(t)
	pt, err := c.Decrypt("")
	require.Error(t, err)
	require.Empty(t, pt)
}

func TestDecrypt_ErrorUnwrapsBase64Cause(t *testing.T) {
	t.Parallel()

	c := newClient(t)
	_, err := c.Decrypt("!!!not-base64!!!")
	require.Error(t, err)

	var b64Err base64.CorruptInputError
	require.True(t, errors.As(err, &b64Err), "decryption error should unwrap to base64.CorruptInputError, got: %v", err)
}

func TestDecrypt_AcrossClientsWithSameKey(t *testing.T) {
	t.Parallel()

	key := newKey(t)
	enc, err := encryption.NewWithBytes(key)
	require.NoError(t, err)
	dec, err := encryption.NewWithBytes(key)
	require.NoError(t, err)

	ct, err := enc.Encrypt([]byte("portable"))
	require.NoError(t, err)

	pt, err := dec.Decrypt(ct)
	require.NoError(t, err)
	require.Equal(t, "portable", pt)
}
