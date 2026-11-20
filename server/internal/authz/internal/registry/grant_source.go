// Where the grants of a scope are set.
//
// authzgen copies everything below the package line into package authz, so
// the model and the evaluator share these names without importing each other.
// Keep this file free of imports and of anything only the registry needs.
package registry

// GrantSource says where the grants of a scope are set: through roles, or
// through risk policies. Reads can show every grant, but a write must come
// from the scope's source, so one part of the product cannot replace grants
// that another part sets. The stored values predate the name: "access" means
// roles.
type GrantSource string

const (
	GrantSourceRoles      GrantSource = "access"
	GrantSourceRiskPolicy GrantSource = "risk_policy"
)
