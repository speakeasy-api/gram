// Package riskhealth records whether risk analysis is actually running.
//
// Every risk engine fails open or degrades to a sentinel, so finding, block
// and scan counts look the same during an outage as on a quiet day. This
// package's one counter, risk.analysis.evaluations, records each evaluation of
// every engine and of every customer policy as completed, degraded or
// canceled, and gives degraded ones a reason from a closed set. Completed and
// degraded share one instrument so a degraded ratio has a consistent
// denominator, and the reasons stay bounded so Datadog monitors can match
// them exactly.
package riskhealth

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	typesafe "github.com/speakeasy-api/gram/server/internal/thirdparty/typesafedecisions"
)

// MeterEvaluations is the counter's name. Datadog monitors query it by name,
// so renaming it silently disables them.
const MeterEvaluations = "risk.analysis.evaluations"

const meterName = "github.com/speakeasy-api/gram/server/internal/scanners/riskhealth"

// Component is the engine or policy layer an evaluation belongs to.
type Component string

const (
	// ComponentPromptInjectionJudge is one message classified by the
	// prompt-injection judge, including its prefilter when it runs as a
	// cascade.
	ComponentPromptInjectionJudge Component = "prompt_injection_judge"

	// ComponentPromptPolicyJudge is one message judged against one
	// customer-authored prompt policy.
	ComponentPromptPolicyJudge Component = "prompt_policy_judge"

	// ComponentLLMAnalyzer is one message analyzed by the fine-tuned risk
	// model.
	ComponentLLMAnalyzer Component = "llm_analyzer"

	// ComponentPolicyEvaluation is one customer policy evaluated against one
	// event, across every engine the policy depends on.
	ComponentPolicyEvaluation Component = "policy_evaluation"
)

// Outcome is how an evaluation ended.
type Outcome string

const (
	// OutcomeCompleted means the evaluation reached a verdict.
	OutcomeCompleted Outcome = "completed"

	// OutcomeDegraded means the evaluation reached no usable verdict and the
	// caller fell back to its fail-open or fail-closed behavior.
	OutcomeDegraded Outcome = "degraded"

	// OutcomeCanceled means the caller abandoned the evaluation: the request
	// ended, a sibling policy already matched, or the process is draining.
	// It is kept apart from degraded so deploys do not page.
	OutcomeCanceled Outcome = "canceled"
)

// Reason is why an evaluation degraded.
type Reason string

const (
	// ReasonNone is the reason on completed and canceled evaluations.
	ReasonNone Reason = "none"

	// ReasonInsufficientCredits means the provider refused the call because
	// the key's credit balance or limit is exhausted. It never clears on its
	// own.
	ReasonInsufficientCredits Reason = "insufficient_credits"

	// ReasonKeyDisabled means the organization's provisioned platform key is
	// locked down, so no call was made.
	ReasonKeyDisabled Reason = "key_disabled"

	// ReasonUnauthorized means the provider rejected the credentials (401 or
	// a 403 that is not a content refusal).
	ReasonUnauthorized Reason = "unauthorized"

	// ReasonRateLimited means the provider throttled the call (429).
	ReasonRateLimited Reason = "rate_limited"

	// ReasonThrottled means Speakeasy's own per-organization limiter refused
	// the call before it reached the provider.
	ReasonThrottled Reason = "throttled"

	// ReasonUpstreamUnavailable means the provider or model host answered
	// with a server error or an overload status.
	ReasonUpstreamUnavailable Reason = "upstream_unavailable"

	// ReasonTimeout means the call ran past its deadline.
	ReasonTimeout Reason = "timeout"

	// ReasonContentPolicy means the provider's safety classifier refused the
	// payload.
	ReasonContentPolicy Reason = "content_policy"

	// ReasonBadRequest means the provider rejected the request as invalid,
	// such as an unknown model or an unsupported parameter.
	ReasonBadRequest Reason = "bad_request"

	// ReasonMalformedResponse means the model answered but its reply could not
	// be read as a verdict.
	ReasonMalformedResponse Reason = "malformed_response"

	// ReasonTruncatedResponse means the reply hit the completion token cap
	// before the verdict was complete.
	ReasonTruncatedResponse Reason = "truncated_response"

	// ReasonInputTooLarge means the evidence exceeded what the engine can
	// judge, so it declined rather than clear content it did not see.
	ReasonInputTooLarge Reason = "input_too_large"

	// ReasonNotConfigured means the deployment has no engine wired for this
	// evaluation, so nothing judged the content.
	ReasonNotConfigured Reason = "not_configured"

	// ReasonDependencyUnavailable means a customer policy reached no verdict
	// because an engine it depends on degraded.
	ReasonDependencyUnavailable Reason = "dependency_unavailable"

	// ReasonPolicyError means evaluating a customer policy failed outside its
	// engines, such as reading its exclusions or compiling its scopes.
	ReasonPolicyError Reason = "policy_error"

	// ReasonError is any failure the other reasons do not describe.
	ReasonError Reason = "error"
)

// Recorder records evaluations for one component. A nil *Recorder records
// nothing, so callers need no nil checks.
type Recorder struct {
	component   Component
	evaluations metric.Int64Counter
}

// New returns a Recorder for component. Every Recorder shares one instrument,
// so all components land on the same Datadog metric.
func New(meterProvider metric.MeterProvider, logger *slog.Logger, component Component) *Recorder {
	evaluations, err := meterProvider.Meter(meterName).Int64Counter(
		MeterEvaluations,
		metric.WithDescription("Risk analysis evaluations by component, outcome and degradation reason"),
		metric.WithUnit("{evaluation}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(MeterEvaluations), attr.SlogError(err))
	}
	return &Recorder{component: component, evaluations: evaluations}
}

// Completed records an evaluation that reached a verdict.
func (r *Recorder) Completed(ctx context.Context, orgID string, extra ...attribute.KeyValue) {
	r.record(ctx, orgID, OutcomeCompleted, ReasonNone, extra)
}

// Degraded records an evaluation that reached no usable verdict.
func (r *Recorder) Degraded(ctx context.Context, orgID string, reason Reason, extra ...attribute.KeyValue) {
	r.record(ctx, orgID, OutcomeDegraded, reason, extra)
}

// Canceled records an evaluation its caller abandoned.
func (r *Recorder) Canceled(ctx context.Context, orgID string, extra ...attribute.KeyValue) {
	r.record(ctx, orgID, OutcomeCanceled, ReasonNone, extra)
}

// Failed records an evaluation that ended with err: canceled when the caller
// canceled it, degraded with ReasonFromError otherwise. A nil err records a
// completed evaluation.
func (r *Recorder) Failed(ctx context.Context, orgID string, err error, extra ...attribute.KeyValue) {
	switch {
	case err == nil:
		r.Completed(ctx, orgID, extra...)
	case errors.Is(err, context.Canceled):
		r.Canceled(ctx, orgID, extra...)
	default:
		r.Degraded(ctx, orgID, ReasonFromError(err), extra...)
	}
}

func (r *Recorder) record(ctx context.Context, orgID string, outcome Outcome, reason Reason, extra []attribute.KeyValue) {
	if r == nil || r.evaluations == nil {
		return
	}
	attrs := make([]attribute.KeyValue, 0, 4+len(extra))
	attrs = append(attrs,
		attr.OrganizationID(orgID),
		attr.RiskAnalysisComponent(r.component),
		attr.Outcome(string(outcome)),
		attr.RiskAnalysisDegradationReason(reason),
	)
	attrs = append(attrs, extra...)
	r.evaluations.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// ReasonFromError reduces an engine error to a Reason. Deadline and timeout
// checks come first so a slow provider is not reported as a credential or
// billing failure. A canceled context classifies as ReasonError; callers that
// can be canceled use Recorder.Failed, which records those as canceled.
func ReasonFromError(err error) Reason {
	if err == nil {
		return ReasonNone
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ReasonTimeout
	}
	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		return ReasonTimeout
	}

	switch {
	case openrouter.IsInsufficientCredits(err):
		return ReasonInsufficientCredits
	case openrouter.IsPlatformKeyDisabled(err):
		return ReasonKeyDisabled
	case errors.Is(err, openrouter.ErrRateLimited):
		return ReasonRateLimited
	case openrouter.IsContentPolicy(err):
		return ReasonContentPolicy
	case openrouter.IsBadRequest(err), openrouter.IsHistoryCorruptionCandidate(err):
		return ReasonBadRequest
	case errors.Is(err, typesafe.ErrUnavailable):
		return ReasonNotConfigured
	case errors.Is(err, typesafe.ErrContextLengthExceeded):
		return ReasonInputTooLarge
	}

	if httpErr, ok := errors.AsType[*openrouter.HTTPError](err); ok {
		return ReasonForHTTPStatus(httpErr.StatusCode)
	}
	if statusErr, ok := errors.AsType[*typesafe.StatusError](err); ok {
		return ReasonForHTTPStatus(statusErr.StatusCode)
	}
	return ReasonError
}

// ReasonForHTTPStatus maps a provider's non-2xx status to a Reason, for
// clients whose error types this package cannot import.
func ReasonForHTTPStatus(status int) Reason {
	switch {
	case status == http.StatusPaymentRequired:
		return ReasonInsufficientCredits
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return ReasonUnauthorized
	case status == http.StatusRequestTimeout:
		return ReasonTimeout
	case status == http.StatusTooManyRequests:
		return ReasonRateLimited
	case status >= http.StatusInternalServerError:
		return ReasonUpstreamUnavailable
	case status >= http.StatusBadRequest:
		return ReasonBadRequest
	default:
		return ReasonError
	}
}
