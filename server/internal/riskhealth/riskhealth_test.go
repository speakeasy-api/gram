// The wire names in this file are duplicated string literals on purpose. This
// is the metric Datadog monitors are written against, so a rename that does
// not also update the runbook should break a test rather than silently
// disable the alerting.
package riskhealth_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/riskhealth"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func TestMetricsRecordsWireContract(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	reader := sdkmetric.NewManualReader()
	metrics := riskhealth.NewMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), testenv.NewLogger(t))

	metrics.RecordCompleted(ctx, "org_1", riskhealth.ComponentPromptInjectionJudge)
	metrics.RecordCompleted(ctx, "org_1", riskhealth.ComponentPromptInjectionJudge)
	metrics.RecordDegraded(ctx, "org_1", riskhealth.ComponentPromptInjectionJudge, riskhealth.ReasonInsufficientCredits)
	metrics.RecordCanceled(ctx, "org_1", riskhealth.ComponentPromptInjectionJudge)

	points := collect(t, reader)
	require.Len(t, points, 3, "one series per outcome")

	assert.Equal(t, int64(2), points[attribute.NewSet(
		attribute.String("gram.org.id", "org_1"),
		attribute.String("gram.risk.component", "prompt_injection_judge"),
		attribute.String("gram.outcome", "completed"),
		attribute.String("gram.risk.degradation_reason", "none"),
	)])
	assert.Equal(t, int64(1), points[attribute.NewSet(
		attribute.String("gram.org.id", "org_1"),
		attribute.String("gram.risk.component", "prompt_injection_judge"),
		attribute.String("gram.outcome", "degraded"),
		attribute.String("gram.risk.degradation_reason", "insufficient_credits"),
	)])
	assert.Equal(t, int64(1), points[attribute.NewSet(
		attribute.String("gram.org.id", "org_1"),
		attribute.String("gram.risk.component", "prompt_injection_judge"),
		attribute.String("gram.outcome", "canceled"),
		attribute.String("gram.risk.degradation_reason", "none"),
	)])
}

// A degraded evaluation always carries a reason: "none" on the degraded
// outcome would make the credit-exhaustion monitor silently under-count.
func TestRecordDegradedNeverReportsNoReason(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	reader := sdkmetric.NewManualReader()
	metrics := riskhealth.NewMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), testenv.NewLogger(t))

	metrics.RecordDegraded(ctx, "org_1", riskhealth.ComponentPolicyEvaluation, riskhealth.ReasonNone)

	for set := range collect(t, reader) {
		reason, ok := set.Value("gram.risk.degradation_reason")
		require.True(t, ok)
		assert.Equal(t, "error", reason.AsString())
	}
}

func TestRecordResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantOutcom string
		wantReason string
	}{
		{name: "success", err: nil, wantOutcom: "completed", wantReason: "none"},
		{
			name:       "drained balance",
			err:        fmt.Errorf("judge: %w", &openrouter.HTTPError{StatusCode: http.StatusPaymentRequired, Err: openrouter.ErrInsufficientCredits}),
			wantOutcom: "degraded",
			wantReason: "insufficient_credits",
		},
		{
			name:       "provider outage",
			err:        &openrouter.HTTPError{StatusCode: http.StatusBadGateway, Err: nil},
			wantOutcom: "degraded",
			wantReason: "upstream_unavailable",
		},
		{
			name:       "deadline",
			err:        fmt.Errorf("judge: %w", context.DeadlineExceeded),
			wantOutcom: "degraded",
			wantReason: "timeout",
		},
		{
			name:       "caller walked away",
			err:        fmt.Errorf("judge: %w", context.Canceled),
			wantOutcom: "canceled",
			wantReason: "none",
		},
		{name: "unclassified", err: errors.New("socket hang up"), wantOutcom: "degraded", wantReason: "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			reader := sdkmetric.NewManualReader()
			metrics := riskhealth.NewMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), testenv.NewLogger(t))

			metrics.RecordResult(ctx, "org_1", riskhealth.ComponentPromptPolicyJudge, tt.err)

			points := collect(t, reader)
			require.Len(t, points, 1)
			for set, value := range points {
				assert.Equal(t, int64(1), value)
				outcome, ok := set.Value("gram.outcome")
				require.True(t, ok)
				assert.Equal(t, tt.wantOutcom, outcome.AsString())
				reason, ok := set.Value("gram.risk.degradation_reason")
				require.True(t, ok)
				assert.Equal(t, tt.wantReason, reason.AsString())
			}
		})
	}
}

// A content-policy refusal is a verdict about the payload, not an outage, so
// it must not inflate the degradation ratio that pages the on-call.
func TestReasonFromErrorIgnoresNonOutages(t *testing.T) {
	t.Parallel()

	assert.Equal(t, riskhealth.ReasonNone, riskhealth.ReasonFromError(nil))
	assert.Equal(t, riskhealth.ReasonNone, riskhealth.ReasonFromError(
		&openrouter.HTTPError{StatusCode: http.StatusForbidden, Err: openrouter.ErrContentPolicy},
	))
	assert.Equal(t, riskhealth.ReasonNone, riskhealth.ReasonFromError(context.Canceled))
}

func TestNilMetricsIsInert(t *testing.T) {
	t.Parallel()

	var metrics *riskhealth.Metrics
	require.NotPanics(t, func() {
		metrics.RecordCompleted(t.Context(), "org_1", riskhealth.ComponentLLMAnalyzer)
		metrics.RecordDegraded(t.Context(), "org_1", riskhealth.ComponentLLMAnalyzer, riskhealth.ReasonTimeout)
		metrics.RecordCanceled(t.Context(), "org_1", riskhealth.ComponentLLMAnalyzer)
		metrics.RecordResult(t.Context(), "org_1", riskhealth.ComponentLLMAnalyzer, errors.New("boom"))
	})
}

func collect(t *testing.T, reader *sdkmetric.ManualReader) map[attribute.Set]int64 {
	t.Helper()

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))

	points := map[attribute.Set]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "risk.analysis.evaluations" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "risk.analysis.evaluations must stay a monotonic counter")
			for _, point := range sum.DataPoints {
				points[point.Attributes] += point.Value
			}
		}
	}
	return points
}
