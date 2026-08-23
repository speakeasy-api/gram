package gateway

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
)

// Decryptor opens secrets written by gram-server's encryption client
// (server/internal/encryption): base64(nonce || AES-256-GCM ciphertext) under
// a shared 32-byte key. The gateway only ever reads secrets, so no Encrypt is
// implemented here; the format is owned by the server package and any change
// there must keep the two in lockstep. Go's internal-package rule keeps the
// server implementation unimportable from this tree.
type Decryptor struct {
	key []byte
}

func NewDecryptor(base64Key string) (*Decryptor, error) {
	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return nil, fmt.Errorf("decode encryption key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid AES-256 key size: %d bytes", len(key))
	}
	return &Decryptor{key: key}, nil
}

func (d *Decryptor) Decrypt(ciphertextStr string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextStr)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(d.key)
	if err != nil {
		return "", fmt.Errorf("init cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("init gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("ciphertext too short")
	}
	nonce, sealed := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("open ciphertext: %w", err)
	}
	return string(plaintext), nil
}
