package clientauth

import (
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

// AssertionType is the only client_assertion_type this server accepts
// (RFC 7523 §2.2).
const AssertionType = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"

// maxAssertionBytes bounds the assertion before it is parsed. A real client
// assertion is well under 2 KiB even with an RSA-4096 signature; the cap
// keeps the parsing and key-resolution cost of a rejected assertion
// proportional to what a legitimate one could ever be, rather than to
// whatever body limit the calling endpoint happens to apply.
const maxAssertionBytes = 8 * 1024

// allowedAlgorithms is the shared assertion algorithm allowlist, taken once
// at initialization. jwks returns a fresh copy per call so callers cannot edit
// the policy; holding one copy here spares that allocation on every request.
var allowedAlgorithms = jwks.AllowedSignatureAlgorithms()

// UnverifiedClientID reads the client identifier an assertion claims, without
// checking its signature.
//
// RFC 7521 §4.2 makes the client_id form parameter optional when a client
// assertion is present, since sub already identifies the client. This gives
// the caller a row to look up in that case, and nothing more: the value is
// unauthenticated, and Verify independently re-checks iss and sub against the
// client_id after the signature verifies. Selecting a row by an unverified
// claim grants nothing, because a caller could equally have typed the same
// value into the form parameter.
//
// The algorithm allowlist still applies, so an assertion this cannot parse
// would not have verified either.
func UnverifiedClientID(assertion string) (string, error) {
	token, err := parseAssertion(assertion)
	if err != nil {
		return "", err
	}
	var claims jwt.Claims
	if err := token.UnsafeClaimsWithoutVerification(&claims); err != nil {
		return "", rejectWith(ReasonMalformed, err)
	}
	if claims.Issuer == "" || claims.Issuer != claims.Subject {
		return "", reject(ReasonSubjectMismatch, "iss and sub must both be present and equal")
	}
	return claims.Subject, nil
}

// parseAssertion bounds and parses an assertion under the algorithm
// allowlist, so `none` and every HS* are a parse failure rather than something
// a later check must catch.
func parseAssertion(assertion string) (*jwt.JSONWebToken, error) {
	if len(assertion) > maxAssertionBytes {
		return nil, reject(ReasonMalformed, "client_assertion exceeds %d bytes", maxAssertionBytes)
	}
	token, err := jwt.ParseSigned(assertion, allowedAlgorithms)
	if err != nil {
		return nil, rejectWith(ReasonMalformed, err)
	}
	return token, nil
}

// Assertion is the client assertion a request presented, taken verbatim from
// its form parameters.
type Assertion struct {
	// Value is the client_assertion parameter.
	Value string

	// Type is the client_assertion_type parameter.
	Type string
}

// Presented reports whether a request carries anything that looks like an
// attempt at assertion-based authentication. Callers use it to tell a public
// client presenting nothing from one presenting an assertion it was never
// registered to use, which is refused rather than treated as a free upgrade.
func (a Assertion) Presented() bool {
	return a.Value != "" || a.Type != ""
}
