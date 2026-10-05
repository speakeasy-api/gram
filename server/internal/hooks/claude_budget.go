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

	// hooksFailOpenLookupTimeout bounds the posture read after the budget
	// fires. The read is normally a cache hit; a slow one must not use up the
	// headroom the budget leaves for the client.
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
// answered as a pass-through, so its block side effects must be skipped.
func isVerdictSuperseded(ctx context.Context) bool {
	superseded, ok := ctx.Value(hookVerdictSupersededKey{}).(*atomic.Bool)
	return ok && superseded.Load()
}

// claudeHookVerdict is a handler's response to a legacy Claude hook event.
type claudeHookVerdict struct {
	result *gen.ClaudeHookResult
	err    error
}

// decideClaudeHookWithinBudget runs the event's handler and returns its
// verdict when it lands within claudeDecisionBudget of start. Otherwise it
// answers from the org's fail-open posture and reports answeredFromPosture.
//
// The handler runs on a detached context either way, so an overrun handler
// still finishes its scan and telemetry after the response is sent, then logs
// the verdict it would have returned. The event itself is persisted before
// dispatch (recordHook), so an overrun never drops it.
func (s *Service) decideClaudeHookWithinBudget(ctx context.Context, logger *slog.Logger, start time.Time, hookEvent any, hookEventName string) (*gen.ClaudeHookResult, bool, error) {
	ctx, superseded := withVerdictSupersededFlag(ctx)
	// answered goes to whichever side responds: the handler's verdict or the
	// budget fallback. Exactly one side wins it.
	answered := new(atomic.Bool)
	verdicts := make(chan claudeHookVerdict, 1)

	decisionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), legacyClaudeHookDetachedDecisionTimeout)
	s.claudeDecisionDrains.Go(func() {
		defer cancel()
		result, err := s.dispatchClaudeHookEvent(decisionCtx, logger, hookEvent, hookEventName)
		if answered.CompareAndSwap(false, true) {
			verdicts <- claudeHookVerdict{result: result, err: err}
			return
		}
		lateLogger := logger
		if err != nil {
			lateLogger = logger.With(attr.SlogError(err))
		}
		lateLogger.WarnContext(decisionCtx, "claude hook verdict arrived after its decision budget",
			attr.SlogEvent("claude_hook_late_verdict"),
			attr.SlogHookDecision(claudeHookDecision(result)),
			attr.SlogHookElapsed(time.Since(start)),
		)
	})

	budget := time.NewTimer(s.claudeDecisionBudget - time.Since(start))
	defer budget.Stop()
	select {
	case verdict := <-verdicts:
		return verdict.result, false, verdict.err
	case <-budget.C:
	}

	noun, organizationID, blockable := claudeBlockableEvent(hookEvent)
	failOpen := !blockable || s.hooksFailOpen(ctx, organizationID)
	if !answered.CompareAndSwap(false, true) {
		// The verdict landed while the posture was read, so it still answers.
		verdict := <-verdicts
		return verdict.result, false, verdict.err
	}
	if failOpen {
		superseded.Store(true)
	}

	res := claudeBudgetFallback(hookEventName, noun, failOpen)
	trace.SpanFromContext(ctx).SetAttributes(
		attr.Outcome(hookMetricOutcomeBudgetExceeded),
		attr.HookFailOpen(failOpen),
	)
	logger.WarnContext(ctx, "claude hook decision exceeded its budget; answered from the hooks fail-open setting",
		attr.SlogEvent("claude_hook_decision_budget_exceeded"),
		attr.SlogHookFailOpen(failOpen),
		attr.SlogHookDecision(claudeHookDecision(res)),
		attr.SlogHookElapsed(time.Since(start)),
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
// verify. The fail-closed answer blocks with a reason that says the check
// timed out, so the user sees why instead of a transport error.
func claudeBudgetFallback(hookEventName, noun string, failOpen bool) *gen.ClaudeHookResult {
	if failOpen {
		return makeHookResult(hookEventName)
	}
	return constructBlockResponse(hookEventName, fmt.Sprintf(
		"Speakeasy blocked this %s: its security check did not finish in time. Retry in a moment, and contact your administrator if this keeps happening.",
		noun,
	))
}

// hooksFailOpen reads the organization's hooks fail-open setting, the posture
// the hooks binary mirrors from the ingest response's org settings. The read
// is detached from the request and bounded by hooksFailOpenLookupTimeout. An
// unknown organization, a missing feature client or a failed read resolves to
// fail-open, the default for new organizations, which matches how the session
// quarantine gate treats an unreadable setting.
func (s *Service) hooksFailOpen(ctx context.Context, organizationID string) bool {
	if organizationID == "" || s.productFeatures == nil {
		return true
	}
	lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), hooksFailOpenLookupTimeout)
	defer cancel()
	failOpen, err := s.productFeatures.IsFeatureEnabled(lookupCtx, organizationID, productfeatures.FeatureHooksFailOpen)
	if err != nil {
		s.logger.WarnContext(ctx, "read hooks fail-open setting; failing open",
			attr.SlogEvent("claude_hook_fail_open_lookup_failed"),
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
		)
		return true
	}
	return failOpen
}
