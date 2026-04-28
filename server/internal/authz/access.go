package authz

import "maps"

// Check describes a single authorization requirement. ResourceID identifies the
// resource. ResourceKind overrides the kind derived from the scope — leave empty
// to derive automatically. Dimensions carry optional narrowing attributes (tool,
// disposition, collection) for multi-dimensional checks.
type Check struct {
	Scope        Scope
	ResourceKind string
	ResourceID   string
	Dimensions   map[string]string
}

// selector converts the check into a Selector for matching against grant selectors.
func (c Check) selector() Selector {
	kind := c.ResourceKind
	if kind == "" {
		kind = ResourceKindForScope(c.Scope)
	}
	s := Selector{
		"resource_kind": kind,
		"resource_id":   c.ResourceID,
	}
	maps.Copy(s, c.Dimensions)
	return s
}

// expand returns all scope variants that would satisfy this check: the check's
// own scope, any higher-privilege scopes that imply it, and ScopeRoot. Selector
// matching handles wildcard grants natively ({"resource_id":"*"} matches any
// check), so we only need one entry per scope level.
func (c Check) expand() []Check {
	checks := []Check{
		{Scope: ScopeRoot, ResourceKind: c.ResourceKind, ResourceID: c.ResourceID, Dimensions: c.Dimensions},
	}
	scopes := append([]Scope{c.Scope}, scopeExpansions[c.Scope]...)
	for _, s := range scopes {
		checks = append(checks, Check{Scope: s, ResourceKind: c.ResourceKind, ResourceID: c.ResourceID, Dimensions: c.Dimensions})
	}
	return checks
}
