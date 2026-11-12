package authz

import (
	"context"
	"slices"
)

// This file holds the request shapes the typed refs compose into, and the
// one engine entry point added for them. A handler gates with Require, asks
// a yes/no question with Can, and narrows a list with Filter:
//
//	e.Require(ctx, authz.MCPServer(id, project).Write())
//	e.Require(ctx, authz.AnyOf(authz.Project(p).Write(), authz.Skill(id).Write()))
//	ok, err := e.Can(ctx, authz.MCPServer(id, project).Connect().Probe())

// AnyOf requests any one of the alternatives, the way RequireAnyUnblocked
// does: a block matching any alternative refuses the request, unless a
// direct grant on the concrete resource outranks it, and otherwise one
// alternative must be allowed. Use it when a project-wide grant and a grant
// on the resource both authorize the operation, so a broad allow cannot
// bypass a block on the actual resource.
//
// Only Require and Can accept the result, and only as their single check.
func AnyOf(alternatives ...Check) Check {
	var first Check
	if len(alternatives) > 0 {
		first = alternatives[0]
	}
	return Check{
		Scope:           first.Scope,
		ResourceKind:    first.ResourceKind,
		ResourceID:      first.ResourceID,
		Dimensions:      nil,
		selectorMatch:   selectorMatchNormal,
		representedUser: "",
		probe:           false,
		anyOf:           append([]Check{}, alternatives...),
	}
}

// Probe marks the check as a discovery question: could the caller act on
// some resource matching it. A dimension the check leaves out matches any
// value a grant names. Can and Filter accept a probe; Require refuses one,
// because gating an action needs the concrete resource.
func (c Check) Probe() Check {
	c.probe = true
	return c
}

// Can reports whether the caller satisfies every check, without recording a
// challenge: a false result is a branch, not a denial. It is Evaluate under
// the name the typed API uses, and it also accepts a single AnyOf check.
func (e *Engine) Can(ctx context.Context, checks ...Check) (bool, error) {
	if alternatives, ok := singleAnyOf(checks); ok {
		return e.canAnyOf(ctx, alternatives)
	}
	return e.Evaluate(ctx, checks...)
}

func (e *Engine) canAnyOf(ctx context.Context, alternatives []Check) (bool, error) {
	if len(alternatives) == 0 {
		return false, e.mapError(ctx, ErrNoChecks)
	}
	if err := validateShape(alternatives, true); err != nil {
		return false, e.mapError(ctx, err)
	}
	enforce, err := e.ShouldEnforce(ctx)
	if err != nil {
		return false, err
	}
	if !enforce {
		return slices.ContainsFunc(alternatives, unenforcedAllows), nil
	}
	authorization, ok := grantAuthorizationFromContext(ctx)
	if !ok {
		return false, e.mapError(ctx, ErrMissingGrants)
	}
	for _, check := range alternatives {
		if err := validateInput(check); err != nil {
			return false, e.mapError(ctx, err)
		}
	}
	blocked, err := firstUnoverriddenExclusion(authorization, alternatives)
	if err != nil {
		return false, e.mapError(ctx, err)
	}
	if blocked != nil {
		return false, nil
	}
	for _, check := range alternatives {
		evaluation, err := authorization.evaluate(check)
		if err != nil {
			return false, e.mapError(ctx, err)
		}
		if !evaluation.Denied && evaluation.Grant != nil {
			return true, nil
		}
	}
	return false, nil
}

// singleAnyOf returns the alternatives when checks is one AnyOf check.
func singleAnyOf(checks []Check) ([]Check, bool) {
	if len(checks) != 1 || checks[0].anyOf == nil {
		return nil, false
	}
	return checks[0].anyOf, true
}

// validateShape refuses request shapes the API never accepts, before the
// unenforced short-circuit, so a misuse fails whether or not RBAC is enforced:
// an AnyOf nested where a single check is expected, or a probe where only a
// concrete check can be required.
func validateShape(checks []Check, allowProbe bool) error {
	for _, c := range checks {
		if c.anyOf != nil || (c.probe && !allowProbe) {
			return InvalidCheck(c.Scope, c.ResourceID)
		}
	}
	return nil
}

// validateRequireInput is validateInput for the Require family, which also
// refuses a probe.
func validateRequireInput(c Check) error {
	if c.probe {
		return InvalidCheck(c.Scope, c.ResourceID)
	}
	return validateInput(c)
}
