package otel

import (
	"context"
	"errors"
	"testing"
	"time"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/otel/chrepo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// readableMeter is a meter provider whose counters a test can read back, so
// the rows_inserted and rows_skipped paths are asserted rather than trusted.
// The noop provider testenv hands out would let a broken wiring pass.
func readableMeter(t *testing.T) (*sdkmetric.ManualReader, *sdkmetric.MeterProvider) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	return reader, provider
}

// agentEventCount reads one counter's data point carrying the given attribute,
// or zero when none was recorded.
func agentEventCount(t *testing.T, reader *sdkmetric.ManualReader, metricName string, key attribute.Key, value string) int64 {
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

type captureAgentEventInserter struct {
	batches [][]chrepo.AgentEventRow
	err     error
}

func (c *captureAgentEventInserter) InsertAgentEvents(_ context.Context, rows []chrepo.AgentEventRow) error {
	c.batches = append(c.batches, rows)
	return c.err
}

func TestAgentEventLogCHWriter(t *testing.T) {
	t.Parallel()

	fixedNow := time.Unix(1_724_600_000, 42)

	t.Run("it inserts the projectable records and drops the poison ones", func(t *testing.T) {
		t.Parallel()
		inserter := &captureAgentEventInserter{batches: nil, err: nil}
		reader, meterProvider := readableMeter(t)
		writer := NewAgentEventLogCHWriter(testenv.NewLogger(t), meterProvider, inserter)
		writer.now = func() time.Time { return fixedNow }

		good := agentEventTestLog(claudeCodeScopeName, "api_request", logEventTestKV("session.id", "s-1"))
		poison := agentEventTestLog(claudeCodeScopeName, "api_request")
		poison.GetProvenance().SetOrganizationId("")
		noObserved := agentEventTestLog(claudeCodeScopeName, "user_prompt")
		noObserved.SetObservedTimeUnixNano(0)

		err := writer.HandleBatch(t.Context(), []*otelv1.LogRecord{good, poison, nil, noObserved}, nil)
		require.NoError(t, err)

		require.Len(t, inserter.batches, 1)
		rows := inserter.batches[0]
		require.Len(t, rows, 2)
		require.Equal(t, "s-1", rows[0].SessionID)
		require.Equal(t, fixedNow.UnixNano(), rows[1].ObservedAtUnixNano, "the consumer clock stands in when the record carries no observed time")

		// The two rows that landed are counted as inserted, and each drop is
		// counted under the reason it was dropped for.
		require.Equal(t, int64(2), agentEventCount(t, reader, meterAgentEventCHWriterRowsInserted, attr.OutcomeKey, string(o11y.OutcomeSuccess)))
		require.Equal(t, int64(1), agentEventCount(t, reader, meterAgentEventCHWriterRowsSkipped, attr.ReasonKey, "missing_organization_id"))
		require.Equal(t, int64(1), agentEventCount(t, reader, meterAgentEventCHWriterRowsSkipped, attr.ReasonKey, "nil_record"))
	})

	t.Run("it acks a batch with nothing to write without touching ClickHouse", func(t *testing.T) {
		t.Parallel()
		inserter := &captureAgentEventInserter{batches: nil, err: nil}
		writer := NewAgentEventLogCHWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), inserter)

		err := writer.HandleBatch(t.Context(), []*otelv1.LogRecord{nil}, nil)
		require.NoError(t, err)
		require.Empty(t, inserter.batches)
	})

	t.Run("it returns the insert error so the batch is redelivered", func(t *testing.T) {
		t.Parallel()
		inserter := &captureAgentEventInserter{batches: nil, err: errors.New("clickhouse down")}
		reader, meterProvider := readableMeter(t)
		writer := NewAgentEventLogCHWriter(testenv.NewLogger(t), meterProvider, inserter)

		err := writer.HandleBatch(t.Context(), []*otelv1.LogRecord{agentEventTestLog(claudeCodeScopeName, "api_request")}, nil)
		require.ErrorContains(t, err, "clickhouse down")

		// The attempt is still counted, under the failure outcome, so a
		// ClickHouse outage shows up as rows that did not land.
		require.Equal(t, int64(1), agentEventCount(t, reader, meterAgentEventCHWriterRowsInserted, attr.OutcomeKey, string(o11y.OutcomeFailure)))
	})
}

func TestAgentEventSpanCHWriter(t *testing.T) {
	t.Parallel()

	fixedNow := time.Unix(1_724_600_000, 7)

	t.Run("it stamps spans with the consumer clock as their observed time", func(t *testing.T) {
		t.Parallel()
		inserter := &captureAgentEventInserter{batches: nil, err: nil}
		writer := NewAgentEventSpanCHWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), inserter)
		writer.now = func() time.Time { return fixedNow }

		good := spanEventTestSpan("org-1", "litellm")
		poison := spanEventTestSpan("org-1", "litellm")
		poison.SetSpanId(nil)

		err := writer.HandleBatch(t.Context(), []*otelv1.Span{good, poison}, nil)
		require.NoError(t, err)
		require.Len(t, inserter.batches, 1)
		require.Len(t, inserter.batches[0], 1)
		require.Equal(t, fixedNow.UnixNano(), inserter.batches[0][0].ObservedAtUnixNano)
		require.Equal(t, "claude_code.api_request", inserter.batches[0][0].RawEventName)
	})

	t.Run("it returns the insert error so the batch is redelivered", func(t *testing.T) {
		t.Parallel()
		inserter := &captureAgentEventInserter{batches: nil, err: errors.New("clickhouse down")}
		writer := NewAgentEventSpanCHWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), inserter)

		err := writer.HandleBatch(t.Context(), []*otelv1.Span{spanEventTestSpan("org-1", "x")}, nil)
		require.ErrorContains(t, err, "clickhouse down")
	})
}
