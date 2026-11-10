package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
)

const (
	errCodeAESNewCipher = "ENC-1"
	errCodeNewGCM       = "ENC-2"
	errCodeNonceRead    = "ENC-3"
	errCodeGCMOpen      = "ENC-4"
	errCodeBase64Decode = "ENC-5"
)

type encryptionError struct {
	inner error
	code  string
}

func newEncryptionError(code string, err error) error {
	if err == nil {
		return nil
	}
	return &encryptionError{
		inner: err,
		code:  code,
	}
}

func (e *encryptionError) Error() string {
	return fmt.Sprintf("encryption error (%s): %s", e.code, e.inner.Error())
}

func (e *encryptionError) Unwrap() error {
	return e.inner
}

type decryptionError struct {
	inner error
	code  string
}

func newDecryptionError(code string, err error) error {
	if err == nil {
		return nil
	}
	return &decryptionError{
		inner: err,
		code:  code,
	}
}

func (e *decryptionError) Error() string {
	return fmt.Sprintf("decryption error (%s): %s", e.code, e.inner.Error())
}

func (e *decryptionError) Unwrap() error {
	return e.inner
}

type Client struct {
	key []byte
}

func New(base64Key string) (*Client, error) {
	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 key: %w", err)
	}
	return NewWithBytes(key)
}

func NewWithBytes(key []byte) (*Client, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid AES-256 key size: %d bytes", len(key))
	}
	return &Client{key: key}, nil
}

// fingerprintHexChars is how much of the key's SHA-256 a fingerprint shows:
// enough to tell keys apart across pods, far too little to recover anything.
const fingerprintHexChars = 8

// Fingerprint names the key without revealing it, so logs can show which key a pod holds.
func (e *Client) Fingerprint() string {
	sum := sha256.Sum256(e.key)
	return hex.EncodeToString(sum[:])[:fingerprintHexChars]
}

// Encrypt encrypts the plaintext using AES-GCM and returns a base64 encoded string
func (e *Client) Encrypt(plaintext []byte) (string, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", newEncryptionError(errCodeAESNewCipher, err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", newEncryptionError(errCodeNewGCM, err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", newEncryptionError(errCodeNonceRead, err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts the base64 encoded ciphertext using AES-GCM
func (e *Client) Decrypt(ciphertextStr string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextStr)
	if err != nil {
		return "", newDecryptionError(errCodeBase64Decode, err)
	}

	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", newDecryptionError(errCodeAESNewCipher, err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", newDecryptionError(errCodeNewGCM, err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", newDecryptionError(errCodeGCMOpen, err)
	}

	return string(plaintext), nil
}
