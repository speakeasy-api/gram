// The built-in role names and the wildcard resource.
//
// authzgen copies everything below the package line into package authz, so
// the model and the evaluator share these names without importing each other.
// Keep this file free of imports and of anything only the registry needs.
package registry

// The names of the built-in roles, and the value that stands for every
// resource in a selector.
const (
	SystemRoleAdmin  = "admin"
	SystemRoleMember = "member"
	WildcardResource = "*"
)
