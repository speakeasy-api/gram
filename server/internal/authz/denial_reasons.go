package authz

import (
	"context"
	"maps"
)

// DenialReason says why the caller is not authorized for a check.
type DenialReason string

const (
	// DenialReasonNone marks a check the caller is authorized for.
	DenialReasonNone DenialReason = ""

	// DenialReasonBlocked marks a check an exclusion (a *:blocked_* grant)
	// withdraws.
	DenialReasonBlocked DenialReason = "blocked"

	// DenialReasonDisposition marks a check no grant allows only because of
	// its disposition dimension: a grant would allow it if the check did not
	// name a disposition, so the caller's access is limited to other
	// dispositions (for example read_only tools).
	DenialReasonDisposition DenialReason = "disposition"

	// DenialReasonNotGranted marks a check no grant allows.
	DenialReasonNotGranted DenialReason = "not_granted"
)

// DenialReasons evaluates each check against the prepared grants and returns
// why each is refused, aligned with the input order. It records no authz
// challenge, because it only labels decisions that [Engine.FindMatched] or
// [Engine.Require] already make, and it reads only the grants already on the
// context.
//
// When RBAC is not enforced every check is authorized except auth-client
// checks, which are not granted.
func (e *Engine) DenialReasons(ctx context.Context, checks []Check) ([]DenialReason, error) {
	reasons := make([]DenialReason, len(checks))
	for _, c := range checks {
		if err := validateInput(c); err != nil {
			return nil, e.mapError(ctx, err)
		}
	}

	enforce, err := e.ShouldEnforce(ctx)
	if err != nil {
		return nil, err
	}
	if !enforce {
		for i, c := range checks {
			if !unenforcedAllows(c) {
				reasons[i] = DenialReasonNotGranted
			}
		}
		return reasons, nil
	}

	authorization, ok := grantAuthorizationFromContext(ctx)
	if !ok {
		return nil, e.mapError(ctx, ErrMissingGrants)
	}

	for i, c := range checks {
		evaluation, err := authorization.evaluate(c)
		if err != nil {
			return nil, e.mapError(ctx, err)
		}
		switch {
		case evaluation.Grant != nil:
			reasons[i] = DenialReasonNone
		case evaluation.Denied:
			reasons[i] = DenialReasonBlocked
		default:
			reasons[i] = DenialReasonNotGranted
		}
		if reasons[i] != DenialReasonNotGranted {
			continue
		}

		if _, hasDisposition := c.Dimensions[SelectorKeyDisposition]; !hasDisposition {
			continue
		}
		withoutDisposition := c
		withoutDisposition.Dimensions = maps.Clone(c.Dimensions)
		delete(withoutDisposition.Dimensions, SelectorKeyDisposition)
		evaluation, err = authorization.evaluate(withoutDisposition)
		if err != nil {
			return nil, e.mapError(ctx, err)
		}
		if evaluation.Grant != nil {
			reasons[i] = DenialReasonDisposition
		}
	}

	return reasons, nil
}
