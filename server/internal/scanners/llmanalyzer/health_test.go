package llmanalyzer_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
	"github.com/speakeasy-api/gram/server/internal/scanners/riskhealth"
	"github.com/speakeasy-api/gram/server/internal/scanners/riskhealth/riskhealthtest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func analyzerSample(outcome riskhealth.Outcome, reason riskhealth.Reason) riskhealthtest.Sample {
	return riskhealthtest.Sample{OrgID: "org-1", Component: riskhealth.ComponentLLMAnalyzer, Outcome: outcome, Reason: reason, PolicyID: ""}
}

func analyze(t *testing.T, meter *riskhealthtest.Meter, completer llmanalyzer.Completer) {
	t.Helper()

	llmanalyzer.NewAnalyzer(testenv.NewLogger(t), testenv.NewTracerProvider(t), meter.Provider, completer).Analyze(t.Context(), userRequest("hello"))
}

func TestAnalyzeRecordsHealthPerOutcome(t *testing.T) {
	t.Parallel()

	meter := riskhealthtest.NewMeter(t)
	clean := &llmanalyzer.StubCompleter{Response: llmanalyzer.VerdictJSON(map[string]int{}, "clean"), Model: "risk-judge-4b"}
	drained := &llmanalyzer.StubCompleter{Err: fmt.Errorf("complete: %w", &llmanalyzer.UpstreamError{Status: http.StatusPaymentRequired, Body: ""})}
	overloaded := &llmanalyzer.StubCompleter{Err: &llmanalyzer.UpstreamError{Status: http.StatusServiceUnavailable, Body: ""}}
	timedOut := &llmanalyzer.StubCompleter{Err: fmt.Errorf("%w: deadline", llmanalyzer.ErrTimeout)}
	unparseable := &llmanalyzer.StubCompleter{Response: "not a verdict", Model: "risk-judge-4b"}

	for _, completer := range []llmanalyzer.Completer{clean, drained, overloaded, timedOut, unparseable, nil} {
		analyze(t, meter, completer)
	}

	require.Equal(t, map[riskhealthtest.Sample]int64{
		analyzerSample(riskhealth.OutcomeCompleted, riskhealth.ReasonNone):               1,
		analyzerSample(riskhealth.OutcomeDegraded, riskhealth.ReasonInsufficientCredits): 1,
		analyzerSample(riskhealth.OutcomeDegraded, riskhealth.ReasonUpstreamUnavailable): 1,
		analyzerSample(riskhealth.OutcomeDegraded, riskhealth.ReasonTimeout):             1,
		analyzerSample(riskhealth.OutcomeDegraded, riskhealth.ReasonMalformedResponse):   1,
		analyzerSample(riskhealth.OutcomeDegraded, riskhealth.ReasonNotConfigured):       1,
	}, meter.Evaluations(t))
}
