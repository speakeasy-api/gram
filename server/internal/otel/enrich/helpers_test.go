package enrich

import (
	"context"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const (
	claudeCodeScopeName = "com.anthropic.claude_code.events"
	codexScopeName      = "codex_otel.log_only"
)

func logStringAttribute(key, value string) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &value}).Build(),
	}).Build()
}

// readableMeter is a meter provider whose counters a test can read back, so
// a counter is asserted rather than trusted. The noop provider testenv hands
// out would let a broken wiring pass.
func readableMeter(t *testing.T) (*sdkmetric.ManualReader, *sdkmetric.MeterProvider) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	return reader, provider
}

// counterValue reads one counter's data point carrying the given attribute,
// or zero when none was recorded.
func counterValue(t *testing.T, reader *sdkmetric.ManualReader, metricName string, key attribute.Key, value string) int64 {
	t.Helper()
	var resourceMetrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &resourceMetrics))
	for _, scopeMetrics := range resourceMetrics.ScopeMetrics {
		for _, candidate := range scopeMetrics.Metrics {
			if candidate.Name != metricName {
				continue
			}
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				if got, ok := point.Attributes.Value(key); ok && got.AsString() == value {
					return point.Value
				}
			}
		}
	}
	return 0
}
