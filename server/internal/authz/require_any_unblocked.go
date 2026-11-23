package authz

import (
	"context"
)

// RequireAnyUnblocked accepts any allow alternative, but rejects explicit
// exclusions matching ANY supplied check first. Use it when project-wide and
// resource-specific grants authorize the same operation: a broad allow must
// not bypass an exclusion on the actual resource (or vice versa).
//
// Checks carry base scopes, not blocked scopes, and must include the concrete
// resource dimensions. Exclusions match exactly, open no key, and never expand
// to root. Every admitted policy can exclude; one allow alternative must still
// be satisfied by all policies, just as with RequireAny. An exclusion inherited
// from a role or user:all does not reject a check that a direct grant naming
// the concrete resource satisfies.
func (e *Engine) RequireAnyUnblocked(ctx context.Context, checks ...Check) error {
	return e.Require(ctx, AnyOf(checks...))
}

// firstUnoverriddenExclusion returns the first check an exclusion rejects,
// bound to the caller, or nil when none does. An exclusion inherited from a
// role or user:all does not count when a direct grant naming the concrete
// resource satisfies the check.
func firstUnoverriddenExclusion(authorization grantAuthorization, checks []Check) (*Check, error) {
	for _, check := range checks {
		check = authorization.bind(check)
		exclusionScope, ok := ExclusionScopeFor(check.Scope)
		if !ok {
			continue
		}
		exclusion := check
		exclusion.exact = true
		exclusion.Scope = exclusionScope
		// Preserve the base resource kind, including explicit overrides.
		if exclusion.ResourceKind == "" {
			exclusion.ResourceKind = ResourceKindForScope(check.Scope)
		}
		for _, policy := range authorization.policiesFor(check) {
			grant, _ := matchingGrant(policy, expandWithoutRoot(exclusion))
			if grant == nil {
				continue
			}
			override, _, err := directOverride(policy, check)
			if err != nil {
				return nil, err
			}
			if override != nil {
				continue
			}
			return &check, nil
		}
	}
	return nil, nil
}
