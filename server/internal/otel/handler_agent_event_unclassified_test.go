package otel

import (
	"context"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// unclassifiedCount sums the rows_unclassified counter's data points whose
// attribute set contains every given attribute.
func unclassifiedCount(t *testing.T, reader *sdkmetric.ManualReader, want ...attribute.KeyValue) int64 {
	t.Helper()

	var resourceMetrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &resourceMetrics))

	var total int64
	for _, scope := range resourceMetrics.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != meterAgentEventCHWriterRowsUnclassified {
				continue
			}
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			require.True(t, ok, "rows_unclassified should be an int64 sum")
			for _, point := range sum.DataPoints {
				matches := true
				for _, kv := range want {
					if got, ok := point.Attributes.Value(kv.Key); !ok || got != kv.Value {
						matches = false
						break
					}
				}
				if matches {
					total += point.Value
				}
			}
		}
	}
	return total
}

func TestAgentEventCHWriterCountsUnclassifiedRows(t *testing.T) {
	t.Parallel()

	t.Run("it counts unclassified log records by source and raw name, after they are written", func(t *testing.T) {
		t.Parallel()
		reader := sdkmetric.NewManualReader()
		meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })

		inserter := &captureAgentEventInserter{batches: nil, err: nil}
		writer := NewAgentEventLogCHWriter(testenv.NewLogger(t), meterProvider, inserter)

		classified := agentEventTestLog(claudeCodeScopeName, "api_request")
		unknownName := agentEventTestLog(claudeCodeScopeName, "custom.thing")
		unknownProducer := agentEventTestLog("com.example.app", "custom.thing")

		require.NoError(t, writer.HandleBatch(t.Context(), []*otelv1.LogRecord{classified, unknownName, unknownProducer}, nil))
		require.Len(t, inserter.batches[0], 3, "unclassified rows are written, not dropped")

		require.Equal(t, int64(2), unclassifiedCount(t, reader,
			attr.AgentEventSource("claude-code"),
			attr.AgentEventSignal("log"),
			attr.AgentEventRawName("custom.thing"),
		))
		require.Equal(t, int64(2), unclassifiedCount(t, reader))
	})

	t.Run("it does not count rows the insert rejected", func(t *testing.T) {
		t.Parallel()
		reader := sdkmetric.NewManualReader()
		meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })

		inserter := &captureAgentEventInserter{batches: nil, err: context.DeadlineExceeded}
		writer := NewAgentEventLogCHWriter(testenv.NewLogger(t), meterProvider, inserter)

		require.Error(t, writer.HandleBatch(t.Context(), []*otelv1.LogRecord{agentEventTestLog("com.example.app", "custom.thing")}, nil))
		require.Zero(t, unclassifiedCount(t, reader))
	})

	t.Run("it counts unclassified spans without their unbounded names", func(t *testing.T) {
		t.Parallel()
		reader := sdkmetric.NewManualReader()
		meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })

		inserter := &captureAgentEventInserter{batches: nil, err: nil}
		writer := NewAgentEventSpanCHWriter(testenv.NewLogger(t), meterProvider, inserter)

		span := spanEventTestSpan("org-1", "my-agent")
		span.SetName("GET /health/" + "very-specific-path")
		span.SetAttributes(nil)

		require.NoError(t, writer.HandleBatch(t.Context(), []*otelv1.Span{span}, nil))
		require.Equal(t, int64(1), unclassifiedCount(t, reader,
			attr.AgentEventSource("my-agent"),
			attr.AgentEventSignal("span"),
			attr.AgentEventRawName(""),
		))
	})
}
