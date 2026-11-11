package authz

import (
	"context"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// DecisionMode says whether an authorization entry point evaluated grants for
// a request or skipped evaluation because the request is not enforced.
type DecisionMode string

const (
	// DecisionModeEnforced is a decision made against the caller's grants.
	DecisionModeEnforced DecisionMode = "enforced"
	// DecisionModeSkipped is a call to an entry point that returned without
	// evaluating grants because ShouldEnforce reported false, for example for a
	// legacy API key or a session-less internal caller.
	DecisionModeSkipped DecisionMode = "skipped"
)

// DecisionOperation names the entry point a decision went through. It is a
// bounded set, so it is safe to use as a log or metric attribute.
type DecisionOperation string

const (
	DecisionOperationRequire             DecisionOperation = "require"
	DecisionOperationRequireAny          DecisionOperation = "require_any"
	DecisionOperationRequireAnyUnblocked DecisionOperation = "require_any_unblocked"
	DecisionOperationEvaluate            DecisionOperation = "evaluate"
	DecisionOperationFilter              DecisionOperation = "filter"
	DecisionOperationFindMatched         DecisionOperation = "find_matched"
	DecisionOperationEvaluateLoaded      DecisionOperation = "evaluate_loaded_grants"
	DecisionOperationMandatory           DecisionOperation = "mandatory"
	DecisionOperationShouldEnforce       DecisionOperation = "should_enforce"
	// DecisionOperationRequireUserOrganization is RequireUserOrganizationScope
	// refusing a caller before it loads grants, or skipping where enforcement
	// is off. Its enforced evaluation is counted as evaluate_loaded_grants.
	DecisionOperationRequireUserOrganization DecisionOperation = "require_user_organization_scope"
)

// DecisionSummary counts the authorization decisions one request made. It
// carries no checks, grants or resource ids.
type DecisionSummary struct {
	// Enforced counts decisions made while enforcement is on, including
	// refusals decided before any grant is evaluated.
	Enforced int
	// Skipped counts entry point calls that returned without evaluating.
	Skipped int
	// Operations counts decisions per entry point, enforced and skipped
	// together.
	Operations map[DecisionOperation]int
	// Prepared is true once PrepareContext ran for the request, which every
	// authenticated transport does before calling a handler. A prepared
	// request that makes no decision reached a handler that never asked.
	Prepared bool
	// SkipReasons counts skipped decisions by why the request is not
	// enforced. Phase 6 of the RBAC plan replaces each reason with an explicit
	// policy, so this is the inventory it works from.
	SkipReasons map[SkipReason]int
}

// SkipReason says why ShouldEnforce reported false for a request. It is a
// bounded set.
type SkipReason string

const (
	// SkipReasonLegacyAPIKey is a legacy API key, authorized by its own scopes
	// outside RBAC.
	SkipReasonLegacyAPIKey SkipReason = "legacy_api_key"
	// SkipReasonNoSession is an authenticated caller with no session that is
	// not one of the session-less surfaces RBAC enforces.
	SkipReasonNoSession SkipReason = "no_session"
)

// Total is every decision the request made, enforced or skipped.
func (s DecisionSummary) Total() int { return s.Enforced + s.Skipped }

type decisionRecorderKey struct{}

type decisionRecorder struct {
	mu      sync.Mutex
	summary DecisionSummary
}

// WithDecisionRecorder returns a context that counts the authorization
// decisions made with it. HTTP middleware installs one per request and reads
// it back with RecordedDecisions when the response is written, so a request
// that reaches a handler and makes no decision at all can be reported.
func WithDecisionRecorder(ctx context.Context) context.Context {
	return context.WithValue(ctx, decisionRecorderKey{}, &decisionRecorder{
		mu:      sync.Mutex{},
		summary: DecisionSummary{Enforced: 0, Skipped: 0, Operations: nil, Prepared: false, SkipReasons: nil},
	})
}

// RecordedDecisions returns the decisions recorded on ctx so far. ok is false
// when no recorder was installed.
func RecordedDecisions(ctx context.Context) (summary DecisionSummary, ok bool) {
	recorder, ok := ctx.Value(decisionRecorderKey{}).(*decisionRecorder)
	if !ok {
		return DecisionSummary{Enforced: 0, Skipped: 0, Operations: nil, Prepared: false, SkipReasons: nil}, false
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	operations := make(map[DecisionOperation]int, len(recorder.summary.Operations))
	for operation, count := range recorder.summary.Operations {
		operations[operation] = count
	}
	reasons := make(map[SkipReason]int, len(recorder.summary.SkipReasons))
	for reason, count := range recorder.summary.SkipReasons {
		reasons[reason] = count
	}
	return DecisionSummary{
		Enforced:    recorder.summary.Enforced,
		Skipped:     recorder.summary.Skipped,
		Operations:  operations,
		Prepared:    recorder.summary.Prepared,
		SkipReasons: reasons,
	}, true
}

// recordDecision notes that an entry point was asked for a decision. It is a
// no-op outside a recorded request, so the engine behaves the same in
// background jobs and tests that install nothing.
func recordDecision(ctx context.Context, operation DecisionOperation, mode DecisionMode) {
	recorder, ok := ctx.Value(decisionRecorderKey{}).(*decisionRecorder)
	if !ok {
		return
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	switch mode {
	case DecisionModeEnforced:
		recorder.summary.Enforced++
	case DecisionModeSkipped:
		recorder.summary.Skipped++
		if recorder.summary.SkipReasons == nil {
			recorder.summary.SkipReasons = make(map[SkipReason]int)
		}
		recorder.summary.SkipReasons[skipReasonFor(ctx)]++
	}
	if recorder.summary.Operations == nil {
		recorder.summary.Operations = make(map[DecisionOperation]int)
	}
	recorder.summary.Operations[operation]++
}

// decisionModeFor maps ShouldEnforce's answer to a decision mode.
func decisionModeFor(enforce bool) DecisionMode {
	if enforce {
		return DecisionModeEnforced
	}
	return DecisionModeSkipped
}

// recordPrepared notes that the request's authorization context was prepared.
func recordPrepared(ctx context.Context) {
	recorder, ok := ctx.Value(decisionRecorderKey{}).(*decisionRecorder)
	if !ok {
		return
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.summary.Prepared = true
}

// skipReasonFor names why ShouldEnforce skipped ctx. It mirrors the order of
// checks in shouldEnforce, which is the only place that decides.
func skipReasonFor(ctx context.Context) SkipReason {
	if mode, ok := contextvalues.APIKeyAuthorization(ctx); ok && mode == contextvalues.APIKeyAuthorizationModeLegacy {
		return SkipReasonLegacyAPIKey
	}
	return SkipReasonNoSession
}
