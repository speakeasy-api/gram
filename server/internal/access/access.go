package access

// Check describes a single scope/resource authorization requirement.
type Check struct {
	Scope      Scope
	ResourceID string
}

// Expand returns all grants that would satisfy this check: the check itself
// (exact resource and wildcard), any higher-privilege scopes that imply it
// (exact and wildcard), and always a ScopeRoot wildcard grant.
func (c Check) Expand() []Check {
	checks := []Check{
		{Scope: ScopeRoot, ResourceID: WildcardResource},
	}
	scopes := append([]Scope{c.Scope}, scopeExpansions[c.Scope]...)
	for _, s := range scopes {
		checks = append(checks,
			Check{Scope: s, ResourceID: c.ResourceID},
			Check{Scope: s, ResourceID: WildcardResource},
		)
	}
	return checks
}

func validateInput(c Check) error {
	switch c.ResourceID {
	case "":
		return InvalidCheck(c.Scope, c.ResourceID)
	case WildcardResource:
		return InvalidCheck(c.Scope, c.ResourceID)
	default:
		return nil
	}
}
