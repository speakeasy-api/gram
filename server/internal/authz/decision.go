package authz

import (
	"context"
	"maps"
	"slices"

	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
)

// Decision is the engine's answer to one check.
type Decision struct {
	Allowed bool
	// Enforced is false when RBAC does not apply to this caller, such as a
	// legacy API key. Allowed is then true for every check except an
	// auth-client check.
	Enforced bool
	Reason   Reason
	Check    Check

	match       *grantMatch
	alternative int
	blocked     []bool
	// blockedBy is set when a block refused an AnyOf before any alternative
	// was matched.
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

// Decide answers each check without refusing anything: a "no" is a branch for
// the handler, not a denial. Use Require to gate an action. Example: a list
// page passes one check per server and shows those whose decision is allowed.
func (e *Engine) Decide(ctx context.Context, checks ...Check) (Decisions, error) {
	q, err := e.prepare(ctx, checks, false)
	if err != nil {
		return Decisions{AllAllowed: false, Each: nil}, e.mapError(ctx, err)
	}
	result := Decisions{AllAllowed: len(checks) > 0, Each: make([]Decision, 0, len(checks))}
	e.logOtherOrganizations(ctx, q.aboutOtherOrganization, checks)
	for _, check := range checks {
		d, err := q.decide(check)
		if err != nil {
			if q.enforced {
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
	if len(checks) > 0 {
		e.recordBatch(ctx, q, result.Each, nil)
	}
	return result, nil
}

type question struct {
	enforced               bool
	grants                 grantAuthorization
	aboutOtherOrganization func(Check) bool
}

// prepare runs the setup in one fixed order for every entry point, so the
// same checks can never be refused in one place and allowed in another.
func (e *Engine) prepare(ctx context.Context, checks []Check, forRequire bool) (question, error) {
	if err := validateShape(checks, forRequire); err != nil {
		return question{}, err
	}
	enforced, err := e.ShouldEnforce(ctx)
	if err != nil {
		return question{}, err
	}
	var noGrants grantAuthorization
	q := question{enforced: enforced, grants: noGrants, aboutOtherOrganization: e.aboutOtherOrganization(ctx)}
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

// decideAnyOf: a block on any alternative refuses the whole AnyOf unless a
// direct grant on the concrete resource outranks it. Example: a project-wide
// skill read and a read of skill S, with S blocked, is refused.
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

// dispositionReason names the disposition as the reason when the same call
// under another disposition would pass. Example: a role limited to read_only
// tools is refused a destructive tool with ReasonReadOnly.
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
		FilterAllowedCount:   uint32(allowedCount),
	}
	switch {
	case focus != nil:
		row.Checks = append(row.Checks, *focus)
		row.Outcome = authzrepo.OutcomeError
		row.Reason = authzrepo.ReasonInvalidCheck
		row.Matches = nil
		row.FilterAllowedCount = 0
		row.FilterCandidateCount = uint32(len(row.Checks)) //nolint:gosec // candidate count is small
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

func allowedPlainChecks(decided []Decision) []Check {
	var allowed []Check
	for _, d := range decided {
		if d.Allowed && d.Check.anyOf == nil {
			allowed = append(allowed, d.Check)
		}
	}
	return allowed
}
