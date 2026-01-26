package oauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// OAuthSessionState represents the state passed through OAuth flows for session-scoped credentials.
type OAuthSessionState struct {
	SessionID string `json:"sid"`
	ToolsetID string `json:"tid"`
	ProjectID string `json:"pid"`
	Origin    string `json:"org"` // For postMessage target
	Nonce     string `json:"n"`
}

// ErrInvalidSignature is returned when the state signature is invalid.
var ErrInvalidSignature = errors.New("invalid state signature")

// ErrMalformedState is returned when the state cannot be parsed.
var ErrMalformedState = errors.New("malformed state")

// SignSessionState creates an HMAC-signed state string from the session state.
// Format: base64(json) + "." + base64(hmac)
func SignSessionState(state *OAuthSessionState, secret string) (string, error) {
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("marshal state: %w", err)
	}

	stateB64 := base64.RawURLEncoding.EncodeToString(stateJSON)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(stateJSON)
	signature := mac.Sum(nil)
	sigB64 := base64.RawURLEncoding.EncodeToString(signature)

	return stateB64 + "." + sigB64, nil
}

// ValidateSessionState validates and parses a signed state string.
func ValidateSessionState(signedState, secret string) (*OAuthSessionState, error) {
	parts := strings.SplitN(signedState, ".", 2)
	if len(parts) != 2 {
		return nil, ErrMalformedState
	}

	stateB64, sigB64 := parts[0], parts[1]

	stateJSON, err := base64.RawURLEncoding.DecodeString(stateB64)
	if err != nil {
		return nil, fmt.Errorf("%w: decode state: %w", ErrMalformedState, err)
	}

	providedSig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, fmt.Errorf("%w: decode signature: %w", ErrMalformedState, err)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(stateJSON)
	expectedSig := mac.Sum(nil)

	if !hmac.Equal(providedSig, expectedSig) {
		return nil, ErrInvalidSignature
	}

	var state OAuthSessionState
	if err := json.Unmarshal(stateJSON, &state); err != nil {
		return nil, fmt.Errorf("%w: unmarshal state: %w", ErrMalformedState, err)
	}

	return &state, nil
}
