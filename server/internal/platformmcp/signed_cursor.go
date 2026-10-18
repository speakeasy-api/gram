package platformmcp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

var errSignedCursorKey = errors.New("platform mcp signed cursor key is not configured")

// signedCursorKey is the HMAC key for one kind of opaque token a caller holds
// between calls — a page cursor or a version token. Each kind derives its own
// key from the shared material under a distinct domain, so a token minted for
// one listing never verifies in another.
type signedCursorKey []byte

// newSignedCursorKey returns nil when no key material is configured, and every
// seal and open against a nil key fails closed.
func newSignedCursorKey(domain, keyMaterial string) signedCursorKey {
	if keyMaterial == "" {
		return nil
	}
	key := sha256.Sum256([]byte(domain + ":" + keyMaterial))
	return key[:]
}

// sealCursor returns base64url(json(cursor) || HMAC-SHA256(key, json(cursor))).
// The payload is integrity-protected, not encrypted: it carries only values
// the caller is already entitled to see.
func sealCursor[T any](key signedCursorKey, cursor T) (string, error) {
	if len(key) == 0 {
		return "", errSignedCursorKey
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("marshal signed cursor payload: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	token := make([]byte, 0, len(payload)+sha256.Size)
	token = append(token, payload...)
	token = append(token, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(token), nil
}

// openCursor verifies and decodes a token sealCursor produced under the same
// key. It reports only whether the token is authentic and well formed; binding
// the payload to the principal and scope it is presented under is the caller's
// job, because that is what differs between cursor kinds.
func openCursor[T any](key signedCursorKey, value string) (T, bool) {
	var zero T
	if len(key) == 0 || value == "" {
		return zero, false
	}
	token, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(token) <= sha256.Size {
		return zero, false
	}
	payload, signature := token[:len(token)-sha256.Size], token[len(token)-sha256.Size:]
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return zero, false
	}
	var cursor T
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return zero, false
	}
	return cursor, true
}
