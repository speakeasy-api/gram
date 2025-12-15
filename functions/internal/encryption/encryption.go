package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
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

func New(key []byte) (*Client, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid AES-256 key size: %d bytes", len(key))
	}
	return &Client{key: key}, nil
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
