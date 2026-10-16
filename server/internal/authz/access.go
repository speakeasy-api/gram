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

// WithStrictSelectorMatch returns a copy that only accepts grants whose every
// selector constraint is represented by this check. Use it when proving that
// requested authority is no broader than a live grant.
func (c Check) WithStrictSelectorMatch() Check {
	c.selectorMatch = selectorMatchStrict
	return c
}

func (c Check) matchesAllowSelector(selector Selector) bool {
	if c.selectorMatch == selectorMatchStrict {
		return selector.StrictMatches(c.selector())
	}
	return selector.Matches(c.selector())
}

// BindSelectorValue resolves a symbolic value for one selector dimension into
// its request-local value. It preserves principal provenance and never mutates
// the supplied grants or their selectors. Other dimensions are left unchanged.
// A matching symbol with an empty or wildcard binding rejects the whole set.
func BindSelectorValue(grants []Grant, key, symbol, value string) []Grant {
	bound := make([]Grant, 0, len(grants))
	for _, grant := range grants {
		if selected, ok := grant.Selector[key]; ok && selected == symbol {
			// Reject the whole set on an unresolved binding: dropping only a
			// matching exclusion could otherwise broaden an unrelated allow.
			if value == "" || value == WildcardResource {
				return nil
			}
			grant.Selector = maps.Clone(grant.Selector)
			grant.Selector[key] = value
		}
		bound = append(bound, grant)
	}
	return bound
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

// GrantsContainSelector reports allow-grant containment for a delegation set,
// including wildcard resource IDs. Unlike runtime checks this does not require
// a concrete resource. Every parent dimension must be retained by the candidate.
// This is allow-only: callers must separately reject overlapping exclusions.
func GrantsContainSelector(grants []Grant, scope Scope, selector Selector) bool {
	if selector[SelectorKeyResourceID] == "" || ValidateSelector(scope, selector) != nil {
		return false
	}
	check := strictSelectorCheck(scope, selector)
	grant, _ := matchingGrant(grants, check.expand())
	return grant != nil
}

func strictSelectorCheck(scope Scope, selector Selector) Check {
	dimensions := maps.Clone(selector)
	delete(dimensions, SelectorKeyResourceKind)
	delete(dimensions, SelectorKeyResourceID)
	return Check{Scope: scope, ResourceKind: selector[SelectorKeyResourceKind], ResourceID: selector[SelectorKeyResourceID], Dimensions: dimensions, selectorMatch: selectorMatchStrict}
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
