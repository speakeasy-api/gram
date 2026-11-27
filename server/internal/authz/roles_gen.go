// Code generated from registry/roles.go by authzgen. DO NOT EDIT.
// Regenerate with `mise run gen:authz`.

package authz

// The names of the built-in roles, and the value that stands for every
// resource in a selector.
const (
	SystemRoleAdmin          = "admin"
	SystemRoleMember         = "member"
	SystemRoleSuperAdmin     = "super_admin"
	SystemRoleStandardAdmin  = "standard_admin"
	SystemRoleStandardMember = "standard_member"
	WildcardResource         = "*"
)
