package authz

// Check describes a single scope/resource authorization requirement.
type Check struct {
	Scope      Scope
	ResourceID string
}

// selector converts the check into a Selector for matching against grants.
// Derives resource_kind from the check's scope.
func (c Check) selector() Selector {
	return NewSelector(c.Scope, c.ResourceID)
}

// expand returns all scope variants that would satisfy this check: the check's
// own scope, any higher-privilege scopes that imply it, and ScopeRoot. Selector
// matching handles wildcard grants natively ({"resource_id":"*"} matches any
// check), so we only need one entry per scope level.
func (c Check) expand() []Check {
	checks := []Check{
		{Scope: ScopeRoot, ResourceID: c.ResourceID},
	}
	scopes := append([]Scope{c.Scope}, scopeExpansions[c.Scope]...)
	for _, s := range scopes {
		checks = append(checks, Check{Scope: s, ResourceID: c.ResourceID})
	}
	return checks
}
