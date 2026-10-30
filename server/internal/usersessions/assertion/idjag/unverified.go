package idjag

import (
	"encoding/json"

	assertioncore "github.com/speakeasy-api/gram/server/internal/usersessions/assertion"
)

// UnverifiedResource reads an ID-JAG's resource claim without verifying it,
// for routing only: Validate must still check the claim against the server.
// It reports false only when the assertion is absent or carries no resource
// claim; an unreadable assertion reports true with an error.
func UnverifiedResource(raw string) (string, bool, error) {
	if raw == "" {
		return "", false, nil
	}
	token, err := assertioncore.ParseSigned(raw, maxBytes)
	if err != nil {
		return "", true, reject(ReasonMalformed, err)
	}
	// Decoded with encoding/json as Validate does; go-jose's decoder is case-sensitive.
	var payload json.RawMessage
	if err := token.UnsafeClaimsWithoutVerification(&payload); err != nil {
		return "", true, reject(ReasonMalformed, err)
	}
	var claims struct {
		Resource json.RawMessage `json:"resource,omitempty"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", true, reject(ReasonMalformed, err)
	}
	resource, ok, err := parseResourceClaim(claims.Resource)
	if err != nil {
		return "", true, reject(ReasonResourceMismatch, err)
	}
	return resource, ok, nil
}
