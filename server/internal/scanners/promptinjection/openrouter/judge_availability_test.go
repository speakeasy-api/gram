package openrouter

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/riskhealth"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	gramopenrouter "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// A drained OpenRouter balance is the failure that took prompt-injection
// scanning offline without anything paging. It must be distinguishable from
// every other judge error, on both the fail-open breakdown and the
// cross-engine availability counter.
func TestJudgeFailureReasonsAreActionable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantFailOp string
		wantReason riskhealth.Reason
	}{
		{
			name:       "drained balance",
			err:        &gramopenrouter.HTTPError{StatusCode: http.StatusPaymentRequired, Err: gramopenrouter.ErrInsufficientCredits},
			wantFailOp: "insufficient_credits",
			wantReason: riskhealth.ReasonInsufficientCredits,
		},
		{
			name:       "provider throttling",
			err:        &gramopenrouter.HTTPError{StatusCode: http.StatusTooManyRequests, Err: gramopenrouter.ErrRateLimited},
			wantFailOp: "rate_limited",
			wantReason: riskhealth.ReasonRateLimited,
		},
		{
			name:       "provider outage",
			err:        &gramopenrouter.HTTPError{StatusCode: http.StatusServiceUnavailable, Err: nil},
			wantFailOp: "upstream_unavailable",
			wantReason: riskhealth.ReasonUpstreamUnavailable,
		},
		{
			name:       "revoked key",
			err:        &gramopenrouter.HTTPError{StatusCode: http.StatusUnauthorized, Err: nil},
			wantFailOp: "unauthorized",
			wantReason: riskhealth.ReasonUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := sdkmetric.NewManualReader()
			meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })

			client := &fakeCompletionClient{err: tt.err}
			engine := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), meterProvider, client, testJudgeLimiter(t))

			results, err := engine.Classify(t.Context(), req("current event"))
			require.NoError(t, err)
			require.Len(t, results, 1)
			require.Equal(t, promptinjection.LabelUnavailable, results[0].Label, "a judge failure still fails open")

			var collected metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(t.Context(), &collected))

			failOpen := counterPoints(t, collected, meterTypedFailOpen)
			require.Len(t, failOpen, 1)
			require.Equal(t, tt.wantFailOp, metricAttrs(failOpen[0].Attributes)["reason"].AsString())

			availability := counterPoints(t, collected, riskhealth.MeterEvaluations)
			require.Len(t, availability, 1)
			attrs := metricAttrs(availability[0].Attributes)
			require.Equal(t, string(riskhealth.OutcomeDegraded), attrs["gram.outcome"].AsString())
			require.Equal(t, string(tt.wantReason), attrs["gram.risk.degradation_reason"].AsString())
			require.Equal(t, string(riskhealth.ComponentPromptInjectionJudge), attrs["gram.risk.component"].AsString())
			require.Equal(t, "org-a", attrs["gram.org.id"].AsString())
		})
	}
}

func TestJudgeRecordsCompletedVerdicts(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })

	client := &fakeCompletionClient{responder: func(string) string { return safeVerdictJSON }}
	engine := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), meterProvider, client, testJudgeLimiter(t))

	_, err := engine.Classify(t.Context(), req("first", "second"))
	require.NoError(t, err)

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))

	points := counterPoints(t, collected, riskhealth.MeterEvaluations)
	require.Len(t, points, 1)
	require.Equal(t, int64(2), points[0].Value)
	attrs := metricAttrs(points[0].Attributes)
	require.Equal(t, string(riskhealth.OutcomeCompleted), attrs["gram.outcome"].AsString())
	require.Equal(t, "none", attrs["gram.risk.degradation_reason"].AsString())
}

// A malformed answer is not an outage on the provider's side, but the message
// still went unjudged, so it has to land in the degraded bucket with a reason
// that does not send the on-call looking at OpenRouter's status page.
func TestJudgeMalformedVerdictIsDegraded(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })

	client := &fakeCompletionClient{responder: func(string) string { return "not json" }}
	engine := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), meterProvider, client, testJudgeLimiter(t))

	_, err := engine.Classify(t.Context(), req("current event"))
	require.NoError(t, err)

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))

	points := counterPoints(t, collected, riskhealth.MeterEvaluations)
	require.Len(t, points, 1)
	attrs := metricAttrs(points[0].Attributes)
	require.Equal(t, string(riskhealth.OutcomeDegraded), attrs["gram.outcome"].AsString())
	require.Equal(t, string(riskhealth.ReasonMalformedResponse), attrs["gram.risk.degradation_reason"].AsString())
}

// An abandoned request is an ordinary consequence of a deploy draining
// in-flight work. Counting it as degraded would keep the availability ratio
// permanently noisy.
func TestJudgeCancellationIsNotDegraded(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })

	client := &fakeCompletionClient{blockUntilCanceled: true}
	engine := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), meterProvider, client, testJudgeLimiter(t))

	ctx, cancel := context.WithCancel(t.Context())
	client.onCompletion = func(context.Context) { cancel() }

	_, err := engine.Classify(ctx, req("current event"))
	require.NoError(t, err)

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))

	points := counterPoints(t, collected, riskhealth.MeterEvaluations)
	require.Len(t, points, 1)
	attrs := metricAttrs(points[0].Attributes)
	require.Equal(t, string(riskhealth.OutcomeCanceled), attrs["gram.outcome"].AsString())
}

func TestTypedFailureReasonKeepsProviderClassification(t *testing.T) {
	t.Parallel()

	credits := fmt.Errorf("openrouter completion: %w", &gramopenrouter.HTTPError{
		StatusCode: http.StatusPaymentRequired,
		Err:        gramopenrouter.ErrInsufficientCredits,
	})
	require.Equal(t, "insufficient_credits", typedFailureReason(credits, "failure"))
	require.Equal(t, "error", typedFailureReason(fmt.Errorf("socket hang up"), "failure"))
}
