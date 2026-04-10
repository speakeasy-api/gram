package access

// Check describes a single scope/resource authorization requirement.
type Check struct {
	Scope      Scope
	ResourceID string
}

// expand returns all grants that would satisfy this check: the check itself
// (exact resource and wildcard), any higher-privilege scopes that also satisfy it
// (exact and wildcard), and always a ScopeRoot wildcard grant.
func (c Check) expand() []Check {
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
