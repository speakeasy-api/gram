package llmanalyzer_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/riskhealth"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// The analyzer turns every model failure into a dead-letter result rather
// than an error, so its outages are invisible unless they are counted. A
// deployment with no model URL configured is the quietest case of all: it
// never calls anything.
func TestAnalyzeRecordsAvailability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		completer   llmanalyzer.Completer
		wantOutcome riskhealth.Outcome
		wantReason  riskhealth.Reason
	}{
		{
			name:        "clean verdict",
			completer:   &llmanalyzer.StubCompleter{Response: llmanalyzer.VerdictJSON(map[string]int{}, "clean"), Model: "risk-judge-4b"},
			wantOutcome: riskhealth.OutcomeCompleted,
			wantReason:  riskhealth.ReasonNone,
		},
		{
			name:        "no model configured",
			completer:   nil,
			wantOutcome: riskhealth.OutcomeDegraded,
			wantReason:  riskhealth.ReasonNotConfigured,
		},
		{
			name:        "model outage",
			completer:   &llmanalyzer.StubCompleter{Err: &llmanalyzer.UpstreamError{Status: http.StatusServiceUnavailable}},
			wantOutcome: riskhealth.OutcomeDegraded,
			wantReason:  riskhealth.ReasonUpstreamUnavailable,
		},
		{
			name:        "model throttling",
			completer:   &llmanalyzer.StubCompleter{Err: &llmanalyzer.UpstreamError{Status: http.StatusTooManyRequests}},
			wantOutcome: riskhealth.OutcomeDegraded,
			wantReason:  riskhealth.ReasonRateLimited,
		},
		{
			name:        "model timeout",
			completer:   &llmanalyzer.StubCompleter{Err: llmanalyzer.ErrTimeout},
			wantOutcome: riskhealth.OutcomeDegraded,
			wantReason:  riskhealth.ReasonTimeout,
		},
		{
			name:        "unparseable verdict",
			completer:   &llmanalyzer.StubCompleter{Response: "not json", Model: "risk-judge-4b"},
			wantOutcome: riskhealth.OutcomeDegraded,
			wantReason:  riskhealth.ReasonMalformedResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := sdkmetric.NewManualReader()
			meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })

			analyzer := llmanalyzer.NewAnalyzer(testenv.NewLogger(t), testenv.NewTracerProvider(t), meterProvider, tt.completer)
			analyzer.Analyze(t.Context(), userRequest("hello world"))

			var collected metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(t.Context(), &collected))

			points := availabilityPoints(t, collected)
			require.Len(t, points, 1)
			for set, value := range points {
				require.Equal(t, int64(1), value)
				require.Equal(t, string(tt.wantOutcome), setValue(t, set, "gram.outcome"))
				require.Equal(t, string(tt.wantReason), setValue(t, set, "gram.risk.degradation_reason"))
				require.Equal(t, string(riskhealth.ComponentLLMAnalyzer), setValue(t, set, "gram.risk.component"))
				require.Equal(t, "org-1", setValue(t, set, "gram.org.id"))
			}
		})
	}
}

func availabilityPoints(t *testing.T, collected metricdata.ResourceMetrics) map[attribute.Set]int64 {
	t.Helper()

	points := map[attribute.Set]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != riskhealth.MeterEvaluations {
				continue
			}
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				points[point.Attributes] += point.Value
			}
		}
	}
	return points
}

func setValue(t *testing.T, set attribute.Set, key string) string {
	t.Helper()

	value, ok := set.Value(attribute.Key(key))
	require.Truef(t, ok, "missing attribute %q", key)
	return value.AsString()
}
