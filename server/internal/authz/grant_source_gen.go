// Code generated from registry/grant_source.go by authzgen. DO NOT EDIT.
// Regenerate with `mise run gen:authz`.

package authz

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
