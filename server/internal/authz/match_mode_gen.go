// Code generated from registry/match_mode.go by authzgen. DO NOT EDIT.
// Regenerate with `mise run gen:authz`.

package authz

// MatchMode is how an allow grant's selector is compared with a check.
type MatchMode int

const (
	// MatchLoose skips grant keys the check does not carry. It is today's
	// default for every resource type except auth clients.
	MatchLoose MatchMode = iota
	// MatchStrict requires every grant key to be present in the check.
	MatchStrict
)
