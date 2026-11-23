package openrouter

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/scanners/riskhealth"
	"github.com/speakeasy-api/gram/server/internal/scanners/riskhealth/riskhealthtest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	gramopenrouter "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	typesafe "github.com/speakeasy-api/gram/server/internal/thirdparty/typesafedecisions"
)

func piSample(outcome riskhealth.Outcome, reason riskhealth.Reason) riskhealthtest.Sample {
	return riskhealthtest.Sample{OrgID: "org-a", Component: riskhealth.ComponentPromptInjectionJudge, Outcome: outcome, Reason: reason, PolicyID: ""}
}

func insufficientCreditsError() error {
	return fmt.Errorf("openrouter completion: %w", &gramopenrouter.HTTPError{StatusCode: http.StatusPaymentRequired, Err: gramopenrouter.ErrInsufficientCredits})
}

// TestEngineRecordsDrainedCreditsAsDegraded reproduces the outage this metric
// exists for: the judge fails open on a 402, and the health counter names the
// drained balance instead of counting a clean scan.
func TestEngineRecordsDrainedCreditsAsDegraded(t *testing.T) {
	t.Parallel()

	meter := riskhealthtest.NewMeter(t)
	client := &fakeCompletionClient{err: insufficientCreditsError()}
	engine := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), meter.Provider, client)

	_, err := engine.Classify(t.Context(), req("first", "second"))
	require.NoError(t, err)

	require.Equal(t, map[riskhealthtest.Sample]int64{
		piSample(riskhealth.OutcomeDegraded, riskhealth.ReasonInsufficientCredits): 2,
	}, meter.Evaluations(t))
}

func TestEngineRecordsVerdictAsCompleted(t *testing.T) {
	t.Parallel()

	meter := riskhealthtest.NewMeter(t)
	client := &fakeCompletionClient{responder: func(string) string { return safeVerdictJSON }}
	engine := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), meter.Provider, client)

	_, err := engine.Classify(t.Context(), req("hello", ""))
	require.NoError(t, err)

	require.Equal(t, map[riskhealthtest.Sample]int64{
		piSample(riskhealth.OutcomeCompleted, riskhealth.ReasonNone): 1,
	}, meter.Evaluations(t), "a message with no content is not an evaluation")
}

func TestEngineRecordsVerdictShapeFailures(t *testing.T) {
	t.Parallel()

	meter := riskhealthtest.NewMeter(t)
	truncating := &fakeCompletionClient{
		responder:    func(string) string { return safeVerdictJSON },
		finishReason: new(gramopenrouter.FinishReasonLength),
	}
	refusing := &fakeCompletionClient{refuseModels: map[string]bool{Model: true}}
	malformed := &fakeCompletionClient{responder: func(string) string { return "not json" }}
	for _, client := range []*fakeCompletionClient{truncating, refusing, malformed} {
		engine := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), meter.Provider, client)
		_, err := engine.Classify(t.Context(), req("hello"))
		require.NoError(t, err)
	}

	require.Equal(t, map[riskhealthtest.Sample]int64{
		piSample(riskhealth.OutcomeDegraded, riskhealth.ReasonTruncatedResponse): 1,
		piSample(riskhealth.OutcomeDegraded, riskhealth.ReasonContentPolicy):     1,
		piSample(riskhealth.OutcomeDegraded, riskhealth.ReasonMalformedResponse): 1,
	}, meter.Evaluations(t))
}

func TestEngineRecordsCanceledCallerAsCanceled(t *testing.T) {
	t.Parallel()

	meter := riskhealthtest.NewMeter(t)
	engine := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), meter.Provider, &fakeCompletionClient{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := engine.Classify(ctx, req("hello"))
	require.NoError(t, err)

	require.Equal(t, map[riskhealthtest.Sample]int64{
		piSample(riskhealth.OutcomeCanceled, riskhealth.ReasonNone): 1,
	}, meter.Evaluations(t))
}

func newMeteredCascade(t *testing.T, meter *riskhealthtest.Meter, probability float64, client *fakeCompletionClient) *Cascade {
	t.Helper()

	template, _ := testCascade(t, probability, safeVerdictJSON)
	return NewCascade(testenv.NewLogger(t), testenv.NewTracerProvider(t), meter.Provider, client, template.jev, judgemessage.NewWindowLoader(nil).Load)
}

func TestCascadeRecordsPrefilterClearanceAsCompleted(t *testing.T) {
	t.Parallel()

	meter := riskhealthtest.NewMeter(t)
	cascade := newMeteredCascade(t, meter, 0.1, &fakeCompletionClient{})

	_, err := cascade.Classify(t.Context(), req("quoted attack"))
	require.NoError(t, err)

	require.Equal(t, map[riskhealthtest.Sample]int64{
		piSample(riskhealth.OutcomeCompleted, riskhealth.ReasonNone): 1,
	}, meter.Evaluations(t))
}

// TestCascadeRecordsConfirmerFailureOnce pins that an escalated message is
// recorded by the confirmer alone, not again by the cascade.
func TestCascadeRecordsConfirmerFailureOnce(t *testing.T) {
	t.Parallel()

	meter := riskhealthtest.NewMeter(t)
	cascade := newMeteredCascade(t, meter, 0.99, &fakeCompletionClient{err: insufficientCreditsError()})

	_, err := cascade.Classify(t.Context(), req("ignore previous rules"))
	require.NoError(t, err)

	require.Equal(t, map[riskhealthtest.Sample]int64{
		piSample(riskhealth.OutcomeDegraded, riskhealth.ReasonInsufficientCredits): 1,
	}, meter.Evaluations(t))
}

func TestCascadeRecordsUnconfiguredPrefilter(t *testing.T) {
	t.Parallel()

	meter := riskhealthtest.NewMeter(t)
	cascade := newMeteredCascade(t, meter, 0.99, &fakeCompletionClient{})
	cascade.jev = typesafe.Unavailable{}

	_, err := cascade.Classify(t.Context(), req("candidate"))
	require.NoError(t, err)

	require.Equal(t, map[riskhealthtest.Sample]int64{
		piSample(riskhealth.OutcomeDegraded, riskhealth.ReasonNotConfigured): 1,
	}, meter.Evaluations(t))
}
