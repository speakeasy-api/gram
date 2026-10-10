package openrouter

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/riskhealth"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func judgeInput() promptpolicy.Input {
	return promptpolicy.Input{
		OrgID:     "org-b",
		ProjectID: "proj",
		UserID:    "user-1",
		Prompt:    "flag secrets",
		Message:   judgemessage.New(message.User, "", "hello"),
		Config:    promptpolicy.Config{Temperature: nil, FailOpen: true},
	}
}

// A customer's prompt policy that cannot be evaluated is the "silently
// no-ops" case from the incident: Evaluate returns no verdict and no error,
// so nothing downstream can tell it apart from a policy that found nothing.
func TestEvaluateReportsPolicyFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		client     openrouter.CompletionClient
		wantReason riskhealth.Reason
	}{
		{name: "no judge wired", client: nil, wantReason: riskhealth.ReasonNotConfigured},
		{
			name: "drained balance",
			client: &countingCompletionClient{err: &openrouter.HTTPError{
				StatusCode: http.StatusPaymentRequired,
				Err:        openrouter.ErrInsufficientCredits,
			}},
			wantReason: riskhealth.ReasonInsufficientCredits,
		},
		{
			name: "provider outage",
			client: &countingCompletionClient{err: &openrouter.HTTPError{
				StatusCode: http.StatusBadGateway,
				Err:        nil,
			}},
			wantReason: riskhealth.ReasonUpstreamUnavailable,
		},
		{
			name:       "unparseable verdict",
			client:     &successfulCompletionClient{body: "not json", usage: openrouter.Usage{}},
			wantReason: riskhealth.ReasonMalformedResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := sdkmetric.NewManualReader()
			meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })

			judge := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), meterProvider, tt.client, testJudgeLimiter(t))
			_, _ = judge.Evaluate(t.Context(), judgeInput())

			points := availabilityPoints(t, reader)
			require.Len(t, points, 1)
			for set, value := range points {
				require.Equal(t, int64(1), value)
				require.Equal(t, string(riskhealth.OutcomeDegraded), attrValue(t, set, "gram.outcome"))
				require.Equal(t, string(tt.wantReason), attrValue(t, set, "gram.risk.degradation_reason"))
				require.Equal(t, string(riskhealth.ComponentPromptPolicyJudge), attrValue(t, set, "gram.risk.component"))
				require.Equal(t, "org-b", attrValue(t, set, "gram.org.id"))
			}
		})
	}
}

func TestEvaluateRecordsCompletedVerdict(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })

	client := &successfulCompletionClient{
		body:  `{"matched":false,"confidence":0.1,"rationale":"safe"}`,
		usage: openrouter.Usage{},
	}
	judge := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), meterProvider, client, testJudgeLimiter(t))

	verdict, err := judge.Evaluate(t.Context(), judgeInput())
	require.NoError(t, err)
	require.NotNil(t, verdict)

	points := availabilityPoints(t, reader)
	require.Len(t, points, 1)
	for set, value := range points {
		require.Equal(t, int64(1), value)
		require.Equal(t, string(riskhealth.OutcomeCompleted), attrValue(t, set, "gram.outcome"))
	}
}

// Gram's own limiter refusing a call is a Gram-side quota problem, not a
// provider one, so it must not be filed under the provider's rate limit.
func TestEvaluateThrottledIsItsOwnReason(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })

	client := &countingCompletionClient{err: nil}
	judge := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), meterProvider, client, testJudgeLimiter(t))
	drainLimiter(t, judge)

	_, err := judge.Evaluate(t.Context(), judgeInput())
	require.ErrorIs(t, err, promptpolicy.ErrRateLimited)

	points := availabilityPoints(t, reader)
	require.Len(t, points, 1)
	for set := range points {
		require.Equal(t, string(riskhealth.ReasonThrottled), attrValue(t, set, "gram.risk.degradation_reason"))
	}
}

func availabilityPoints(t *testing.T, reader *sdkmetric.ManualReader) map[attribute.Set]int64 {
	t.Helper()

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))

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

func attrValue(t *testing.T, set attribute.Set, key string) string {
	t.Helper()

	value, ok := set.Value(attribute.Key(key))
	require.Truef(t, ok, "missing attribute %q", key)
	return value.AsString()
}
