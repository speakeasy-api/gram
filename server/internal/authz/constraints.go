package authz

import (
	"context"
	"maps"
)

// ScopeConstraints describes how a caller's grants for one scope on one
// resource narrow that scope's optional selector dimensions.
//
// Selector matching skips dimensions a check does not constrain, so a grant
// narrowed to a dimension still satisfies a dimensionless Require. That is what
// makes row-level scoping expressible: the handler proves the caller holds the
// scope with Require, then asks for the constraints it has to apply to the data
// it is about to return.
type ScopeConstraints struct {
	// Granted reports whether at least one grant satisfies the dimensionless
	// check. It is the same answer Evaluate would give.
	Granted bool

	// Unrestricted reports whether the caller holds the scope without any
	// constraint on the requested dimensions — either through a grant that
	// leaves them unset, or one that wildcards them.
	Unrestricted bool

	// Narrowed carries one entry per narrowing grant, reduced to the requested
	// dimensions. Dimensions within an entry are ANDed (a selector naming both
	// a department and a group means both must hold); entries are ORed. Empty
	// when Unrestricted is true.
	Narrowed []Selector
}

// ScopeConstraintsFor derives the dimension constraints that already-loaded
// grants place on a scope. Handlers should use [Engine.ScopeConstraints] so
// request enforcement semantics apply.
func ScopeConstraintsFor(grants []Grant, check Check, keys []string) (ScopeConstraints, error) {
	none := ScopeConstraints{Granted: false, Unrestricted: false, Narrowed: nil}
	if err := validateInput(check); err != nil {
		return none, err
	}

	// The dimensionless check decides whether the scope is held at all,
	// including any exclusion grants that subtract from it.
	evaluation, err := evaluateGrantCheck(grants, check)
	if err != nil {
		return none, err
	}
	if evaluation.Grant == nil {
		return none, nil
	}

	constraints := ScopeConstraints{Granted: true, Unrestricted: false, Narrowed: nil}
	checks := check.expand()
	for i := range grants {
		grant := &grants[i]
		if !grantSatisfiesAny(grant, checks) {
			continue
		}

		narrowing := make(Selector, len(keys))
		for _, key := range keys {
			value, ok := grant.Selector[key]
			if !ok || value == WildcardResource {
				continue
			}
			narrowing[key] = value
		}
		if len(narrowing) == 0 {
			constraints.Unrestricted = true
			constraints.Narrowed = nil
			return constraints, nil
		}
		constraints.Narrowed = append(constraints.Narrowed, narrowing)
	}

	return constraints, nil
}

func grantSatisfiesAny(grant *Grant, checks []Check) bool {
	for i := range checks {
		check := &checks[i]
		if grant.Scope != check.Scope {
			continue
		}
		if check.matchesAllowSelector(grant.Selector) {
			return true
		}
	}
	return false
}

// ScopeConstraints reports whether the caller holds scope for the checked
// resource and, when they do, which selector dimensions narrow it. Unlike
// Require it records no authz challenge: an unsatisfied scope is not a denial
// to surface but a routine branch for handlers that scope their results
// instead of rejecting the request.
//
// Returns unrestricted when RBAC is not enforced for the request, matching
// Require's short-circuit.
func (e *Engine) ScopeConstraints(ctx context.Context, check Check, keys ...string) (ScopeConstraints, error) {
	unrestricted := ScopeConstraints{Granted: true, Unrestricted: true, Narrowed: nil}

	enforce, err := e.ShouldEnforce(ctx)
	if err != nil {
		return ScopeConstraints{Granted: false, Unrestricted: false, Narrowed: nil}, err
	}
	if !enforce {
		return unrestricted, nil
	}

	authorization, ok := grantAuthorizationFromContext(ctx)
	if !ok {
		return ScopeConstraints{Granted: false, Unrestricted: false, Narrowed: nil}, e.mapError(ctx, ErrMissingGrants)
	}

	constraints, err := authorization.constraints(check, keys)
	if err != nil {
		return ScopeConstraints{Granted: false, Unrestricted: false, Narrowed: nil}, e.mapError(ctx, err)
	}

	return constraints, nil
}

// constraints intersects the per-policy constraints of a request. A request
// bounded by several policies (a principal credential is bounded by the
// credential, the agent, and the owner) is only as wide as the narrowest one.
//
// Intersecting two populations that are each an OR of AND-ed dimensions is not
// expressible as another list of selectors, so more than one narrowing policy
// collapses to "granted, nothing visible" — under-approximating rather than
// widening. Single-policy requests, which is every dashboard session, keep
// their exact constraints.
func (a grantAuthorization) constraints(check Check, keys []string) (ScopeConstraints, error) {
	none := ScopeConstraints{Granted: false, Unrestricted: false, Narrowed: nil}
	if len(a.policies) == 0 {
		return none, nil
	}

	combined := ScopeConstraints{Granted: true, Unrestricted: true, Narrowed: nil}
	narrowingPolicies := 0
	for _, policy := range a.policies {
		constraints, err := ScopeConstraintsFor(policy, check, keys)
		if err != nil {
			return none, err
		}
		if !constraints.Granted {
			return none, nil
		}
		if constraints.Unrestricted {
			continue
		}

		narrowingPolicies++
		combined.Unrestricted = false
		if narrowingPolicies > 1 {
			combined.Narrowed = nil
			continue
		}
		combined.Narrowed = make([]Selector, 0, len(constraints.Narrowed))
		for _, narrowing := range constraints.Narrowed {
			combined.Narrowed = append(combined.Narrowed, maps.Clone(narrowing))
		}
	}

	return combined, nil
}
