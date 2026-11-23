package authz

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// MembershipFetcher retrieves a WorkOS membership for a user+org pair.
type MembershipFetcher interface {
	GetOrgMembership(ctx context.Context, workOSUserID, workOSOrgID string) (*workos.Member, error)
}

type EngineOpts struct {
	DevMode bool
	// AdmitPrincipalCredential must be configured to accept principal-backed credentials.
	// Omission disables them rather than falling back to user authorization.
	AdmitPrincipalCredential PrincipalCredentialAdmitter
	// AdmitPrincipalCredentialWithDBTX must be configured for atomic credential refresh.
	AdmitPrincipalCredentialWithDBTX PrincipalCredentialDBTXAdmitter
	// AdmitWorkloadSession must be configured to accept workload sessions.
	// Omission disables them rather than falling back to user authorization,
	// which would hand a machine the user:all grant set.
	AdmitWorkloadSession WorkloadSessionAdmitter
	// AdmitWorkloadSessionInTx must be configured for atomic workload issuance.
	AdmitWorkloadSessionInTx WorkloadSessionTxAdmitter
	// LoadUserPolicy reads a member's live policy for consent to a connection
	// they attached to an agent. Nil reads it from the database.
	LoadUserPolicy UserPolicyLoader
}

// ChallengeLoggingEnabled checks whether authz challenge logging to ClickHouse
// is enabled for a given organization.
type ChallengeLoggingEnabled func(ctx context.Context, organizationID string) (bool, error)

type Engine struct {
	admitPrincipalCredential         PrincipalCredentialAdmitter
	admitPrincipalCredentialWithDBTX PrincipalCredentialDBTXAdmitter
	admitWorkloadSession             WorkloadSessionAdmitter
	admitWorkloadSessionInTx         WorkloadSessionTxAdmitter
	loadUserPolicy                   UserPolicyLoader
	logger                           *slog.Logger
	db                               *pgxpool.Pool
	challengeLoggingEnabled          ChallengeLoggingEnabled
	isDev                            bool
	membership                       MembershipFetcher
}

func NewEngine(
	logger *slog.Logger,
	db *pgxpool.Pool,
	challengeLogging ChallengeLoggingEnabled,
	membership MembershipFetcher,
	opts ...EngineOpts,
) *Engine {
	var devMode bool
	var admitPrincipalCredential PrincipalCredentialAdmitter
	var admitPrincipalCredentialWithDBTX PrincipalCredentialDBTXAdmitter
	var admitWorkloadSession WorkloadSessionAdmitter
	var admitWorkloadSessionInTx WorkloadSessionTxAdmitter
	var loadUserPolicy UserPolicyLoader
	if len(opts) > 0 {
		devMode = opts[0].DevMode
		admitPrincipalCredential = opts[0].AdmitPrincipalCredential
		admitPrincipalCredentialWithDBTX = opts[0].AdmitPrincipalCredentialWithDBTX
		admitWorkloadSession = opts[0].AdmitWorkloadSession
		admitWorkloadSessionInTx = opts[0].AdmitWorkloadSessionInTx
		loadUserPolicy = opts[0].LoadUserPolicy
	}

	authzLogger := logger.With(attr.SlogComponent("authz"))

	return &Engine{
		admitPrincipalCredential:         admitPrincipalCredential,
		admitPrincipalCredentialWithDBTX: admitPrincipalCredentialWithDBTX,
		admitWorkloadSession:             admitWorkloadSession,
		admitWorkloadSessionInTx:         admitWorkloadSessionInTx,
		loadUserPolicy:                   loadUserPolicy,
		logger:                           authzLogger,
		db:                               db,
		challengeLoggingEnabled:          challengeLogging,
		isDev:                            devMode,
		membership:                       membership,
	}
}

// GetScopeOverrides returns the parsed scope overrides from the request context
// if they are present AND the caller is authorised to use them. In local dev
// any authenticated user may use the override header; in production only
// platform admins can. Returns nil, false when overrides are absent or disallowed.
func (e *Engine) GetScopeOverrides(ctx context.Context) ([]RoleGrant, bool) {
	overrides, ok := readScopeOverrides(ctx)
	if !ok {
		return nil, false
	}
	if e.isDev {
		return overrides, true
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || !authCtx.IsAdmin {
		return nil, false
	}

	return overrides, true
}

func (e *Engine) PrepareContext(ctx context.Context) (context.Context, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return ctx, nil
	}

	if mode, hasMode := contextvalues.APIKeyAuthorization(ctx); hasMode {
		switch mode {
		case contextvalues.APIKeyAuthorizationModeLegacy:
			return ctx, nil
		case contextvalues.APIKeyAuthorizationModePrincipal:
			return e.AdmitPrincipalCredential(ctx)
		}
	}
	if authCtx.APIKeyID != "" {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	// A workload inherits its authority from the agent assigned to it, so it
	// admits through its own path. Checked before the credential branch below
	// because a workload session carries the same credential profile, and ahead
	// of user resolution because ResolveUserPrincipals seeds user:all before it
	// looks at the caller — which would hand a machine every grant written for
	// every member.
	if actor, hasActor := contextvalues.AuthenticatedActor(ctx); hasActor && actor.Type == urn.PrincipalTypeWorkload {
		prepared, err := e.AdmitWorkloadSession(ctx)
		if err == nil && ctx.Value(withoutCredentialCeilingKey{}) == true {
			prepared = WithoutCredentialCeiling(prepared)
		}
		return prepared, err
	}

	// Future principal-backed transports, including agent MCP sessions, attach
	// the same immutable credential profile and reuse this admission path.
	if _, hasCredential := contextvalues.PrincipalCredentialAuthorization(ctx); hasCredential {
		return e.AdmitPrincipalCredential(ctx)
	}

	if _, ok := GrantsFromContext(ctx); ok {
		return ctx, nil
	}

	// Assistant-token auth and Platform MCP have no session but should resolve
	// grants against the owning user stamped as UserID on the context.
	if authCtx.SessionID == nil && !enforcedWithoutSession(ctx) {
		return ctx, nil
	}
	if authCtx.ActiveOrganizationID == "" {
		return GrantsToContext(ctx, nil), nil
	}

	// Sessions in the shared demo org (which has no membership rows) get the
	// full user-visible grant set. This must precede scope and admin overrides
	// so the set a demo session holds is always the one access.ListGrants
	// reports.
	if authCtx.ActiveOrganizationID == constants.DemoOrganizationID {
		return GrantsToContext(ctx, DemoScopeGrants()), nil
	}

	if overrides, ok := e.GetScopeOverrides(ctx); ok {
		return GrantsToContext(ctx, GrantsFromOverrides(overrides)), nil
	}

	// Admins impersonating a customer org have no WorkOS membership in that
	// org, so the normal role-resolution path would yield zero grants and
	// every Require() call would 403. Grant all scopes — matching the
	// carve-out in access.ListGrants.
	if contextvalues.IsSupportSession(ctx) {
		return GrantsToContext(ctx, allScopeGrants()), nil
	}

	principals, err := ResolveUserPrincipals(ctx, e.db, authCtx.ActiveOrganizationID, authCtx.UserID)
	if err != nil {
		if errors.Is(err, ErrPrincipalInvalid) {
			return ctx, oops.E(oops.CodeUnauthorized, err, "invalid user principal")
		}
		if errors.Is(err, ErrPrincipalNotFound) {
			return GrantsToContext(ctx, nil), nil
		}
		e.logger.ErrorContext(
			ctx,
			"failed to resolve principals for authz grants",
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
			attr.SlogUserID(authCtx.UserID),
			attr.SlogError(err),
		)
		return ctx, fmt.Errorf("resolve principals: %w", err)
	}

	grants, err := LoadGrants(ctx, e.db, authCtx.ActiveOrganizationID, principals)
	if err != nil {
		e.logger.ErrorContext(
			ctx,
			"failed to load authz grants",
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
			attr.SlogUserID(authCtx.UserID),
			attr.SlogError(err),
		)
		return ctx, fmt.Errorf("load authz grants: %w", err)
	}

	return GrantsToContext(ctx, grants), nil
}

// Require refuses the request unless the caller satisfies every check. A
// single AnyOf check is enough when one of its alternatives is allowed and no
// block refuses any of them. A probe is refused: gating an action needs the
// concrete resource.
func (e *Engine) Require(ctx context.Context, checks ...Check) error {
	if len(checks) == 0 {
		return e.mapError(ctx, ErrNoChecks)
	}
	// A check about another organization is refused before anything else,
	// whether or not RBAC applies to the caller. An AnyOf check leaves out
	// such alternatives when it is decided, so another alternative can pass.
	if err := validateShape(checks, true); err != nil {
		return e.mapError(ctx, err)
	}
	aboutOther := e.aboutOtherOrganization(ctx)
	e.logOtherOrganizations(ctx, aboutOther, checks)
	if other := slices.IndexFunc(checks, func(c Check) bool { return !isAnyOf(c) && aboutOther(c) }); other >= 0 {
		return e.mapError(ctx, Denied(checks[other].Scope, checks[other].selector()))
	}
	q, err := e.prepare(ctx, checks, true)
	if err != nil {
		return e.mapError(ctx, err)
	}
	return e.requireAll(ctx, q, checks, true)
}

// EvaluateLoadedGrants evaluates explicit grants against checks without
// consulting ShouldEnforce or reading grants from context. Request handlers
// should use Require so normal request enforcement semantics apply.
func (e *Engine) EvaluateLoadedGrants(ctx context.Context, grants []Grant, checks ...Check) error {
	if _, ok := contextvalues.PrincipalCredentialAuthorization(ctx); ok {
		return oops.C(oops.CodeForbidden)
	}
	if mode, ok := contextvalues.APIKeyAuthorization(ctx); ok && mode == contextvalues.APIKeyAuthorizationModePrincipal {
		return oops.C(oops.CodeForbidden)
	}
	if len(checks) == 0 {
		return e.mapError(ctx, ErrNoChecks)
	}
	if err := validateShape(checks, true); err != nil {
		return e.mapError(ctx, err)
	}
	if slices.ContainsFunc(checks, isAnyOf) {
		return e.mapError(ctx, InvalidCheck(checks[0].Scope, checks[0].ResourceID))
	}
	q := question{enforced: true, grants: loadedGrantAuthorization(grants), aboutOtherOrganization: func(Check) bool { return false }}
	// Only the caller's own refusal is explained to the client: a probe of
	// some other principal's grants says nothing about what the caller lacks.
	return e.requireAll(ctx, q, checks, false)
}

// requireAll decides the checks in order and stops at the first refusal. It
// records one challenge row: the refusal, the check that could not be
// evaluated, or the allow with every matched grant. explain records the
// refusal for the client (see recordDenial).
func (e *Engine) requireAll(ctx context.Context, q question, checks []Check, explain bool) error {
	operation := authzrepo.OperationRequire
	if checks[0].anyOf != nil {
		operation = authzrepo.OperationRequireAny
	}
	row := challengeLogger{
		Operation:            operation,
		Outcome:              authzrepo.OutcomeAllow,
		Reason:               authzrepo.ReasonGrantMatched,
		Checks:               checks,
		Focus:                &checks[0],
		Matches:              make([]grantMatch, 0, len(checks)),
		EvaluatedGrantCount:  uint32(q.grants.grantCount()), //nolint:gosec // grant count is small
		FilterCandidateCount: 0,
		FilterAllowedCount:   0,
	}
	if operation == authzrepo.OperationRequireAny {
		row.Checks = checks[0].anyOf
	}
	var allowed []Decision
	for _, check := range checks {
		d, err := q.decide(check)
		if err != nil {
			if q.enforced {
				row.Outcome, row.Reason, row.Focus, row.Matches = authzrepo.OutcomeError, authzrepo.ReasonInvalidCheck, &check, nil
				row.Log(ctx, e.db, e.logger, e.challengeLoggingEnabled)
			}
			return e.mapError(ctx, err)
		}
		if !d.Allowed {
			if !q.enforced {
				return e.mapError(ctx, Denied(ScopeMCPAuth, nil))
			}
			refusal := e.refusal(ctx, q, d)
			row.Outcome, row.Reason, row.Focus, row.Matches = authzrepo.OutcomeDeny, challengeReason(q, d), refusal.focus, nil
			row.Log(ctx, e.db, e.logger, e.challengeLoggingEnabled)
			if explain && refusal.explained != nil {
				recordDenial(ctx, refusal.explained)
			}
			return e.mapError(ctx, refusal.err)
		}
		allowed = append(allowed, d)
		if d.match != nil {
			row.Matches = append(row.Matches, *d.match)
		}
	}
	if !q.enforced {
		return nil
	}
	if operation == authzrepo.OperationRequireAny {
		e.reportAuthorizerRefusals(ctx, q.grants, q.grants.allowedAlternatives(allowed[0].Check.anyOf), true)
		row.Focus = &allowed[0].Check.anyOf[allowed[0].alternative]
	} else {
		e.reportAuthorizerRefusals(ctx, q.grants, checks, false)
	}
	row.Log(ctx, e.db, e.logger, e.challengeLoggingEnabled)
	return nil
}

// refusal is how Require reports one refused decision.
type refusal struct {
	// err is returned to the handler.
	err error
	// explained is recorded for the client, or nil to record nothing.
	explained error
	// focus is the check the challenge row names.
	focus *Check
}

// refusal works out what Require returns, explains and records for a refused
// decision.
func (e *Engine) refusal(ctx context.Context, q question, d Decision) refusal {
	check := d.Check
	if check.anyOf == nil {
		if d.Reason == ReasonBlocked {
			err := deniedByBlock(check.Scope, check.selector())
			return refusal{err: err, explained: err, focus: &check}
		}
		e.logMissingKeys(ctx, q.grants, check)
		err := Denied(check.Scope, check.selector())
		return refusal{err: err, explained: err, focus: &check}
	}

	alternatives := check.anyOf
	first := alternatives[0]
	// A block refused the whole AnyOf before its alternatives were matched.
	if d.blockedBy != nil {
		blocked := d.blockedBy
		return refusal{err: Denied(blocked.Scope, blocked.selector()), explained: nil, focus: blocked}
	}
	// Every alternative was about another organization, so none was matched.
	if d.blocked == nil {
		return refusal{err: Denied(first.Scope, first.selector()), explained: nil, focus: &first}
	}
	var unblocked []Check
	for i, alternative := range alternatives {
		if !d.blocked[i] {
			unblocked = append(unblocked, alternative)
		}
	}
	e.logMissingKeys(ctx, q.grants, unblocked...)
	// The alternatives are equivalent to the handler but not to the person
	// asking for access: explain the one that grants the least.
	var explained error
	if suggested, ok := leastPrivilegedAlternative(alternatives, d.blocked); ok {
		explained = Denied(suggested.Scope, suggested.selector())
	}
	return refusal{err: Denied(first.Scope, first.selector()), explained: explained, focus: &first}
}

// challengeReason is the challenge row's reason for a refused decision.
func challengeReason(q question, d Decision) authzrepo.Reason {
	switch {
	case d.Reason == ReasonBlocked:
		return authzrepo.ReasonDenyGrant
	case q.grants.grantCount() == 0:
		return authzrepo.ReasonNoGrants
	default:
		return authzrepo.ReasonScopeUnsatisfied
	}
}

func (e *Engine) RequireAny(ctx context.Context, checks ...Check) error {
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

// leastPrivilegedAlternative picks, among the checks a RequireAny offered, the
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

// Evaluate reports whether the caller satisfies every check, without
// recording a challenge: a false result is a branch, not a denial. It is
// Decide's AllAllowed, kept until its callers move to Decide.
func (e *Engine) Evaluate(ctx context.Context, checks ...Check) (bool, error) {
	if len(checks) == 0 {
		return false, e.mapError(ctx, ErrNoChecks)
	}
	if i := slices.IndexFunc(checks, isAnyOf); i >= 0 {
		return false, e.mapError(ctx, InvalidCheck(checks[i].Scope, checks[i].ResourceID))
	}
	d, err := e.decideAll(ctx, checks, false)
	return d.AllAllowed, err
}

// Can is Evaluate that also accepts a single AnyOf check. It is kept until
// its callers move to Decide.
func (e *Engine) Can(ctx context.Context, checks ...Check) (bool, error) {
	if _, ok := singleAnyOf(checks); !ok {
		return e.Evaluate(ctx, checks...)
	}
	d, err := e.decideAll(ctx, checks, false)
	return d.AllAllowed, err
}

// anyOfDecision is the outcome of evaluating alternatives in order, shared by
// RequireAny and Can(AnyOf(...)) so the two cannot drift apart. Callers add
// their own challenge logging and result mapping.
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

// Filter returns the resource IDs of the checks the caller is allowed. It
// records one challenge row for the batch. It is kept until its callers move
// to Decide.
func (e *Engine) Filter(ctx context.Context, checks []Check) ([]string, error) {
	if i := slices.IndexFunc(checks, isAnyOf); i >= 0 {
		return nil, e.mapError(ctx, InvalidCheck(checks[i].Scope, checks[i].ResourceID))
	}
	d, err := e.decideAll(ctx, checks, true)
	if err != nil {
		return nil, err
	}
	allowed := make([]string, 0, len(checks))
	for _, decision := range d.Each {
		if decision.Allowed {
			allowed = append(allowed, decision.Check.ResourceID)
		}
	}
	return allowed, nil
}

// FindMatched reports, for each check in order, whether the caller is
// allowed. It records one challenge row for the batch. It is kept until its
// callers move to Decide.
func (e *Engine) FindMatched(ctx context.Context, checks []Check) ([]bool, error) {
	if i := slices.IndexFunc(checks, isAnyOf); i >= 0 {
		return nil, e.mapError(ctx, InvalidCheck(checks[i].Scope, checks[i].ResourceID))
	}
	d, err := e.decideAll(ctx, checks, true)
	if err != nil {
		return nil, err
	}
	matched := make([]bool, len(checks))
	for i, decision := range d.Each {
		matched[i] = decision.Allowed
	}
	return matched, nil
}

func (e *Engine) ShouldEnforce(ctx context.Context) (bool, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return false, oops.C(oops.CodeUnauthorized)
	}

	if _, principalCredential := contextvalues.PrincipalCredentialAuthorization(ctx); principalCredential {
		return true, nil
	}

	if mode, hasMode := contextvalues.APIKeyAuthorization(ctx); hasMode {
		if authCtx.APIKeyID == "" {
			return false, oops.C(oops.CodeUnauthorized)
		}
		switch mode {
		case contextvalues.APIKeyAuthorizationModeLegacy:
			return false, nil
		case contextvalues.APIKeyAuthorizationModePrincipal:
			return true, nil
		}
	}
	if authCtx.APIKeyID != "" {
		return false, oops.C(oops.CodeUnauthorized)
	}

	// Scope overrides are checked after the explicit legacy API-key exclusion so
	// the toolbar doesn't interfere with API key auth flows.
	if _, ok := e.GetScopeOverrides(ctx); ok {
		return true, nil
	}

	if authCtx.SessionID == nil && !enforcedWithoutSession(ctx) {
		return false, nil
	}

	return true, nil
}

// enforcedWithoutSession reports whether a session-less caller still has to be
// authorized against the acting user's grants.
//
// A missing session normally means an unauthenticated or internal path, which
// is why it disables enforcement. Three surfaces are session-less by design and
// act for a real user anyway: assistant-token auth, the OAuth-authenticated
// Platform MCP endpoint, and the Channels endpoint. Leaving any of them out
// would silently turn every scope check they make into a no-op — Evaluate
// returns true for every check when enforcement is off, so omitting a surface
// here grants it everything rather than nothing.
func enforcedWithoutSession(ctx context.Context) bool {
	if _, isAssistant := contextvalues.GetAssistantPrincipal(ctx); isAssistant {
		return true
	}
	surface, ok := contextvalues.GetActingSurface(ctx)
	return ok && (surface == contextvalues.ActingSurfacePlatformMCP || surface == contextvalues.ActingSurfaceChannels)
}

// unenforcedAllows reports whether check passes where RBAC is not enforced.
// Skipping evaluation never grants auth client use.
func unenforcedAllows(check Check) bool {
	return !check.isAuthClientCheck()
}

func validateInput(c Check) error {
	if c.anyOf != nil {
		return InvalidCheck(c.Scope, c.ResourceID)
	}
	switch c.ResourceID {
	case "":
		return InvalidCheck(c.Scope, c.ResourceID)
	case WildcardResource:
		return InvalidCheck(c.Scope, c.ResourceID)
	default:
		if err := validateCheckIdentity(c); err != nil {
			return err
		}
		if err := validateCheckOperation(c); err != nil {
			return err
		}
		if err := validateCheckDimensions(c); err != nil {
			return err
		}
		if err := validateRequiredDimensions(c); err != nil {
			return err
		}
		return validateAuthClientCheck(c)
	}
}

// logInvalidUnenforced reports checks that would be refused as invalid if the
// caller were enforced. Enforcement is skipped for this caller, so the request
// still goes through: this is the logging window before such checks are
// refused for every caller.
func (e *Engine) logInvalidUnenforced(ctx context.Context, checks []Check) {
	for _, check := range checks {
		if isAnyOf(check) {
			e.logInvalidUnenforced(ctx, check.anyOf)
			continue
		}
		err := validateInput(check)
		if err == nil {
			continue
		}
		e.logger.ErrorContext(
			ctx,
			"invalid authz check on a path that skips enforcement",
			attr.SlogResourceID(check.ResourceID),
			attr.SlogError(err),
		)
	}
}

// logMissingKeys logs an error for each refused check that a grant would have
// covered had the check carried a key the grant names. A check that leaves
// out a key never matches, so this is either a call site that should carry
// the key or a stored grant that names a key its permission does not have.
func (e *Engine) logMissingKeys(ctx context.Context, authorization grantAuthorization, refused ...Check) {
	for _, check := range refused {
		keys := authorization.keysMissingFrom(check)
		if len(keys) == 0 {
			continue
		}
		e.logger.ErrorContext(
			ctx,
			"authz check refused: a grant names a key the check does not carry",
			attr.SlogResourceID(check.ResourceID),
			attr.SlogAuthzMissingSelectorKeys(keys),
			attr.SlogError(Denied(check.Scope, check.selector())),
		)
	}
}

// aboutOtherOrganization reports whether a check is an org check about an
// organization other than the one the caller is acting in. Grants are loaded
// for the caller's organization, so such a check would ask one organization's
// grants about another organization. An id that cannot be compared is never
// read as the caller's organization. Nothing at the call site turns this on:
// the organization is the check's own resource.
func (e *Engine) aboutOtherOrganization(ctx context.Context) func(Check) bool {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		// No caller: the entry points refuse the request on their own.
		return func(Check) bool { return false }
	}
	caller := OrgIDFrom(authCtx.ActiveOrganizationID)
	return func(c Check) bool {
		if c.resourceKind() != ResourceKindOrg {
			return false
		}
		same, err := OrgIDFrom(c.ResourceID).Equals(caller)
		return err != nil || !same
	}
}

// withoutOtherOrganization splits checks into the ones the engine goes on to
// decide and the org checks about another organization, which no caller
// satisfies, whether or not enforcement is skipped for them. Each refused
// check is logged.
func (e *Engine) withoutOtherOrganization(ctx context.Context, checks []Check) (kept []Check, refused []Check) {
	other := e.aboutOtherOrganization(ctx)
	if !slices.ContainsFunc(checks, other) {
		return checks, nil
	}
	kept = make([]Check, 0, len(checks))
	for _, c := range checks {
		if !other(c) {
			kept = append(kept, c)
			continue
		}
		refused = append(refused, c)
		e.logOtherOrganization(ctx, c)
	}
	return kept, refused
}

// logOtherOrganizations logs each check, or AnyOf alternative, that is
// refused because it is about another organization than the caller's.
func (e *Engine) logOtherOrganizations(ctx context.Context, aboutOther func(Check) bool, checks []Check) {
	for _, c := range checks {
		if isAnyOf(c) {
			e.logOtherOrganizations(ctx, aboutOther, c.anyOf)
			continue
		}
		if aboutOther(c) {
			e.logOtherOrganization(ctx, c)
		}
	}
}

// logOtherOrganization logs a check refused because it is about another
// organization than the caller's.
func (e *Engine) logOtherOrganization(ctx context.Context, c Check) {
	e.logger.ErrorContext(
		ctx,
		"authz check refused: it is about another organization than the caller's",
		attr.SlogResourceID(c.ResourceID),
		attr.SlogError(Denied(c.Scope, c.selector())),
	)
}

// validateCheckIdentity rejects a check whose scope the registry does not
// know, whose resource kind is not its scope's kind, or whose dimensions use
// a key the scope does not declare. resource_kind and resource_id are never
// dimensions: they would replace the resource the check names.
func validateCheckIdentity(c Check) error {
	kind, known := scopeResourceKinds[c.Scope]
	if !known {
		return invalidCheckReason(c.Scope, c.ResourceID, "unknown scope")
	}
	if c.ResourceKind != "" && c.ResourceKind != kind {
		return invalidCheckReason(c.Scope, c.ResourceID, "resource kind "+c.ResourceKind+" is not the scope's kind")
	}
	for key := range c.Dimensions {
		if key == SelectorKeyResourceKind || key == SelectorKeyResourceID {
			return invalidCheckReason(c.Scope, c.ResourceID, "dimension "+key+" is reserved")
		}
		if !allowedSelectorKeys[kind][key] && !scopeAllowsSelectorKey(c.Scope, key) {
			return invalidCheckReason(c.Scope, c.ResourceID, "dimension "+key+" is not declared for the scope")
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

// validateShape refuses request shapes the API never accepts, before the
// unenforced short-circuit, so a misuse fails whether or not RBAC is
// enforced. Nobody may nest an AnyOf inside another. Require also refuses a
// probe, because gating an action needs the concrete resource, and an AnyOf
// next to other checks.
func validateShape(checks []Check, forRequire bool) error {
	for _, c := range checks {
		if forRequire && (c.probe || (c.anyOf != nil && len(checks) > 1)) {
			return InvalidCheck(c.Scope, c.ResourceID)
		}
		for _, alternative := range c.anyOf {
			if alternative.anyOf != nil || (forRequire && alternative.probe) {
				return InvalidCheck(alternative.Scope, alternative.ResourceID)
			}
		}
	}
	return nil
}

func isAnyOf(c Check) bool {
	return c.anyOf != nil
}

// singleAnyOf returns the alternatives when checks is one AnyOf check.
func singleAnyOf(checks []Check) ([]Check, bool) {
	if len(checks) != 1 || checks[0].anyOf == nil {
		return nil, false
	}
	return checks[0].anyOf, true
}

func (e *Engine) mapError(ctx context.Context, err error) error {
	if _, mapped := errors.AsType[*oops.ShareableError](err); mapped {
		return err
	}
	switch {
	case errors.Is(err, ErrDenied):
		return oops.C(oops.CodeForbidden)
	case errors.Is(err, ErrMissingGrants):
		return oops.E(oops.CodeUnexpected, err, "authz grants missing from prepared context").LogError(ctx, e.logger)
	case errors.Is(err, ErrInvalidCheck), errors.Is(err, ErrNoChecks):
		return oops.E(oops.CodeUnexpected, err, "invalid authz check").LogError(ctx, e.logger)
	default:
		return oops.E(oops.CodeUnexpected, err, "check authz").LogError(ctx, e.logger)
	}
}
