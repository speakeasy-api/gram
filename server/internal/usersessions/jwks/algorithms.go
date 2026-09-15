package jwks

import (
	"slices"

	"github.com/go-jose/go-jose/v4"
)

// allowedSignatureAlgorithms is the single algorithm allowlist both assertion
// consumers share: every asymmetric JWS algorithm go-jose supports, and
// nothing else. "none" is excluded because an unsigned assertion proves
// nothing, and every HS* is excluded because an HMAC verification key IS the
// signing key — a verifier holding it could forge the assertions it exists to
// check. Per-client or per-issuer algorithm pinning is deliberately not
// stored anywhere; this list is the reason.
var allowedSignatureAlgorithms = []jose.SignatureAlgorithm{
	jose.RS256,
	jose.RS384,
	jose.RS512,
	jose.PS256,
	jose.PS384,
	jose.PS512,
	jose.ES256,
	jose.ES384,
	jose.ES512,
	jose.EdDSA,
}

// AllowedSignatureAlgorithms returns the shared assertion algorithm
// allowlist, for callers that parse assertions (go-jose's ParseSigned
// requires the permitted algorithms up front). The result is a copy, so a
// caller cannot edit the policy for everyone else.
func AllowedSignatureAlgorithms() []jose.SignatureAlgorithm {
	return slices.Clone(allowedSignatureAlgorithms)
}

// isAllowedSignatureAlgorithm reports whether a key's declared alg member is
// on the allowlist. Only called for keys that declare one — absence of the
// optional member is never grounds for rejection.
func isAllowedSignatureAlgorithm(alg string) bool {
	return slices.Contains(allowedSignatureAlgorithms, jose.SignatureAlgorithm(alg))
}
