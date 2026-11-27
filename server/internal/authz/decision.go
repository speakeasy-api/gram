package authz

import (
	"cmp"
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
}

// verdict is a Decision with what the evaluator learned on the way, kept
// private so callers read only the outcome.
type verdict struct {
	Decision
	check       Check
	match       *grantMatch
	alternative int
	blocked     []bool
	// blockedBy is set when a block refused a group before any alternative
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
	// row is the authorization check Record writes, built while deciding. It
	// is nil when RBAC does not apply to the caller, who has nothing to show
	// in the access log.
	row *challengeLogger
}

// Decide answers each check without refusing or recording anything: a "no" is
// a branch for the handler, not a denial. Use Require to gate an action. Pass
// the answer to Record when the access log should show it. Example: a
// dashboard asks whether to show the delete button for a server, and records
// nothing; a list page decides one check per server and records the list.
func (e *Engine) Decide(ctx context.Context, checks ...Check) (Decisions, error) {
	q, err := e.prepare(ctx, checks, false)
	if err != nil {
		return Decisions{AllAllowed: false, Each: nil, row: nil}, e.mapError(ctx, err)
	}
	result := Decisions{AllAllowed: len(checks) > 0, Each: make([]Decision, 0, len(checks)), row: nil}
	verdicts := make([]verdict, 0, len(checks))
	e.logOtherOrganizations(ctx, q.aboutOtherOrganization, checks)
	for _, check := range checks {
		v, err := q.decide(check)
		if err != nil {
			if q.enforced {
				e.writeInvalidCheck(ctx, invalidRow(q, checks, verdicts, check))
			}
			return Decisions{AllAllowed: false, Each: nil, row: nil}, e.mapError(ctx, err)
		}
		result.AllAllowed = result.AllAllowed && v.Allowed
		result.Each = append(result.Each, v.Decision)
		verdicts = append(verdicts, v)
	}
	if !q.enforced {
		return result, nil
	}
	e.reportAuthorizerRefusals(ctx, q.grants, allowedPlainChecks(verdicts))
	for _, v := range verdicts {
		if v.Allowed && v.check.group != nil {
			refused, err := q.grants.authorizerRefusesGroup(v.check.group)
			reportAuthorizerRefusal(ctx, e.logger, q.grants, refused, err)
		}
	}
	switch len(verdicts) {
	case 0:
	case 1:
		var refused *verdict
		if !verdicts[0].Allowed {
			refused = &verdicts[0]
		}
		result.row = rowOf(gateRow(q, checks, verdicts, refused))
	default:
		result.row = rowOf(listRow(q, verdicts, nil))
	}
	if result.row != nil {
		RecordAuthorizationDecision(ctx, result.row.Operation, result.row.Outcome, result.row.Reason)
	}
	return result, nil
}

// Record writes what d decided to the access log. A decision about one check
// is recorded as that check, the way Require records it; a decision about
// several is recorded as the list they filtered, with how many were allowed.
// Recording d twice writes two rows.
func (e *Engine) Record(ctx context.Context, d Decisions) {
	if d.row == nil {
		return
	}
	d.row.write(ctx, e.db, e.logger, e.challengeLoggingEnabled)
}

// Require refuses the request unless the caller satisfies every check. A
// single group check is enough when one of its alternatives is allowed and no
// block refuses any of them. A probe is refused: gating
// an action needs the concrete resource. It records the decision, allowed or
// refused, so the access log shows why the action was allowed.
func (e *Engine) Require(ctx context.Context, checks ...Check) error {
	if len(checks) == 0 {
		return e.mapError(ctx, ErrNoChecks)
	}
	// A check about another organization is refused before anything else,
	// whether or not RBAC applies to the caller. A group check leaves out
	// such alternatives when it is decided, so another alternative can pass.
	if err := validateShape(checks, true); err != nil {
		return e.mapError(ctx, err)
	}
	aboutOther := e.aboutOtherOrganization(ctx)
	e.logOtherOrganizations(ctx, aboutOther, checks)
	if other := slices.IndexFunc(checks, func(c Check) bool { return !isGroup(c) && aboutOther(c) }); other >= 0 {
		return e.mapError(ctx, Denied(checks[other].Scope, checks[other].selector()))
	}
	q, err := e.prepare(ctx, checks, true)
	if err != nil {
		return e.mapError(ctx, err)
	}
	return e.requireAll(ctx, q, checks, true)
}

// requireAll decides, records and refuses. explain is false when the grants
// are not the caller's own, since their refusal says nothing about what the
// caller lacks.
func (e *Engine) requireAll(ctx context.Context, q question, checks []Check, explain bool) error {
	d, refused, err := e.decideGate(ctx, q, checks)
	if err != nil {
		return err
	}
	e.Record(ctx, d)
	if refused == nil {
		return nil
	}
	if !q.enforced {
		return e.mapError(ctx, Denied(ScopeMCPAuth, nil))
	}
	refusal := refusalOf(*refused)
	e.logMissingKeys(ctx, q.grants, refusal.missing...)
	if explain && refusal.explained != nil {
		recordDenial(ctx, refusal.explained)
	}
	return e.mapError(ctx, refusal.err)
}

// decideGate decides checks in order and stops at the first refusal, which it
// returns. A later check is not evaluated, so its being invalid does not hide
// the refusal.
func (e *Engine) decideGate(ctx context.Context, q question, checks []Check) (Decisions, *verdict, error) {
	result := Decisions{AllAllowed: true, Each: make([]Decision, 0, len(checks)), row: nil}
	verdicts := make([]verdict, 0, len(checks))
	for _, check := range checks {
		v, err := q.decide(check)
		if err != nil {
			if q.enforced {
				e.writeInvalidCheck(ctx, invalidGateRow(q, checks, verdicts, check))
			}
			return Decisions{AllAllowed: false, Each: nil, row: nil}, nil, e.mapError(ctx, err)
		}
		result.Each = append(result.Each, v.Decision)
		verdicts = append(verdicts, v)
		if !v.Allowed {
			result.AllAllowed = false
			if q.enforced {
				result.row = rowOf(gateRow(q, checks, verdicts, &v))
				RecordAuthorizationDecision(ctx, result.row.Operation, result.row.Outcome, result.row.Reason)
			}
			return result, &v, nil
		}
	}
	if !q.enforced {
		return result, nil, nil
	}
	if checks[0].group != nil {
		refused, err := q.grants.authorizerRefusesGroup(verdicts[0].check.group)
		reportAuthorizerRefusal(ctx, e.logger, q.grants, refused, err)
	} else {
		e.reportAuthorizerRefusals(ctx, q.grants, checks)
	}
	result.row = rowOf(gateRow(q, checks, verdicts, nil))
	RecordAuthorizationDecision(ctx, result.row.Operation, result.row.Outcome, result.row.Reason)
	return result, nil, nil
}

// writeInvalidCheck records a check the engine could not evaluate. It is
// written while deciding, not left to Record: it reports a broken call, not a
// decision a caller may choose to leave out.
func (e *Engine) writeInvalidCheck(ctx context.Context, row challengeLogger) {
	RecordAuthorizationDecision(ctx, row.Operation, row.Outcome, row.Reason)
	row.write(ctx, e.db, e.logger, e.challengeLoggingEnabled)
}

func rowOf(row challengeLogger) *challengeLogger { return &row }

// invalidRow is the row for a Decide call holding a check that could not be
// evaluated, in the shape its answer would have been recorded in.
func invalidRow(q question, checks []Check, decided []verdict, invalid Check) challengeLogger {
	if len(checks) == 1 {
		return invalidGateRow(q, checks, decided, invalid)
	}
	focus := invalidPart(invalid)
	return listRow(q, decided, &focus)
}

func invalidGateRow(q question, checks []Check, decided []verdict, invalid Check) challengeLogger {
	focus := invalidPart(invalid)
	row := gateRow(q, checks, decided, nil)
	row.Outcome, row.Reason, row.Focus, row.Matches = authzrepo.OutcomeError, authzrepo.ReasonInvalidCheck, &focus, nil
	return row
}

// gateRow is the row for checks that must all pass: it names the refused
// check, or on an allow the first check (for a group, the alternative that
// allowed it), with every grant that matched.
func gateRow(q question, checks []Check, decided []verdict, refused *verdict) challengeLogger {
	operation := authzrepo.OperationRequire
	// Copied, so a caller reusing its slice after Decide cannot change what
	// Record writes.
	recorded := slices.Clone(checks)
	if checks[0].group != nil {
		operation = authzrepo.OperationRequireAny
		recorded = slices.Clone(checks[0].group)
	}
	first := checks[0]
	row := challengeLogger{
		Operation:            operation,
		Outcome:              authzrepo.OutcomeAllow,
		Reason:               authzrepo.ReasonGrantMatched,
		Checks:               recorded,
		Focus:                &first,
		Matches:              make([]grantMatch, 0, len(decided)),
		EvaluatedGrantCount:  uint32(q.grants.grantCount()), //nolint:gosec // grant count is small
		FilterCandidateCount: 0,
		FilterAllowedCount:   0,
	}
	if refused != nil {
		row.Outcome, row.Reason, row.Focus, row.Matches = authzrepo.OutcomeDeny, challengeReason(q, *refused), refusalOf(*refused).focus, nil
		return row
	}
	for _, d := range decided {
		if d.match != nil {
			row.Matches = append(row.Matches, *d.match)
		}
	}
	if operation == authzrepo.OperationRequireAny && len(decided) > 0 && decided[0].Allowed {
		row.Focus = &decided[0].check.group[decided[0].alternative]
	}
	return row
}

type refusal struct {
	err error
	// explained is what the client is told it lacks; nil tells it nothing.
	explained error
	focus     *Check
	// missing are the checks whose missing selector keys are worth a log
	// line when the refusal is returned.
	missing []Check
}

func refusalOf(d verdict) refusal {
	check := d.check
	if check.group == nil {
		if d.Reason == ReasonBlocked {
			err := deniedByBlock(check.Scope, check.selector())
			return refusal{err: err, explained: err, focus: &check, missing: nil}
		}
		err := Denied(check.Scope, check.selector())
		return refusal{err: err, explained: err, focus: &check, missing: []Check{check}}
	}

	alternatives := check.group
	first := alternatives[0]
	if d.blockedBy != nil {
		blocked := d.blockedBy
		return refusal{err: Denied(blocked.Scope, blocked.selector()), explained: nil, focus: blocked, missing: nil}
	}
	if d.blocked == nil {
		return refusal{err: Denied(first.Scope, first.selector()), explained: nil, focus: &first, missing: nil}
	}
	var unblocked []Check
	for i, alternative := range alternatives {
		if !d.blocked[i] {
			unblocked = append(unblocked, alternative)
		}
	}
	// The alternatives are equivalent to the handler but not to the person
	// asking for access: explain the one that grants the least.
	var explained error
	if suggested, ok := leastPrivilegedAlternative(alternatives, d.blocked); ok {
		explained = Denied(suggested.Scope, suggested.selector())
	}
	return refusal{err: Denied(first.Scope, first.selector()), explained: explained, focus: &first, missing: unblocked}
}

func challengeReason(q question, d verdict) authzrepo.Reason {
	switch {
	case d.Reason == ReasonBlocked:
		return authzrepo.ReasonDenyGrant
	case q.grants.grantCount() == 0:
		return authzrepo.ReasonNoGrants
	default:
		return authzrepo.ReasonScopeUnsatisfied
	}
}

// resourceKindBreadth orders resource kinds by how much a grant on one covers:
// an organization holds projects, a project holds everything else.
func resourceKindBreadth(check Check) int {
	kind := check.ResourceKind
	if kind == "" {
		kind = ResourceKindForScope(check.Scope)
	}
	switch kind {
	case ResourceKindOrg:
		return 2
	case ResourceKindProject:
		return 1
	default:
		return 0
	}
}

// leastPrivilegedAlternative picks, among the alternatives of a group, the
// one a client should ask to be granted. Alternatives a block refused are left
// out, since granting them again changes nothing. Of the rest, a scope that
// satisfies fewer of the other alternatives is narrower, then a scope checked
// on a smaller kind of resource, then one that satisfies fewer scopes overall;
// the handler's own order breaks any remaining tie.
func leastPrivilegedAlternative(checks []Check, blocked []bool) (Check, bool) {
	type ranked struct {
		check   Check
		covers  int
		breadth int
		implies int
		index   int
	}
	candidates := make([]ranked, 0, len(checks))
	for i, check := range checks {
		if blocked[i] {
			continue
		}
		closure := ScopeImplicationClosure(check.Scope)
		covers := 0
		for j, other := range checks {
			if j != i && slices.Contains(closure, other.Scope) {
				covers++
			}
		}
		candidates = append(candidates, ranked{
			check:   check,
			covers:  covers,
			breadth: resourceKindBreadth(check),
			implies: len(closure),
			index:   i,
		})
	}
	if len(candidates) == 0 {
		var none Check
		return none, false
	}
	best := slices.MinFunc(candidates, func(a, b ranked) int {
		return cmp.Or(
			cmp.Compare(a.covers, b.covers),
			cmp.Compare(a.breadth, b.breadth),
			cmp.Compare(a.implies, b.implies),
			cmp.Compare(a.index, b.index),
		)
	})
	return best.check, true
}

// anyOfDecision is the outcome of evaluating a group's alternatives in order.
type anyOfDecision struct {
	// match is the evaluation of the first alternative a grant allows, nil when
	// none does.
	match *grantCheckEvaluation
	// index is the alternative that matched, or the one that failed to evaluate.
	index int
	// blocked marks the alternatives a deny grant refused.
	blocked []bool
}

func (d anyOfDecision) allowed() bool { return d.match != nil }

// decideAnyOf evaluates the alternatives in order and stops at the first one a
// grant allows. Alternatives a deny grant refuses are marked and skipped.
func (a grantAuthorization) decideAnyOf(checks []Check) (anyOfDecision, error) {
	decision := anyOfDecision{match: nil, index: 0, blocked: make([]bool, len(checks))}
	for i, check := range checks {
		decision.index = i
		evaluation, err := a.evaluate(check)
		if err != nil {
			return decision, err
		}
		if evaluation.Denied {
			decision.blocked[i] = true
			continue
		}
		if evaluation.Grant != nil {
			decision.match = &evaluation
			return decision, nil
		}
	}
	return decision, nil
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
		if err := e.validateUnenforced(ctx, checks); err != nil {
			return question{}, err
		}
		return q, nil
	}
	grants, ok := grantAuthorizationFromContext(ctx)
	if !ok {
		return question{}, ErrMissingGrants
	}
	q.grants = grants
	return q, nil
}

func (q question) decide(check Check) (verdict, error) {
	refused := q.verdict(check, false)
	allowed := q.verdict(check, true)
	if check.group != nil {
		return q.decideGroup(check)
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

func (q question) verdict(check Check, allowed bool) verdict {
	reason := ReasonNotGranted
	if allowed {
		reason = ReasonNone
	}
	return verdict{
		Decision:    Decision{Allowed: allowed, Enforced: q.enforced, Reason: reason},
		check:       check,
		match:       nil,
		alternative: 0,
		blocked:     nil,
		blockedBy:   nil,
	}
}

// decideGroup: a block on any alternative refuses the whole group unless a
// direct grant on the concrete resource outranks it. Example: a project-wide
// skill read and a read of skill S, with S blocked, is refused.
func (q question) decideGroup(check Check) (verdict, error) {
	refused := q.verdict(check, false)
	alternatives := slices.DeleteFunc(slices.Clone(check.group), q.aboutOtherOrganization)
	if len(alternatives) == 0 {
		if len(check.group) == 0 {
			return refused, ErrNoChecks
		}
		return refused, nil
	}
	refused.check.group = alternatives
	if !q.enforced {
		if slices.ContainsFunc(alternatives, unenforcedAllows) {
			return q.verdict(refused.check, true), nil
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
		allowed := q.verdict(refused.check, true)
		allowed.match = &grantMatch{Grant: *result.match.Grant, ViaCheck: *result.match.Check}
		allowed.alternative = result.index
		allowed.blocked = result.blocked
		return allowed, nil
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

// listRow is the row for checks decided as a list: every check, how many
// were candidates and how many were allowed. focus names a check that could
// not be evaluated.
func listRow(q question, decided []verdict, focus *Check) challengeLogger {
	checks := make([]Check, 0, len(decided))
	matches := make([]grantMatch, 0, len(decided))
	allowedCount := 0
	anyBlocked := false
	for _, d := range decided {
		checks = append(checks, recordedChecks(d)...)
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
	return row
}

func allowedPlainChecks(decided []verdict) []Check {
	var allowed []Check
	for _, d := range decided {
		if d.Allowed && d.check.group == nil {
			allowed = append(allowed, d.check)
		}
	}
	return allowed
}

// invalidPart names the alternative that failed validation when a group
// check is invalid, so the challenge row points at the check to fix.
func invalidPart(check Check) Check {
	for _, alternative := range check.group {
		if validateInput(alternative) != nil {
			return alternative
		}
	}
	return check
}

// recordedChecks is what a challenge row lists for one decision: for a group,
// the alternative that allowed it, or every alternative when none did, never
// the group itself, whose scope and id are only the first alternative's.
func recordedChecks(d verdict) []Check {
	switch {
	case d.check.group == nil:
		return []Check{d.check}
	case d.Allowed:
		return []Check{d.check.group[d.alternative]}
	default:
		return d.check.group
	}
}
