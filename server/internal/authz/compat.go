package authz

import (
	"context"
	"slices"

	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
)

// RequireAny refuses the request unless one of the checks is allowed. A block
// on one alternative only rules that alternative out.
//
// Deprecated: Use Require(ctx, AnyOf(...)), but classify each caller first:
// there a block on any alternative refuses the whole group. Example: with a
// project-wide skill read granted and skill S blocked, RequireAny on both
// allows reading S, Require(AnyOf(...)) refuses it.
func (e *Engine) RequireAny(ctx context.Context, checks ...Check) error {
	if i := slices.IndexFunc(checks, isAnyOf); i >= 0 {
		return e.mapError(ctx, InvalidCheck(checks[i].Scope, checks[i].ResourceID))
	}
	checks, refused := e.withoutOtherOrganization(ctx, checks)
	if len(checks) == 0 && len(refused) > 0 {
		return e.mapError(ctx, Denied(refused[0].Scope, refused[0].selector()))
	}
	enforce, err := e.ShouldEnforce(ctx)
	if err != nil {
		return err
	}
	if err := validateShape(checks, true); err != nil {
		return e.mapError(ctx, err)
	}
	if !enforce {
		e.logInvalidUnenforced(ctx, checks)
		if len(checks) > 0 && !slices.ContainsFunc(checks, unenforcedAllows) {
			return e.mapError(ctx, Denied(ScopeMCPAuth, nil))
		}
		return nil
	}
	if len(checks) == 0 {
		return e.mapError(ctx, ErrNoChecks)
	}

	authorization, ok := grantAuthorizationFromContext(ctx)
	if !ok {
		return e.mapError(ctx, ErrMissingGrants)
	}

	for _, check := range checks {
		if err := validateRequireInput(check); err != nil {
			challengeLogger{
				Operation:            authzrepo.OperationRequireAny,
				Outcome:              authzrepo.OutcomeError,
				Reason:               authzrepo.ReasonInvalidCheck,
				Checks:               checks,
				Focus:                &check,
				Matches:              nil,
				EvaluatedGrantCount:  uint32(authorization.grantCount()), //nolint:gosec // grant count is small
				FilterCandidateCount: 0,
				FilterAllowedCount:   0,
			}.Log(ctx, e.db, e.logger, e.challengeLoggingEnabled)
			return e.mapError(ctx, err)
		}
	}

	decision, err := authorization.decideAnyOf(checks)
	if err != nil {
		challengeLogger{
			Operation:            authzrepo.OperationRequireAny,
			Outcome:              authzrepo.OutcomeError,
			Reason:               authzrepo.ReasonInvalidCheck,
			Checks:               checks,
			Focus:                &checks[decision.index],
			Matches:              nil,
			EvaluatedGrantCount:  uint32(authorization.grantCount()), //nolint:gosec // grant count is small
			FilterCandidateCount: 0,
			FilterAllowedCount:   0,
		}.Log(ctx, e.db, e.logger, e.challengeLoggingEnabled)
		return e.mapError(ctx, err)
	}
	if decision.allowed() {
		e.reportAuthorizerRefusals(ctx, authorization, authorization.allowedAlternatives(checks), true)
		challengeLogger{
			Operation:            authzrepo.OperationRequireAny,
			Outcome:              authzrepo.OutcomeAllow,
			Reason:               authzrepo.ReasonGrantMatched,
			Checks:               checks,
			Focus:                &checks[decision.index],
			Matches:              []grantMatch{{Grant: *decision.match.Grant, ViaCheck: *decision.match.Check}},
			EvaluatedGrantCount:  uint32(authorization.grantCount()), //nolint:gosec // grant count is small
			FilterCandidateCount: 0,
			FilterAllowedCount:   0,
		}.Log(ctx, e.db, e.logger, e.challengeLoggingEnabled)
		return nil
	}

	reason := authzrepo.ReasonScopeUnsatisfied
	switch {
	case slices.Contains(decision.blocked, true):
		reason = authzrepo.ReasonDenyGrant
	case authorization.grantCount() == 0:
		reason = authzrepo.ReasonNoGrants
	}
	challengeLogger{
		Operation:            authzrepo.OperationRequireAny,
		Outcome:              authzrepo.OutcomeDeny,
		Reason:               reason,
		Checks:               checks,
		Focus:                &checks[0],
		Matches:              nil,
		EvaluatedGrantCount:  uint32(authorization.grantCount()), //nolint:gosec // grant count is small
		FilterCandidateCount: 0,
		FilterAllowedCount:   0,
	}.Log(ctx, e.db, e.logger, e.challengeLoggingEnabled)
	unblocked := make([]Check, 0, len(checks))
	for i, check := range checks {
		if !decision.blocked[i] {
			unblocked = append(unblocked, check)
		}
	}
	e.logMissingKeys(ctx, authorization, unblocked...)
	// The alternatives are equivalent to the handler but not to the person
	// asking for access: explain the one that grants the least.
	if suggested, ok := leastPrivilegedAlternative(checks, decision.blocked); ok {
		recordDenial(ctx, Denied(suggested.Scope, suggested.selector()))
	}
	return e.mapError(ctx, Denied(checks[0].Scope, checks[0].selector()))
}

// Evaluate reports whether every check is allowed, without a challenge row.
//
// Deprecated: Use Decide and read AllAllowed.
func (e *Engine) Evaluate(ctx context.Context, checks ...Check) (bool, error) {
	if len(checks) == 0 {
		return false, e.mapError(ctx, ErrNoChecks)
	}
	if i := slices.IndexFunc(checks, isAnyOf); i >= 0 {
		return false, e.mapError(ctx, InvalidCheck(checks[i].Scope, checks[i].ResourceID))
	}
	d, err := e.Decide(WithoutChallenges(ctx), checks...)
	return d.AllAllowed, err
}

// Can is Evaluate that also accepts a single AnyOf.
//
// Deprecated: Use Decide and read AllAllowed.
func (e *Engine) Can(ctx context.Context, checks ...Check) (bool, error) {
	if _, ok := singleAnyOf(checks); !ok {
		return e.Evaluate(ctx, checks...)
	}
	d, err := e.Decide(WithoutChallenges(ctx), checks...)
	return d.AllAllowed, err
}

// Filter returns the resource IDs of the allowed checks.
//
// Deprecated: Use Decide and keep the inputs whose Each[i].Allowed is true.
func (e *Engine) Filter(ctx context.Context, checks []Check) ([]string, error) {
	if i := slices.IndexFunc(checks, isAnyOf); i >= 0 {
		return nil, e.mapError(ctx, InvalidCheck(checks[i].Scope, checks[i].ResourceID))
	}
	d, err := e.Decide(ctx, checks...)
	if err != nil {
		return nil, err
	}
	allowed := make([]string, 0, len(checks))
	for i, decision := range d.Each {
		if decision.Allowed {
			allowed = append(allowed, checks[i].ResourceID)
		}
	}
	return allowed, nil
}

// FindMatched reports whether each check is allowed.
//
// Deprecated: Use Decide and read Each[i].Allowed.
func (e *Engine) FindMatched(ctx context.Context, checks []Check) ([]bool, error) {
	if i := slices.IndexFunc(checks, isAnyOf); i >= 0 {
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

// validateRequireInput is validateInput for the Require family, which also
// refuses a probe.
func validateRequireInput(c Check) error {
	if c.probe {
		return InvalidCheck(c.Scope, c.ResourceID)
	}
	return validateInput(c)
}

// singleAnyOf returns the alternatives when checks is one AnyOf check.
func singleAnyOf(checks []Check) ([]Check, bool) {
	if len(checks) != 1 || checks[0].anyOf == nil {
		return nil, false
	}
	return checks[0].anyOf, true
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
		if decision.Enforced && decision.Reason == ReasonNotGranted && !isAnyOf(checks[i]) {
			reasons[i], err = grants.dispositionReason(checks[i])
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
// Deprecated: Use Require(ctx, AnyOf(...)), which behaves the same.
func (e *Engine) RequireAnyUnblocked(ctx context.Context, checks ...Check) error {
	return e.Require(ctx, AnyOf(checks...))
}
