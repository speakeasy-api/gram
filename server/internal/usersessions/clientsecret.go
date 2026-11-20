package usersessions

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// MintClientSecret returns a new client secret and the bcrypt hash stored in
// user_session_clients.client_secret_hash. The secret is 32 bytes of
// cryptographically random data, base64url-encoded; only the hash is kept, so
// the caller must hand the secret over in the same response.
func MintClientSecret() (secret string, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("read random bytes: %w", err)
	}
	secret = base64.RawURLEncoding.EncodeToString(b)

	hashed, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", "", fmt.Errorf("hash client secret: %w", err)
	}

	return secret, string(hashed), nil
}
