// Code generated from registry/match_mode.go by authzgen. DO NOT EDIT.
// Regenerate with `mise run gen:authz`.

package authz

// MatchMode is how an allow grant's selector is compared with a check.
type MatchMode int

const (
	// MatchStandard requires every grant key to be present in the check,
	// except where the check is one of the few built to leave a key out: a
	// probe, a connection against a tool limit, and a risk policy audience
	// question against a server limit.
	MatchStandard MatchMode = iota
	// MatchStrict requires every grant key to be present in the check, with
	// no exception.
	MatchStrict
)
