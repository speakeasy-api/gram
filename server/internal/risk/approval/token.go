package approval

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	// TokenPrefix marks a request token so agents, logs and secret scanners can
	// recognise one.
	TokenPrefix = "apr_"

	// tokenRandomBytes is the token's entropy: 32 bytes (256 bits) from the OS
	// CSPRNG, far past what online guessing could find and short enough, at 43
	// base64url characters, for a model to copy exactly.
	tokenRandomBytes = 32

	// tokenLength is the full token length: the prefix plus 43 base64url
	// characters for 32 bytes without padding.
	tokenLength = len(TokenPrefix) + 43
)

// NewToken returns a fresh request token: TokenPrefix plus 32 random bytes,
// base64url without padding. It names one approval request and is the
// watcher's bearer token for it.
func NewToken() (string, error) {
	b := make([]byte, tokenRandomBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random token bytes: %w", err)
	}
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// IsToken reports whether s has the shape of a request token. It does not say
// whether the token names a live request. Only the canonical encoding passes,
// so one token has exactly one spelling.
func IsToken(s string) bool {
	if len(s) != tokenLength || !strings.HasPrefix(s, TokenPrefix) {
		return false
	}
	encoded := s[len(TokenPrefix):]
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(decoded) != tokenRandomBytes {
		return false
	}
	return base64.RawURLEncoding.EncodeToString(decoded) == encoded
}
