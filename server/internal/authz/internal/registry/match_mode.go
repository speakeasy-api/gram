// How an allow grant's selector is compared with a check.
//
// authzgen copies everything below the package line into package authz, so
// the model and the evaluator share these names without importing each other.
// Keep this file free of imports and of anything only the registry needs.
package registry

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
