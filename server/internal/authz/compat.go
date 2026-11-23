package authz

import (
	"context"
	"slices"
)

// RequireAny refuses the request unless one of the checks is allowed. A block
// on one alternative only rules that alternative out.
//
// Deprecated: Use Require(ctx, Either(...)), which behaves the same.
func (e *Engine) RequireAny(ctx context.Context, checks ...Check) error {
	if len(checks) == 0 {
		return e.Require(ctx)
	}
	return e.Require(ctx, Either(checks...))
}

// Evaluate reports whether every check is allowed, without a challenge row.
//
// Deprecated: Use Decide and read AllAllowed.
func (e *Engine) Evaluate(ctx context.Context, checks ...Check) (bool, error) {
	if len(checks) == 0 {
		return false, e.mapError(ctx, ErrNoChecks)
	}
	if i := slices.IndexFunc(checks, isGroup); i >= 0 {
		return false, e.mapError(ctx, InvalidCheck(checks[i].Scope, checks[i].ResourceID))
	}
	d, err := e.Decide(WithoutChallenges(ctx), checks...)
	return d.AllAllowed, err
}

// Can is Evaluate that also accepts a single AnyUnblocked or Either.
//
// Deprecated: Use Decide and read AllAllowed.
func (e *Engine) Can(ctx context.Context, checks ...Check) (bool, error) {
	if _, ok := singleGroup(checks); !ok {
		return e.Evaluate(ctx, checks...)
	}
	d, err := e.Decide(WithoutChallenges(ctx), checks...)
	return d.AllAllowed, err
}

// Filter returns the resource IDs of the allowed checks.
//
// Deprecated: Use Decide and keep the inputs whose Each[i].Allowed is true.
func (e *Engine) Filter(ctx context.Context, checks []Check) ([]string, error) {
	if i := slices.IndexFunc(checks, isGroup); i >= 0 {
		return nil, e.mapError(ctx, InvalidCheck(checks[i].Scope, checks[i].ResourceID))
	}
	d, err := e.Decide(ctx, checks...)
	if err != nil {
		return nil, err
	}
	allowed := make([]string, 0, len(checks))
	for i, decision := range d.Each {
		if decision.Allowed {
			// #nosec G602 -- Decide returns one decision per check, in order.
			allowed = append(allowed, checks[i].ResourceID)
		}
	}
	return allowed, nil
}

// FindMatched reports whether each check is allowed.
//
// Deprecated: Use Decide and read Each[i].Allowed.
func (e *Engine) FindMatched(ctx context.Context, checks []Check) ([]bool, error) {
	if i := slices.IndexFunc(checks, isGroup); i >= 0 {
		return nil, e.mapError(ctx, InvalidCheck(checks[i].Scope, checks[i].ResourceID))
	}
	d, err := e.Decide(ctx, checks...)
	if err != nil {
		return nil, err
	}
	matched := make([]bool, len(checks))
	for i, decision := range d.Each {
		matched[i] = decision.Allowed
	}
	return matched, nil
}

// singleGroup returns the alternatives when checks is one group check.
func singleGroup(checks []Check) ([]Check, bool) {
	if len(checks) != 1 || checks[0].group == nil {
		return nil, false
	}
	return checks[0].group, true
}

// DenialReasons returns each check's Reason. A refused tool call also says
// whether another disposition would have passed, which costs up to four more
// evaluations per refused tool, so only an inspected tools listing asks.
//
// Deprecated: Use Decide and read Each[i].Reason. Until a tools listing moves
// to Decide, the disposition explanation is only available here.
func (e *Engine) DenialReasons(ctx context.Context, checks []Check) ([]Reason, error) {
	d, err := e.Decide(WithoutChallenges(ctx), checks...)
	if err != nil {
		return nil, err
	}
	grants, _ := grantAuthorizationFromContext(ctx)
	reasons := make([]Reason, len(checks))
	for i, decision := range d.Each {
		reasons[i] = decision.Reason
		// #nosec G602 -- Decide returns one decision per check, in order.
		check := checks[i]
		if decision.Enforced && decision.Reason == ReasonNotGranted && !isGroup(check) {
			reasons[i], err = grants.dispositionReason(check)
			if err != nil {
				return nil, e.mapError(ctx, err)
			}
		}
	}
	return reasons, nil
}

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
//
// Deprecated: Use Require(ctx, AnyUnblocked(...)), which behaves the same.
func (e *Engine) RequireAnyUnblocked(ctx context.Context, checks ...Check) error {
	return e.Require(ctx, AnyUnblocked(checks...))
}
