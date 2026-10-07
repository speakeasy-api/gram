package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

const (
	// legacyClaudeHookDecisionBudget bounds how long /rpc/hooks.claude spends
	// on an event, measured from when the request arrived, before it answers
	// from the org's hooks fail-open posture instead of the handler's verdict.
	// The curl hook clients that still call this route give up after 10s per
	// attempt and turn the resulting HTTP 000 into a block. 5s leaves the other
	// half of that window for client start-up, proxies and the network.
	legacyClaudeHookDecisionBudget = 5 * time.Second

	// legacyClaudeHookDetachedDecisionTimeout bounds a handler that keeps
	// running after its budget, so its risk scan, block telemetry and
	// late-verdict log still land. Enforcement scans have taken about 20s under
	// load; 30s covers that without letting abandoned work pile up.
	legacyClaudeHookDetachedDecisionTimeout = 30 * time.Second

	// hooksFailOpenLookupTimeout bounds the posture read that runs alongside
	// the handler. The read is normally a cache hit; a slow one fails closed
	// instead of delaying the fallback.
	hooksFailOpenLookupTimeout = time.Second
)

// hookVerdictSupersededKey carries a flag the legacy Claude endpoint sets when
// it answered an event as a pass-through before the handler reached its
// verdict. The handler keeps running on a detached context, and its block side
// effects check the flag so a late deny is not recorded as a block the user
// never received.
type hookVerdictSupersededKey struct{}

func withVerdictSupersededFlag(ctx context.Context) (context.Context, *atomic.Bool) {
	superseded := new(atomic.Bool)
	return context.WithValue(ctx, hookVerdictSupersededKey{}, superseded), superseded
}

// isVerdictSuperseded reports whether the event behind ctx was already
// answered as a pass-through.
func isVerdictSuperseded(ctx context.Context) bool {
	superseded, ok := ctx.Value(hookVerdictSupersededKey{}).(*atomic.Bool)
	return ok && superseded.Load()
}

// claudeHookVerdict is a handler's response to a legacy Claude hook event.
type claudeHookVerdict struct {
	result *gen.ClaudeHookResult
	err    error

	// riskScanned reports whether the handler ran an enforcement scan.
	riskScanned bool
}

// answer returns the verdict as the response and marks the request's risk-scan
// tracker when the handler scanned.
func (v claudeHookVerdict) answer(ctx context.Context) (*gen.ClaudeHookResult, bool, error) {
	if v.riskScanned {
		markRiskScanned(ctx)
	}
	return v.result, false, v.err
}

// decideClaudeHookWithinBudget runs the event's handler and returns its
// verdict when it lands within claudeBudget of start. Otherwise it answers
// from the org's fail-open posture and reports answeredFromPosture.
//
// The handler runs on a detached context either way. The event itself is
// persisted before dispatch (recordHook), so an overrun never drops it. The
// posture is read alongside the handler, so the fallback can claim the answer,
// and mark a pass-through superseded, as soon as the budget fires.
func (s *Service) decideClaudeHookWithinBudget(ctx context.Context, logger *slog.Logger, start time.Time, hookEvent any, hookEventName string) (*gen.ClaudeHookResult, bool, error) {
	ctx, superseded := withVerdictSupersededFlag(ctx)
	// answered goes to whichever side responds: the handler's verdict or the
	// budget fallback. Exactly one side wins it.
	answered := new(atomic.Bool)
	verdicts := make(chan claudeHookVerdict, 1)

	noun, organizationID, blockable := claudeBlockableEvent(hookEvent)
	postures := make(chan bool, 1)
	if blockable {
		s.claudeDrains.Go(func() { postures <- s.hooksFailOpen(ctx, organizationID) })
	}
	if !blockable {
		// An event that cannot block always passes through.
		postures <- true
	}

	decisionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), legacyClaudeHookDetachedDecisionTimeout)
	// The handler gets its own risk-scan tracker because it can still be
	// scanning after an overrun response has recorded its metric. answer copies
	// the flag to the request's tracker when the verdict lands in time.
	decisionCtx, riskScanned := withRiskScanTracker(decisionCtx)
	s.claudeDrains.Go(func() {
		defer cancel()
		result, err := s.dispatchClaudeHookEvent(decisionCtx, logger, hookEvent, hookEventName)
		if answered.CompareAndSwap(false, true) {
			verdicts <- claudeHookVerdict{result: result, err: err, riskScanned: *riskScanned}
			return
		}
		lateLogger := logger
		if err != nil {
			lateLogger = logger.With(attr.SlogError(err))
		}
		lateLogger.WarnContext(decisionCtx, "claude hook verdict arrived after its decision budget",
			attr.SlogEvent("claude_hook_late_verdict"),
			attr.SlogHookDecision(claudeHookDecision(result)),
		)
	})

	select {
	case verdict := <-verdicts:
		return verdict.answer(ctx)
	case <-time.After(s.claudeBudget - time.Since(start)):
	}

	failOpen := <-postures
	if !answered.CompareAndSwap(false, true) {
		// The verdict landed while the fallback waited on the posture, so it still answers.
		return (<-verdicts).answer(ctx)
	}
	superseded.Store(failOpen)

	res := claudeBudgetFallback(hookEventName, noun, failOpen)
	decision := claudeHookDecision(res)
	trace.SpanFromContext(ctx).SetAttributes(
		attr.Outcome(hookMetricOutcomeBudgetExceeded),
		attr.HookDecision(decision),
	)
	logger.WarnContext(ctx, "claude hook decision exceeded its budget; answered from the hooks fail-open setting",
		attr.SlogEvent("claude_hook_decision_budget_exceeded"),
		attr.SlogHookDecision(decision),
	)
	return res, true, nil
}

// claudeBlockableEvent reports whether the handler for a Claude event can deny
// it, the noun its block reason uses, and the organization whose fail-open
// posture applies. Every other event only observes and never blocks.
func claudeBlockableEvent(hookEvent any) (noun string, organizationID string, blockable bool) {
	switch ev := hookEvent.(type) {
	case *hookevents.BeforeToolUse:
		return "tool call", ev.Context.OrganizationID, true
	case *hookevents.UserPromptSubmit:
		return "prompt", ev.Context.OrganizationID, true
	default:
		return "", "", false
	}
}

// claudeBudgetFallback is the response for an event whose verdict missed the
// decision budget. The fail-open answer is the pass-through shape the legacy
// client accepts as success. It carries no permissionDecision, so Claude
// Code's own permission rules still apply to a tool call the server did not
// verify.
func claudeBudgetFallback(hookEventName, noun string, failOpen bool) *gen.ClaudeHookResult {
	if failOpen {
		return makeHookResult(hookEventName)
	}
	return constructBlockResponse(hookEventName, fmt.Sprintf(
		"Speakeasy blocked this %s: its security check did not finish in time. Retry in a moment, and contact your administrator if this keeps happening.",
		noun,
	))
}

// hooksFailOpen reports whether the organization's hooks fail-open setting is
// known to be on. An unknown organization, a missing feature client or a failed
// read fails closed: the organization may have chosen to block unverified
// events, and the shadow-MCP guard can deny a tool call even when the request
// carries no organization.
func (s *Service) hooksFailOpen(ctx context.Context, organizationID string) bool {
	if organizationID == "" || s.productFeatures == nil {
		return false
	}
	lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), hooksFailOpenLookupTimeout)
	defer cancel()
	failOpen, err := s.productFeatures.IsFeatureEnabled(lookupCtx, organizationID, productfeatures.FeatureHooksFailOpen)
	if err != nil {
		s.logger.WarnContext(ctx, "read hooks fail-open setting; failing closed",
			attr.SlogEvent("claude_hook_fail_open_lookup_failed"),
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
		)
		return false
	}
	return failOpen
}
