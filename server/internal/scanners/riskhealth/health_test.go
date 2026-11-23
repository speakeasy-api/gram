package riskhealth_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/scanners/riskhealth"
	"github.com/speakeasy-api/gram/server/internal/scanners/riskhealth/riskhealthtest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	typesafe "github.com/speakeasy-api/gram/server/internal/thirdparty/typesafedecisions"
)

// TestWireContract pins the names Datadog monitors query. They are repeated
// as literals so a rename fails here instead of silently disabling alerts.
func TestWireContract(t *testing.T) {
	t.Parallel()

	require.Equal(t, "risk.analysis.evaluations", riskhealth.MeterEvaluations)
	require.Equal(t, "gram.risk.analysis.component", string(attr.RiskAnalysisComponentKey))
	require.Equal(t, "gram.risk.analysis.degradation_reason", string(attr.RiskAnalysisDegradationReasonKey))
	require.Equal(t, "gram.outcome", string(attr.OutcomeKey))
	require.Equal(t, "gram.org.id", string(attr.OrganizationIDKey))
	require.Equal(t, "gram.risk.policy_id", string(attr.RiskPolicyIDKey))

	require.Equal(t, []riskhealth.Component{"prompt_injection_judge", "prompt_policy_judge", "llm_analyzer", "policy_evaluation"}, []riskhealth.Component{
		riskhealth.ComponentPromptInjectionJudge,
		riskhealth.ComponentPromptPolicyJudge,
		riskhealth.ComponentLLMAnalyzer,
		riskhealth.ComponentPolicyEvaluation,
	})
	require.Equal(t, []riskhealth.Outcome{"completed", "degraded", "canceled"}, []riskhealth.Outcome{
		riskhealth.OutcomeCompleted,
		riskhealth.OutcomeDegraded,
		riskhealth.OutcomeCanceled,
	})
	require.Equal(t, []riskhealth.Reason{
		"none", "insufficient_credits", "key_disabled", "unauthorized", "rate_limited", "throttled",
		"upstream_unavailable", "timeout", "content_policy", "bad_request", "malformed_response",
		"truncated_response", "input_too_large", "not_configured", "dependency_unavailable", "policy_error", "error",
	}, []riskhealth.Reason{
		riskhealth.ReasonNone,
		riskhealth.ReasonInsufficientCredits,
		riskhealth.ReasonKeyDisabled,
		riskhealth.ReasonUnauthorized,
		riskhealth.ReasonRateLimited,
		riskhealth.ReasonThrottled,
		riskhealth.ReasonUpstreamUnavailable,
		riskhealth.ReasonTimeout,
		riskhealth.ReasonContentPolicy,
		riskhealth.ReasonBadRequest,
		riskhealth.ReasonMalformedResponse,
		riskhealth.ReasonTruncatedResponse,
		riskhealth.ReasonInputTooLarge,
		riskhealth.ReasonNotConfigured,
		riskhealth.ReasonDependencyUnavailable,
		riskhealth.ReasonPolicyError,
		riskhealth.ReasonError,
	})
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

var _ net.Error = timeoutError{}

func TestReasonFromError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want riskhealth.Reason
	}{
		{name: "nil", err: nil, want: riskhealth.ReasonNone},
		{name: "deadline", err: fmt.Errorf("call: %w", context.DeadlineExceeded), want: riskhealth.ReasonTimeout},
		{name: "network timeout", err: fmt.Errorf("dial: %w", timeoutError{}), want: riskhealth.ReasonTimeout},
		{
			name: "openrouter insufficient credits",
			err:  fmt.Errorf("openrouter completion: %w", &openrouter.HTTPError{StatusCode: http.StatusPaymentRequired, Err: openrouter.ErrInsufficientCredits}),
			want: riskhealth.ReasonInsufficientCredits,
		},
		{name: "platform key disabled", err: fmt.Errorf("resolve key: %w", openrouter.ErrPlatformKeyDisabled), want: riskhealth.ReasonKeyDisabled},
		{
			name: "openrouter rate limited",
			err:  &openrouter.HTTPError{StatusCode: http.StatusTooManyRequests, Err: openrouter.ErrRateLimited},
			want: riskhealth.ReasonRateLimited,
		},
		{
			name: "openrouter content policy",
			err:  &openrouter.HTTPError{StatusCode: http.StatusForbidden, Err: openrouter.ErrContentPolicy},
			want: riskhealth.ReasonContentPolicy,
		},
		{
			name: "openrouter forbidden without content marker",
			err:  &openrouter.HTTPError{StatusCode: http.StatusForbidden, Err: nil},
			want: riskhealth.ReasonUnauthorized,
		},
		{
			name: "openrouter bad request",
			err:  &openrouter.HTTPError{StatusCode: http.StatusBadRequest, Err: openrouter.ErrBadRequest},
			want: riskhealth.ReasonBadRequest,
		},
		{
			name: "openrouter overloaded",
			err:  &openrouter.HTTPError{StatusCode: 529, Err: nil},
			want: riskhealth.ReasonUpstreamUnavailable,
		},
		{name: "typesafe not configured", err: typesafe.ErrUnavailable, want: riskhealth.ReasonNotConfigured},
		{name: "typesafe context length", err: fmt.Errorf("evaluate: %w", typesafe.ErrContextLengthExceeded), want: riskhealth.ReasonInputTooLarge},
		{name: "typesafe status", err: &typesafe.StatusError{StatusCode: http.StatusBadGateway}, want: riskhealth.ReasonUpstreamUnavailable},
		{name: "unclassified", err: errors.New("socket hang up"), want: riskhealth.ReasonError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, riskhealth.ReasonFromError(tc.err))
		})
	}
}

func TestReasonForHTTPStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status int
		want   riskhealth.Reason
	}{
		{status: http.StatusPaymentRequired, want: riskhealth.ReasonInsufficientCredits},
		{status: http.StatusUnauthorized, want: riskhealth.ReasonUnauthorized},
		{status: http.StatusForbidden, want: riskhealth.ReasonUnauthorized},
		{status: http.StatusRequestTimeout, want: riskhealth.ReasonTimeout},
		{status: http.StatusTooManyRequests, want: riskhealth.ReasonRateLimited},
		{status: http.StatusUnprocessableEntity, want: riskhealth.ReasonBadRequest},
		{status: http.StatusInternalServerError, want: riskhealth.ReasonUpstreamUnavailable},
		{status: http.StatusServiceUnavailable, want: riskhealth.ReasonUpstreamUnavailable},
		{status: http.StatusFound, want: riskhealth.ReasonError},
	}
	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, riskhealth.ReasonForHTTPStatus(tc.status))
		})
	}
}

func TestRecorderFailedSeparatesCancellationFromDegradation(t *testing.T) {
	t.Parallel()

	meter := riskhealthtest.NewMeter(t)
	recorder := riskhealth.New(meter.Provider, testenv.NewLogger(t), riskhealth.ComponentPromptPolicyJudge)

	recorder.Failed(t.Context(), "org-a", nil)
	recorder.Failed(t.Context(), "org-a", fmt.Errorf("judge: %w", context.Canceled))
	recorder.Failed(t.Context(), "org-a", &openrouter.HTTPError{StatusCode: http.StatusPaymentRequired, Err: openrouter.ErrInsufficientCredits})
	recorder.Degraded(t.Context(), "org-b", riskhealth.ReasonDependencyUnavailable, attr.RiskPolicyID("policy-1"))

	sample := func(org string, outcome riskhealth.Outcome, reason riskhealth.Reason, policyID string) riskhealthtest.Sample {
		return riskhealthtest.Sample{OrgID: org, Component: riskhealth.ComponentPromptPolicyJudge, Outcome: outcome, Reason: reason, PolicyID: policyID}
	}
	require.Equal(t, map[riskhealthtest.Sample]int64{
		sample("org-a", riskhealth.OutcomeCompleted, riskhealth.ReasonNone, ""):                         1,
		sample("org-a", riskhealth.OutcomeCanceled, riskhealth.ReasonNone, ""):                          1,
		sample("org-a", riskhealth.OutcomeDegraded, riskhealth.ReasonInsufficientCredits, ""):           1,
		sample("org-b", riskhealth.OutcomeDegraded, riskhealth.ReasonDependencyUnavailable, "policy-1"): 1,
	}, meter.Evaluations(t))
}

func TestNilRecorderRecordsNothing(t *testing.T) {
	t.Parallel()

	var recorder *riskhealth.Recorder
	recorder.Completed(t.Context(), "org-a")
	recorder.Degraded(t.Context(), "org-a", riskhealth.ReasonError)
	recorder.Canceled(t.Context(), "org-a")
	recorder.Failed(t.Context(), "org-a", errors.New("boom"))
}
