package hooks

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestMetrics_RecordHookEventDuration(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	metrics := newMetrics(meterProvider, testenv.NewLogger(t))

	metrics.RecordHookEventDuration(ctx, "claude", "PreToolUse", hookMetricOutcomeAccepted, hookMetricDecisionDeny, "acme", true, 150*time.Millisecond)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	histogramPoint := findHookEventDurationPoint(t, rm)
	require.Equal(t, uint64(1), histogramPoint.Count)
	require.InDelta(t, 0.15, histogramPoint.Sum, 0.0001)

	value, ok := histogramPoint.Attributes.Value(attr.HookSourceKey)
	require.True(t, ok)
	require.Equal(t, "claude", value.AsString())

	value, ok = histogramPoint.Attributes.Value(attr.HookEventKey)
	require.True(t, ok)
	require.Equal(t, "PreToolUse", value.AsString())

	value, ok = histogramPoint.Attributes.Value(attr.OutcomeKey)
	require.True(t, ok)
	require.Equal(t, hookMetricOutcomeAccepted, value.AsString())

	value, ok = histogramPoint.Attributes.Value(attr.HookDecisionKey)
	require.True(t, ok)
	require.Equal(t, hookMetricDecisionDeny, value.AsString())

	value, ok = histogramPoint.Attributes.Value(attr.OrganizationSlugKey)
	require.True(t, ok)
	require.Equal(t, "acme", value.AsString())

	value, ok = histogramPoint.Attributes.Value(attr.HookRiskScannedKey)
	require.True(t, ok)
	require.True(t, value.AsBool())
}

func TestMetrics_RecordDeliveryFailureFillsEmptyDimensions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	metrics := newMetrics(meterProvider, testenv.NewLogger(t))

	metrics.RecordDeliveryFailure(ctx, deliveryFailureMetricParams{
		EventType:     "tool.requested",
		Phase:         "drain",
		Cause:         "dns",
		Harness:       "",
		OS:            "linux",
		BinaryVersion: "",
		OrgSlug:       "",
	})

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	var point metricdata.DataPoint[int64]
	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != meterHooksDeliveryFailure {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Len(t, sum.DataPoints, 1)
			point = sum.DataPoints[0]
			found = true
		}
	}
	require.True(t, found, "missing metric %q", meterHooksDeliveryFailure)
	require.Equal(t, int64(1), point.Value)

	value, ok := point.Attributes.Value(attr.HookDeliveryCauseKey)
	require.True(t, ok)
	require.Equal(t, "dns", value.AsString())

	value, ok = point.Attributes.Value(attr.HookDeviceHarnessKey)
	require.True(t, ok)
	require.Equal(t, hookMetricDimensionUnknown, value.AsString(), "an empty dimension becomes a bucket, not an absent tag")

	value, ok = point.Attributes.Value(attr.HookDeviceBinaryVersionKey)
	require.True(t, ok)
	require.Equal(t, hookMetricDimensionUnknown, value.AsString())

	_, ok = point.Attributes.Value(attr.OrganizationSlugKey)
	require.False(t, ok, "an empty org slug is omitted, matching the duration histogram")
}

func TestClaudeHookDecision(t *testing.T) {
	t.Parallel()

	require.Equal(t, hookMetricDecisionNone, claudeHookDecision(nil))
	require.Equal(t, hookMetricDecisionAllow, claudeHookDecision(makeHookResult("Stop")))
	require.Equal(t, hookMetricDecisionAllow, claudeHookDecision(makeHookResult("PreToolUse")))
	require.Equal(t, hookMetricDecisionDeny, claudeHookDecision(constructBlockResponse("UserPromptSubmit", "blocked")))
	require.Equal(t, hookMetricDecisionDeny, claudeHookDecision(constructBlockResponse("PreToolUse", "blocked")))
	require.Equal(t, hookMetricDecisionDeny, claudeHookDecision(constructWarnChallengeResponse("PreToolUse", "agent", "user")))
}

func TestCursorHookDecision(t *testing.T) {
	t.Parallel()

	require.Equal(t, hookMetricDecisionNone, cursorHookDecision(nil))
	require.Equal(t, hookMetricDecisionAllow, cursorHookDecision(&gen.CursorHookResult{
		Permission:        nil,
		UserMessage:       nil,
		AdditionalContext: nil,
		AgentMessage:      nil,
	}))
	require.Equal(t, hookMetricDecisionDeny, cursorHookDecision(&gen.CursorHookResult{
		Permission:        new("deny"),
		UserMessage:       nil,
		AdditionalContext: nil,
		AgentMessage:      nil,
	}))
	require.Equal(t, "ask", cursorHookDecision(&gen.CursorHookResult{
		Permission:        new("ask"),
		UserMessage:       nil,
		AdditionalContext: nil,
		AgentMessage:      nil,
	}))
}

func TestCodexHookDecision(t *testing.T) {
	t.Parallel()

	require.Equal(t, hookMetricDecisionNone, codexHookDecision(nil))
	require.Equal(t, hookMetricDecisionAllow, codexHookDecision(&gen.CodexHookResult{
		Decision: nil,
		Reason:   nil,
	}))
	require.Equal(t, hookMetricDecisionDeny, codexHookDecision(&gen.CodexHookResult{
		Decision: new("deny"),
		Reason:   nil,
	}))
}

func findHookEventDurationPoint(t *testing.T, rm metricdata.ResourceMetrics) metricdata.HistogramDataPoint[float64] {
	t.Helper()

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != meterHooksEventDuration {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			require.Len(t, histogram.DataPoints, 1)
			return histogram.DataPoints[0]
		}
	}

	require.Failf(t, "metric not found", "missing metric %q", meterHooksEventDuration)
	return metricdata.HistogramDataPoint[float64]{}
}
