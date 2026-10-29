package authz

import "context"

// CheckResult is the engine's decision on one check.
type CheckResult struct {
	Allowed bool
	// Blocked reports that a matching block withdrew the check.
	Blocked bool
	// Grant is the grant that allowed the check. It is nil when the check was
	// denied or when RBAC is not enforced for the caller.
	Grant *Grant
}

// EvaluateEach decides every check on its own against the prepared grants,
// without recording a challenge. It reaches the same decisions as Evaluate and
// Require, and reports which grant allowed each check.
func (e *Engine) EvaluateEach(ctx context.Context, checks ...Check) ([]CheckResult, error) {
	if len(checks) == 0 {
		return nil, e.mapError(ctx, ErrNoChecks)
	}
	enforce, err := e.ShouldEnforce(ctx)
	if err != nil {
		return nil, err
	}
	results := make([]CheckResult, len(checks))
	if !enforce {
		for i, check := range checks {
			results[i] = CheckResult{Allowed: unenforcedAllows(check), Blocked: false, Grant: nil}
		}
		return results, nil
	}
	authorization, ok := grantAuthorizationFromContext(ctx)
	if !ok {
		return nil, e.mapError(ctx, ErrMissingGrants)
	}
	for i, check := range checks {
		if err := validateInput(check); err != nil {
			return nil, e.mapError(ctx, err)
		}
		evaluation, err := authorization.evaluate(check)
		if err != nil {
			return nil, e.mapError(ctx, err)
		}
		results[i] = CheckResult{Allowed: evaluation.Grant != nil, Blocked: evaluation.Denied, Grant: evaluation.Grant}
	}
	return results, nil
}

// MapError converts an authz error into the error a handler returns.
func (e *Engine) MapError(ctx context.Context, err error) error {
	return e.mapError(ctx, err)
}
