package authz

import "fmt"

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

// ScopeGrantSource returns where the grants of scope are set.
func ScopeGrantSource(scope Scope) (GrantSource, bool) {
	owner, ok := scopeGrantSource[scope]
	return owner, ok
}

// ValidateScopeGrantSource verifies that scope's grants are set through writer.
func ValidateScopeGrantSource(writer GrantSource, scope Scope) error {
	owner, ok := ScopeGrantSource(scope)
	if !ok {
		return fmt.Errorf("scope %q is not managed by a grant surface", scope)
	}
	if owner != writer {
		return fmt.Errorf("scope %q is managed by %q grants, not %q grants", scope, owner, writer)
	}
	return nil
}

// ValidateGrantSource verifies that every grant in a write payload is set
// through writer, the source handling that write.
func ValidateGrantSource(writer GrantSource, grants []*RoleGrant) error {
	for _, grant := range grants {
		if grant == nil {
			continue
		}
		if err := ValidateScopeGrantSource(writer, Scope(grant.Scope)); err != nil {
			return err
		}
	}
	return nil
}
