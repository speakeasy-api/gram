package openrouter

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/scanners/riskhealth"
	"github.com/speakeasy-api/gram/server/internal/scanners/riskhealth/riskhealthtest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func policyJudgeInput() promptpolicy.Input {
	return promptpolicy.Input{
		OrgID:     "org-a",
		ProjectID: "proj",
		UserID:    "",
		Prompt:    "flag secrets",
		Message:   judgemessage.New(message.User, "", "hello"),
		Config:    promptpolicy.Config{Temperature: nil, FailOpen: true},
	}
}

func policyJudgeSample(outcome riskhealth.Outcome, reason riskhealth.Reason) riskhealthtest.Sample {
	return riskhealthtest.Sample{OrgID: "org-a", Component: riskhealth.ComponentPromptPolicyJudge, Outcome: outcome, Reason: reason, PolicyID: ""}
}

func TestJudgeRecordsDrainedCreditsAsDegraded(t *testing.T) {
	t.Parallel()

	meter := riskhealthtest.NewMeter(t)
	client := &countingCompletionClient{err: fmt.Errorf("object completion: %w", &openrouter.HTTPError{StatusCode: http.StatusPaymentRequired, Err: openrouter.ErrInsufficientCredits})}
	j := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), meter.Provider, client, testJudgeLimiter(t))

	_, err := j.Evaluate(t.Context(), policyJudgeInput())
	require.Error(t, err)

	require.Equal(t, map[riskhealthtest.Sample]int64{
		policyJudgeSample(riskhealth.OutcomeDegraded, riskhealth.ReasonInsufficientCredits): 1,
	}, meter.Evaluations(t))
}

func TestJudgeRecordsVerdictAsCompleted(t *testing.T) {
	t.Parallel()

	meter := riskhealthtest.NewMeter(t)
	client := &successfulCompletionClient{body: `{"matched":false,"confidence":0.1,"rationale":"safe"}`, usage: openrouter.Usage{}}
	j := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), meter.Provider, client, testJudgeLimiter(t))

	_, err := j.Evaluate(t.Context(), policyJudgeInput())
	require.NoError(t, err)

	require.Equal(t, map[riskhealthtest.Sample]int64{
		policyJudgeSample(riskhealth.OutcomeCompleted, riskhealth.ReasonNone): 1,
	}, meter.Evaluations(t))
}

func TestJudgeRecordsVerdictShapeFailures(t *testing.T) {
	t.Parallel()

	meter := riskhealthtest.NewMeter(t)
	truncated := &successfulCompletionClient{body: `{"matched":false,"confidence":0.1,"rationale":"safe"}`, usage: openrouter.Usage{}, finishReason: new(openrouter.FinishReasonLength)}
	malformed := &successfulCompletionClient{body: "not json", usage: openrouter.Usage{}}
	for _, client := range []*successfulCompletionClient{truncated, malformed} {
		j := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), meter.Provider, client, testJudgeLimiter(t))
		_, err := j.Evaluate(t.Context(), policyJudgeInput())
		require.Error(t, err)
	}

	require.Equal(t, map[riskhealthtest.Sample]int64{
		policyJudgeSample(riskhealth.OutcomeDegraded, riskhealth.ReasonTruncatedResponse): 1,
		policyJudgeSample(riskhealth.OutcomeDegraded, riskhealth.ReasonMalformedResponse): 1,
	}, meter.Evaluations(t))
}

func TestJudgeRecordsOwnThrottleSeparatelyFromProviderRateLimit(t *testing.T) {
	t.Parallel()

	meter := riskhealthtest.NewMeter(t)
	j := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), meter.Provider, &countingCompletionClient{}, testJudgeLimiter(t))
	drainLimiter(t, j)

	_, err := j.Evaluate(t.Context(), policyJudgeInput())
	require.ErrorIs(t, err, promptpolicy.ErrRateLimited)

	require.Equal(t, map[riskhealthtest.Sample]int64{
		policyJudgeSample(riskhealth.OutcomeDegraded, riskhealth.ReasonThrottled): 1,
	}, meter.Evaluations(t))
}

// TestJudgeWithoutClientRecordsNotConfigured covers the silent no-op: a judge
// with no completion client returns no verdict and no error, which a
// fail-open policy cannot tell apart from a clean judgement.
func TestJudgeWithoutClientRecordsNotConfigured(t *testing.T) {
	t.Parallel()

	meter := riskhealthtest.NewMeter(t)
	j := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), meter.Provider, nil, testJudgeLimiter(t))

	verdict, err := j.Evaluate(t.Context(), policyJudgeInput())
	require.NoError(t, err)
	require.Nil(t, verdict)

	require.Equal(t, map[riskhealthtest.Sample]int64{
		policyJudgeSample(riskhealth.OutcomeDegraded, riskhealth.ReasonNotConfigured): 1,
	}, meter.Evaluations(t))
}
