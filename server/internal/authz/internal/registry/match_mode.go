// How an allow grant's selector is compared with a check.
//
// authzgen copies everything below the package line into package authz, so
// the model and the evaluator share these names without importing each other.
// Keep this file free of imports and of anything only the registry needs.
package registry

// MatchMode is how an allow grant's selector is compared with a check.
type MatchMode int

const (
	// MatchLoose skips grant keys the check does not carry. It is today's
	// default for every resource type except auth clients.
	MatchLoose MatchMode = iota
	// MatchStrict requires every grant key to be present in the check.
	MatchStrict
)
