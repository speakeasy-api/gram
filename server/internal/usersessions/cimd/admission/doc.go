// Package admission implements CIMD admission control: the per-issuer
// policy deciding WHICH Client ID Metadata Document URLs a Gram user-session
// authorization server will accept, evaluated before any document is
// fetched. A denied client_id costs a map lookup — no outbound request, no
// timeout.
//
// It is deliberately a leaf: it imports nothing from usersessions and
// touches no database. It has to be, because the dependency runs the other
// way — the management API in usersessions imports this package to serve
// the preset catalog. That direction lets the enforcement path in
// internal/mcp, the resolver in internal/usersessions/cimd, and those
// handlers all share one definition of the policy.
//
// The policy has two halves, and this package owns only the first: an
// in-memory decision over the compile-time preset catalog, plus the mode
// resolution rules. The second half — an issuer's own custom URL rows — is a
// database lookup the caller performs, and only when Evaluate asks for it.
//
// Layout is one type per file:
//
//   - admission.go: Evaluate, the entry point combining the rest
//   - mode.go: Mode and the rules resolving a stored column value onto one
//   - decision.go: Decision, what Evaluate returns
//   - outcome.go: Outcome, whether a client was admitted, denied, or needs
//     the caller's custom-URL lookup
//   - admit_reason.go: AdmitReason, why an admission happened
//   - denial_reason.go: DenialReason, why a denial happened
//   - denial_error.go: DenialError, a denial as a Go error
//   - catalog.go: the curated preset catalog
//   - pattern.go: wildcard matching for catalog entries
//   - metrics.go: the cimd.admission.decisions instrument
package admission
