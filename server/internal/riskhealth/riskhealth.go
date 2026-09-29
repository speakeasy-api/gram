// Package riskhealth carries one metric that answers a single question:
// is risk analysis actually running?
//
// Every risk engine in Gram fails open. A prompt-injection judge that cannot
// reach OpenRouter returns UNAVAILABLE and the message sails through; a
// customer policy whose evaluation errors is logged at warn and skipped. That
// is the right runtime behaviour — an analysis outage must not break the
// product — but it also means a total outage looks exactly like a quiet day
// on every metric that counts findings, blocks or scans. Prompt-injection
// scanning once sat dark on a drained OpenRouter balance for that reason.
//
// The per-engine metrics that already exist (risk.prompt_injection.*,
// risk.llm.*, risk.enforcement.*) each describe one engine in its own
// vocabulary, so no single Datadog query spans them. This package adds the
// cross-engine counter that does, with a bounded failure vocabulary an
// operator can act on: insufficient_credits needs a top-up, rate_limited
// needs a quota, upstream_unavailable needs patience.
//
// Monitors are documented in docs/runbooks/risk-analysis-availability-monitors.md.
package riskhealth

import (
	"context"
	"errors"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// MeterEvaluations is the wire name of the counter. Monitors and dashboards
// are written against it, so it is a contract: renaming it silently breaks
// the alerting this package exists to provide.
const MeterEvaluations = "risk.analysis.evaluations"

// Component is the part of risk analysis an evaluation ran in. It is the
// dimension an operator pivots on first: a drained balance takes out both
// OpenRouter judges while leaving the self-hosted analyzer healthy, and a
// customer's broken policy takes out neither.
type Component string

const (
	// ComponentPromptInjectionJudge is the OpenRouter-backed prompt-injection
	// judge, on both the realtime and batch paths.
	ComponentPromptInjectionJudge Component = "prompt_injection_judge"
	// ComponentPromptPolicyJudge is the OpenRouter-backed judge behind
	// customer-authored prompt policies.
	ComponentPromptPolicyJudge Component = "prompt_policy_judge"
	// ComponentLLMAnalyzer is the self-hosted fine-tuned risk model.
	ComponentLLMAnalyzer Component = "llm_analyzer"
	// ComponentPolicyEvaluation is one customer risk policy evaluated against
	// one event, whatever engines it happens to use.
	ComponentPolicyEvaluation Component = "policy_evaluation"
)

// Outcome is what an evaluation produced. Only OutcomeDegraded is alertable:
// it is the state where content passed through unanalyzed.
type Outcome string

const (
	// OutcomeCompleted means an engine reached a usable verdict. A clean
	// verdict and a finding are both completed.
	OutcomeCompleted Outcome = "completed"
	// OutcomeDegraded means no usable verdict was reached, so the event went
	// unanalyzed. The reason says why.
	OutcomeDegraded Outcome = "degraded"
	// OutcomeCanceled means the caller abandoned the evaluation. Recorded so
	// the counter accounts for every attempt, but excluded from the alerting
	// ratio: a deploy that drains in-flight requests is not an outage.
	OutcomeCanceled Outcome = "canceled"
)

// Reason is a bounded explanation of a degraded evaluation. Values are stable
// metric dimensions; add to them rather than renaming.
type Reason string

const (
	// ReasonNone accompanies a completed or canceled evaluation.
	ReasonNone Reason = "none"
	// ReasonInsufficientCredits is a drained model-provider balance. It never
	// self-heals, so any sustained volume is a page.
	ReasonInsufficientCredits Reason = "insufficient_credits"
	// ReasonRateLimited is the model provider throttling Gram.
	ReasonRateLimited Reason = "rate_limited"
	// ReasonThrottled is Gram's own per-organization judge limiter refusing
	// the call. Distinct from ReasonRateLimited: the fix is a Gram-side quota,
	// not a provider one.
	ReasonThrottled Reason = "throttled"
	// ReasonUnauthorized is a missing, revoked or unentitled provider key.
	ReasonUnauthorized Reason = "unauthorized"
	// ReasonKeyDisabled is a Gram-provisioned provider key that Gram locked
	// down itself.
	ReasonKeyDisabled Reason = "key_disabled"
	// ReasonUpstreamUnavailable is the model provider failing or overloaded.
	ReasonUpstreamUnavailable Reason = "upstream_unavailable"
	// ReasonTimeout is an evaluation that ran out of time.
	ReasonTimeout Reason = "timeout"
	// ReasonMalformedResponse is a model answer that could not be parsed into
	// a verdict. The call succeeded; the analysis still did not happen.
	ReasonMalformedResponse Reason = "malformed_response"
	// ReasonNotConfigured is an engine that is not wired up at all — no
	// classifier, no API key. This is the silent no-op an environment falls
	// into after a missed configuration step.
	ReasonNotConfigured Reason = "not_configured"
	// ReasonDependencyUnavailable is a policy that could not be evaluated
	// because an engine it depends on produced no verdict.
	ReasonDependencyUnavailable Reason = "dependency_unavailable"
	// ReasonPolicyError is a policy that errored during its own evaluation:
	// a bad scope expression, an unreachable store, a scanner fault. This is
	// the customer-facing half of the signal.
	ReasonPolicyError Reason = "policy_error"
	// ReasonBadRequest is a deterministic provider rejection of what Gram
	// sent. An engineering signal rather than an operational one, but the
	// analysis still did not happen.
	ReasonBadRequest Reason = "bad_request"
	// ReasonError is everything unclassified.
	ReasonError Reason = "error"
)

// ReasonFromError classifies a risk engine failure. Errors that came from
// OpenRouter keep their provider classification — that is what separates a
// drained balance from a transient outage — and everything else collapses to
// ReasonError.
func ReasonFromError(err error) Reason {
	if err == nil {
		return ReasonNone
	}
	switch openrouter.Classify(err) {
	case openrouter.ReasonNone:
		return ReasonNone
	case openrouter.ReasonInsufficientCredits:
		return ReasonInsufficientCredits
	case openrouter.ReasonRateLimited:
		return ReasonRateLimited
	case openrouter.ReasonUnauthorized:
		return ReasonUnauthorized
	case openrouter.ReasonKeyDisabled:
		return ReasonKeyDisabled
	case openrouter.ReasonUpstreamUnavailable:
		return ReasonUpstreamUnavailable
	case openrouter.ReasonBadRequest:
		return ReasonBadRequest
	case openrouter.ReasonTimeout:
		return ReasonTimeout
	case openrouter.ReasonCanceled:
		// Callers route cancellation to RecordCanceled; a caller that does not
		// still must not report an abandoned request as a provider fault.
		return ReasonNone
	case openrouter.ReasonContentPolicy:
		// The provider looked at the payload and refused it. That is a
		// verdict about the content, not an analysis outage.
		return ReasonNone
	default:
		return ReasonError
	}
}

// IsCanceled reports whether err is the caller abandoning the evaluation.
func IsCanceled(err error) bool {
	return errors.Is(err, context.Canceled)
}

// Metrics records risk analysis availability. Safe for concurrent use; one
// instance should live for the process lifetime so the instrument is created
// once. A nil *Metrics is inert, so callers can hold one unconditionally.
type Metrics struct {
	evaluations metric.Int64Counter
}

// NewMetrics creates the availability counter. An instrument that fails to
// build yields an inert recorder rather than a startup failure: losing a
// metric must not take down risk analysis itself.
func NewMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *Metrics {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/riskhealth")

	evaluations, err := meter.Int64Counter(
		MeterEvaluations,
		metric.WithDescription("Risk analysis evaluations by component and outcome, with a bounded reason for those that produced no verdict"),
		metric.WithUnit("{evaluation}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(MeterEvaluations), attr.SlogError(err))
	}

	return &Metrics{evaluations: evaluations}
}

// RecordCompleted counts an evaluation that reached a usable verdict.
func (m *Metrics) RecordCompleted(ctx context.Context, orgID string, component Component) {
	m.record(ctx, orgID, component, OutcomeCompleted, ReasonNone)
}

// RecordDegraded counts an evaluation that produced no verdict, so the event
// it was judging went unanalyzed.
func (m *Metrics) RecordDegraded(ctx context.Context, orgID string, component Component, reason Reason) {
	if reason == ReasonNone {
		reason = ReasonError
	}
	m.record(ctx, orgID, component, OutcomeDegraded, reason)
}

// RecordCanceled counts an evaluation the caller abandoned.
func (m *Metrics) RecordCanceled(ctx context.Context, orgID string, component Component) {
	m.record(ctx, orgID, component, OutcomeCanceled, ReasonNone)
}

// RecordResult counts one evaluation from the error it returned, routing
// cancellation away from the degraded bucket. Callers that already know their
// own bounded reason (a missing classifier, an unparseable verdict) call
// RecordDegraded directly instead.
func (m *Metrics) RecordResult(ctx context.Context, orgID string, component Component, err error) {
	switch {
	case err == nil:
		m.RecordCompleted(ctx, orgID, component)
	case IsCanceled(err):
		m.RecordCanceled(ctx, orgID, component)
	default:
		m.RecordDegraded(ctx, orgID, component, ReasonFromError(err))
	}
}

func (m *Metrics) record(ctx context.Context, orgID string, component Component, outcome Outcome, reason Reason) {
	if m == nil || m.evaluations == nil {
		return
	}
	m.evaluations.Add(ctx, 1, metric.WithAttributes([]attribute.KeyValue{
		attr.OrganizationID(orgID),
		attr.RiskComponent(component),
		attr.Outcome(outcome),
		attr.RiskDegradationReason(reason),
	}...))
}
