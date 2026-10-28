package idjag

import (
	assertioncore "github.com/speakeasy-api/gram/server/internal/usersessions/assertion"
)

// UnverifiedResource reads an ID-JAG's resource claim without verifying it,
// for routing only: Validate must still check the claim against the server.
// It reports false when the assertion carries no resource claim.
func UnverifiedResource(raw string) (string, bool, error) {
	token, err := assertioncore.ParseSigned(raw, maxBytes)
	if err != nil {
		return "", false, reject(ReasonMalformed, err)
	}
	var extra additionalClaims
	if err := token.UnsafeClaimsWithoutVerification(&extra); err != nil {
		return "", false, reject(ReasonMalformed, err)
	}
	resource, ok, err := extra.resource()
	if err != nil {
		return "", true, reject(ReasonResourceMismatch, err)
	}
	return resource, ok, nil
}
