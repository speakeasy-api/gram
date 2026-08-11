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
	// selectorMatch controls how allow grant selectors are matched. The zero
	// value preserves the default permissive dimension matching.
	selectorMatch selectorMatchMode
}

type selectorMatchMode int

const (
	selectorMatchNormal selectorMatchMode = iota
	selectorMatchStrict
)

func (c Check) matchesAllowSelector(selector Selector) bool {
	if c.selectorMatch == selectorMatchStrict {
		return selector.StrictMatches(c.selector())
	}
	return selector.Matches(c.selector())
}

// selector converts the check into a Selector for matching against grant selectors.
func (c Check) selector() Selector {
	kind := c.ResourceKind
	if kind == "" {
		kind = ResourceKindForScope(c.Scope)
	}
	s := Selector{
		SelectorKeyResourceKind: kind,
		SelectorKeyResourceID:   c.ResourceID,
	}
	maps.Copy(s, c.Dimensions)
	return s
}

// expand returns all scope variants that would satisfy this check: the check's
// own scope, any scopes configured as expansions for it, and ScopeRoot.
// Selector matching handles wildcard grants natively ({"resource_id":"*"}
// matches any check), so we only need one entry per scope level.
func (c Check) expand() []Check {
	checks := []Check{
		{Scope: ScopeRoot, ResourceKind: c.ResourceKind, ResourceID: c.ResourceID, Dimensions: c.Dimensions, selectorMatch: c.selectorMatch},
		{Scope: c.Scope, ResourceKind: c.ResourceKind, ResourceID: c.ResourceID, Dimensions: c.Dimensions, selectorMatch: c.selectorMatch},
	}
	// Additional scopes that also satisfy this check.
	for _, s := range scopeExpansions[c.Scope] {
		checks = append(checks, Check{Scope: s, ResourceKind: c.ResourceKind, ResourceID: c.ResourceID, Dimensions: c.Dimensions, selectorMatch: c.selectorMatch})
	}
	return checks
}
