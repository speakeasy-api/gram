package authz

import (
	"context"
	"maps"
	"slices"

	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
)

// Decision is the engine's answer to one check.
type Decision struct {
	// Allowed is true when the caller may do what the check asks.
	Allowed bool
	// Enforced is false when RBAC does not apply to this caller, such as a
	// legacy API key. Allowed is then true for every check except an
	// auth-client check.
	Enforced bool
	// Reason says why the check was refused. It is ReasonNone when allowed.
	Reason Reason
	// Check is the check this answers.
	Check Check

	// match is the grant that allowed the check. It is nil when the check was
	// refused or RBAC does not apply to the caller.
	match *grantMatch
	// alternative is, for an allowed AnyOf check, the alternative that matched.
	alternative int
	// blocked marks, for an AnyOf check, the alternatives a block refused.
	blocked []bool
	// blockedBy is, for an AnyOf check, the alternative a block refused before
	// any alternative was matched.
	blockedBy *Check
}

// Decisions holds one Decision per check, in the order the checks were
// passed to Decide.
type Decisions struct {
	// AllAllowed is true only when every check is allowed. It is false when
	// no check was passed: absence is never permission.
	AllAllowed bool
	Each       []Decision
}

// Decide answers each check without refusing anything: a check the caller
// may not do is a branch for the handler, not a denial. Use Require to gate
// an action.
//
// Example: a list page passes one check per server and shows the servers
// whose decision is allowed.
//
//	d, err := engine.Decide(ctx, checks...)
//	for i, s := range servers {
//		if d.Each[i].Allowed {
//			visible = append(visible, s)
//		}
//	}
//
// Decide accepts a probe, and an AnyOf check in any position. It records one
// challenge row for the call.
func (e *Engine) Decide(ctx context.Context, checks ...Check) (Decisions, error) {
	return e.decideAll(ctx, checks, true)
}

// decideAll is Decide, with the challenge row optional for the wrappers that
// record none today (Evaluate, Can, DenialReasons).
func (e *Engine) decideAll(ctx context.Context, checks []Check, record bool) (Decisions, error) {
	q, err := e.prepare(ctx, checks, false)
	if err != nil {
		return Decisions{AllAllowed: false, Each: nil}, e.mapError(ctx, err)
	}
	result := Decisions{AllAllowed: len(checks) > 0, Each: make([]Decision, 0, len(checks))}
	for _, check := range checks {
		if !isAnyOf(check) && q.aboutOtherOrganization(check) {
			e.logOtherOrganization(ctx, check)
		}
		d, err := q.decide(check)
		if err != nil {
			if record && q.enforced {
				focus := check
				e.recordBatch(ctx, q, result.Each, &focus)
			}
			return Decisions{AllAllowed: false, Each: nil}, e.mapError(ctx, err)
		}
		result.AllAllowed = result.AllAllowed && d.Allowed
		result.Each = append(result.Each, d)
	}
	if !q.enforced {
		return result, nil
	}
	e.reportAuthorizerRefusals(ctx, q.grants, allowedPlainChecks(result.Each), false)
	for _, d := range result.Each {
		if d.Allowed && d.Check.anyOf != nil {
			e.reportAuthorizerRefusals(ctx, q.grants, q.grants.allowedAlternatives(d.Check.anyOf), true)
		}
	}
	if record && len(checks) > 0 {
		e.recordBatch(ctx, q, result.Each, nil)
	}
	return result, nil
}

// question is one call to Require or Decide after the setup both share.
type question struct {
	// enforced is false when RBAC skips this caller.
	enforced bool
	// grants are the caller's grants, read from the context. Empty when not
	// enforced.
	grants grantAuthorization
	// aboutOtherOrganization reports an org check about an organization other
	// than the caller's, which nobody satisfies.
	aboutOtherOrganization func(Check) bool
}

// prepare runs the setup Require and Decide share, always in this order:
//  1. refuse shapes the API never accepts: a nested AnyOf, or for Require a
//     probe or an AnyOf next to other checks
//  2. ask whether RBAC applies to this caller
//  3. for a skipped caller, report invalid checks
//  4. read the grants from the context
//
// Each check is validated when it is decided.
func (e *Engine) prepare(ctx context.Context, checks []Check, forRequire bool) (question, error) {
	if err := validateShape(checks, forRequire); err != nil {
		return question{}, err
	}
	enforced, err := e.ShouldEnforce(ctx)
	if err != nil {
		return question{}, err
	}
	q := question{enforced: enforced, grants: grantAuthorization{}, aboutOtherOrganization: e.aboutOtherOrganization(ctx)}
	if !enforced {
		e.logInvalidUnenforced(ctx, checks)
		return q, nil
	}
	grants, ok := grantAuthorizationFromContext(ctx)
	if !ok {
		return question{}, ErrMissingGrants
	}
	q.grants = grants
	return q, nil
}

// decide answers one check. A check about another organization is refused,
// a skipped caller is allowed anything but auth-client use, and otherwise the
// caller's grants decide.
func (q question) decide(check Check) (Decision, error) {
	refused := Decision{Allowed: false, Enforced: q.enforced, Reason: ReasonNotGranted, Check: check, match: nil, alternative: 0, blocked: nil, blockedBy: nil}
	allowed := Decision{Allowed: true, Enforced: q.enforced, Reason: ReasonNone, Check: check, match: nil, alternative: 0, blocked: nil, blockedBy: nil}
	if check.anyOf != nil {
		return q.decideAnyOf(check)
	}
	if q.aboutOtherOrganization(check) {
		return refused, nil
	}
	if !q.enforced {
		if unenforcedAllows(check) {
			return allowed, nil
		}
		return refused, nil
	}
	if err := validateInput(check); err != nil {
		return refused, err
	}
	evaluation, err := q.grants.evaluate(check)
	if err != nil {
		return refused, err
	}
	switch {
	case evaluation.Grant != nil:
		allowed.match = &grantMatch{Grant: *evaluation.Grant, ViaCheck: *evaluation.Check}
		return allowed, nil
	case evaluation.Denied:
		refused.Reason = ReasonBlocked
		return refused, nil
	}
	refused.Reason, err = q.grants.dispositionReason(check)
	return refused, err
}

// decideAnyOf answers an AnyOf check: a block on any alternative refuses it,
// unless a direct grant on the concrete resource outranks the block, and
// otherwise one allowed alternative is enough. Alternatives about another
// organization are left out.
func (q question) decideAnyOf(check Check) (Decision, error) {
	refused := Decision{Allowed: false, Enforced: q.enforced, Reason: ReasonNotGranted, Check: check, match: nil, alternative: 0, blocked: nil, blockedBy: nil}
	alternatives := slices.DeleteFunc(slices.Clone(check.anyOf), q.aboutOtherOrganization)
	if len(alternatives) == 0 {
		if len(check.anyOf) == 0 {
			return refused, ErrNoChecks
		}
		return refused, nil
	}
	refused.Check.anyOf = alternatives
	if !q.enforced {
		if slices.ContainsFunc(alternatives, unenforcedAllows) {
			return Decision{Allowed: true, Enforced: false, Reason: ReasonNone, Check: refused.Check, match: nil, alternative: 0, blocked: nil, blockedBy: nil}, nil
		}
		return refused, nil
	}
	for _, alternative := range alternatives {
		if err := validateInput(alternative); err != nil {
			return refused, err
		}
	}
	blocked, err := firstUnoverriddenExclusion(q.grants, alternatives)
	if err != nil {
		return refused, err
	}
	if blocked != nil {
		refused.Reason = ReasonBlocked
		refused.blockedBy = blocked
		return refused, nil
	}
	result, err := q.grants.decideAnyOf(alternatives)
	if err != nil {
		return refused, err
	}
	if result.allowed() {
		return Decision{
			Allowed:     true,
			Enforced:    true,
			Reason:      ReasonNone,
			Check:       refused.Check,
			match:       &grantMatch{Grant: *result.match.Grant, ViaCheck: *result.match.Check},
			alternative: result.index,
			blocked:     result.blocked,
			blockedBy:   nil,
		}, nil
	}
	refused.blocked = result.blocked
	if slices.Contains(result.blocked, true) {
		refused.Reason = ReasonBlocked
	}
	return refused, nil
}

// dispositionReason says why a tool call no grant allows was refused. When
// the same call under another disposition would be allowed, the disposition
// is the reason. Example: a role limited to read_only tools is refused a
// destructive tool with ReasonReadOnly. It costs up to one grant match per
// disposition, and only for a refused check that carries one.
func (a grantAuthorization) dispositionReason(check Check) (Reason, error) {
	disposition, hasDisposition := check.Dimensions[SelectorKeyDisposition]
	if !hasDisposition {
		return ReasonNotGranted, nil
	}
	if disposition != DispositionReadOnly {
		allowed, err := a.allowsWithDisposition(check, DispositionReadOnly)
		if err != nil || allowed {
			return ReasonReadOnly, err
		}
	}
	for _, other := range slices.Sorted(maps.Keys(validDispositions)) {
		if other == disposition || other == DispositionReadOnly {
			continue
		}
		allowed, err := a.allowsWithDisposition(check, other)
		if err != nil || allowed {
			return ReasonDisposition, err
		}
	}
	return ReasonNotGranted, nil
}

func (a grantAuthorization) allowsWithDisposition(check Check, disposition string) (bool, error) {
	check.Dimensions = maps.Clone(check.Dimensions)
	check.Dimensions[SelectorKeyDisposition] = disposition
	evaluation, err := a.evaluate(check)
	return evaluation.Grant != nil, err
}

// recordBatch records one challenge row for a Decide call. focus names the
// check that failed to evaluate, or is nil for a decided batch.
func (e *Engine) recordBatch(ctx context.Context, q question, decided []Decision, focus *Check) {
	checks := make([]Check, 0, len(decided))
	matches := make([]grantMatch, 0, len(decided))
	allowedCount := 0
	anyBlocked := false
	for _, d := range decided {
		checks = append(checks, d.Check)
		if d.Allowed {
			allowedCount++
		}
		if d.match != nil {
			matches = append(matches, *d.match)
		}
		if d.Reason == ReasonBlocked {
			anyBlocked = true
		}
	}
	row := challengeLogger{
		Operation:            authzrepo.OperationFilter,
		Outcome:              authzrepo.OutcomeDeny,
		Reason:               authzrepo.ReasonScopeUnsatisfied,
		Checks:               checks,
		Focus:                focus,
		Matches:              matches,
		EvaluatedGrantCount:  uint32(q.grants.grantCount()), //nolint:gosec // grant count is small
		FilterCandidateCount: uint32(len(decided)),          //nolint:gosec // candidate count is small
		FilterAllowedCount:   uint32(allowedCount),          //nolint:gosec // allowed count is small
	}
	switch {
	case focus != nil:
		row.Checks = append(row.Checks, *focus)
		row.Outcome = authzrepo.OutcomeError
		row.Reason = authzrepo.ReasonInvalidCheck
		row.Matches = nil
		row.FilterAllowedCount = 0
	case allowedCount > 0:
		row.Outcome = authzrepo.OutcomeAllow
		row.Reason = authzrepo.ReasonGrantMatched
	case anyBlocked:
		row.Reason = authzrepo.ReasonDenyGrant
	case q.grants.grantCount() == 0:
		row.Reason = authzrepo.ReasonNoGrants
	}
	row.Log(ctx, e.db, e.logger, e.challengeLoggingEnabled)
}

// allowedPlainChecks returns the allowed checks that are not AnyOf checks.
func allowedPlainChecks(decided []Decision) []Check {
	var allowed []Check
	for _, d := range decided {
		if d.Allowed && d.Check.anyOf == nil {
			allowed = append(allowed, d.Check)
		}
	}
	return allowed
}
