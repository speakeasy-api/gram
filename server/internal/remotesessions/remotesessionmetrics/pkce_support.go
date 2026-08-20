package remotesessionmetrics

import (
	"slices"
)

// PKCESupportState buckets an issuer's stored code_challenge_methods_supported
// by what PKCE enforcement (AIS-566) would conclude from it. The four states
// partition every possible column value, so a breakdown by this dimension
// accounts for every recorded flow.
type PKCESupportState string

const (
	// PKCESupportUncaptured: the column is NULL — neither discovery nor an
	// operator has captured the field for this issuer. A coverage gap in
	// Gram's own data, not a statement about the upstream; enforcement must
	// not treat it as a refusal case.
	PKCESupportUncaptured PKCESupportState = "uncaptured"

	// PKCESupportNone: captured, and the issuer advertises no methods at all.
	// The state MCP-mandated enforcement would refuse.
	PKCESupportNone PKCESupportState = "none"

	// PKCESupportSupported: captured, and S256 — the only method Gram sends —
	// is advertised.
	PKCESupportSupported PKCESupportState = "supported"

	// PKCESupportUnsupported: captured and non-empty, but S256 is absent.
	// Also a refusal state under enforcement, kept separate from none because
	// it names an upstream that supports PKCE just not the method Gram uses.
	PKCESupportUnsupported PKCESupportState = "unsupported"
)

// ClassifyPKCESupport classifies a stored code_challenge_methods_supported
// value. The S256 match is exact and case-sensitive: RFC 7636 defines the
// method name as "S256", and a lowercase "s256" in the wild is itself a
// misconfiguration signal that must not be normalized away.
func ClassifyPKCESupport(methods []string) PKCESupportState {
	switch {
	case methods == nil:
		return PKCESupportUncaptured
	case len(methods) == 0:
		return PKCESupportNone
	case slices.Contains(methods, "S256"):
		return PKCESupportSupported
	default:
		return PKCESupportUnsupported
	}
}
