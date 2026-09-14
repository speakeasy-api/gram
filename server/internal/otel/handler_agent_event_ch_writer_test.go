package otel

import (
	"context"
	"errors"
	"testing"
	"time"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/chrepo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

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
		writer := NewAgentEventLogCHWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), inserter)
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
		writer := NewAgentEventLogCHWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), inserter)

		err := writer.HandleBatch(t.Context(), []*otelv1.LogRecord{agentEventTestLog(claudeCodeScopeName, "api_request")}, nil)
		require.ErrorContains(t, err, "clickhouse down")
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
