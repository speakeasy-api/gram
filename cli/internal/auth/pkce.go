package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// PKCE is the proof key for one browser login (RFC 7636). The challenge goes
// to the dashboard in the sign-in URL; the verifier never leaves this process
// and is what redeems the one-time code the dashboard sends back. A code
// minted for another login, or read off the callback by another local
// process, cannot be redeemed without it.
type PKCE struct {
	Verifier  string
	Challenge string
}

// pkceMethod is the only challenge method the server accepts.
const pkceMethod = "S256"

// NewPKCE returns a fresh verifier from 32 random bytes and its S256
// challenge.
func NewPKCE() (PKCE, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return PKCE{}, fmt.Errorf("read random bytes: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))

	return PKCE{
		Verifier:  verifier,
		Challenge: base64.RawURLEncoding.EncodeToString(sum[:]),
	}, nil
}
