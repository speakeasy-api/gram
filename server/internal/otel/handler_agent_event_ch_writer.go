package otel

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/otel/chrepo"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

const (
	meterAgentEventCHWriterRowsSkipped  = "gram.agent_event_ch_writer.rows_skipped"
	meterAgentEventCHWriterRowsInserted = "gram.agent_event_ch_writer.rows_inserted"
)

// AgentEventInserter writes a batch of agent_events rows to ClickHouse.
// *chrepo.Queries satisfies it; tests supply a fake.
type AgentEventInserter interface {
	InsertAgentEvents(ctx context.Context, rows []chrepo.AgentEventRow) error
}

// agentEventCHWriter is the part of the agent_events writer that does not
// depend on which signal the batch carries: it turns rows into an insert,
// counts what happened, and decides what a failure means for the batch.
//
// A record the projection cannot handle is a poison record: redelivery cannot
// fix it, so it is logged, counted and acknowledged. A ClickHouse failure is
// returned so the whole batch is redelivered. agent_events is append-only, so
// a redelivered batch lands as duplicate rows that readers collapse on
// record_id.
type agentEventCHWriter struct {
	logger       *slog.Logger
	inserter     AgentEventInserter
	now          func() time.Time
	rowsSkipped  metric.Int64Counter
	rowsInserted metric.Int64Counter
}

func newAgentEventCHWriter(logger *slog.Logger, meterProvider metric.MeterProvider, inserter AgentEventInserter, component string) *agentEventCHWriter {
	logger = logger.With(attr.SlogComponent(component))
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/otel")
	rowsSkipped, err := meter.Int64Counter(
		meterAgentEventCHWriterRowsSkipped,
		metric.WithDescription("Normalized OTEL records dropped by the agent_events writer as unprocessable"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterAgentEventCHWriterRowsSkipped), attr.SlogError(err))
	}
	rowsInserted, err := meter.Int64Counter(
		meterAgentEventCHWriterRowsInserted,
		metric.WithDescription("agent_events rows the writer attempted to insert"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(meterAgentEventCHWriterRowsInserted), attr.SlogError(err))
	}

	return &agentEventCHWriter{
		logger:       logger,
		inserter:     inserter,
		now:          time.Now,
		rowsSkipped:  rowsSkipped,
		rowsInserted: rowsInserted,
	}
}

func (w *agentEventCHWriter) skip(ctx context.Context, reason, id string) {
	w.logger.ErrorContext(ctx, "skipping unprocessable record for agent_events",
		attr.SlogReason(reason),
		attr.SlogValueString(id),
	)
	if w.rowsSkipped != nil {
		w.rowsSkipped.Add(ctx, 1, metric.WithAttributes(attr.Reason(reason)))
	}
}

func (w *agentEventCHWriter) write(ctx context.Context, rows []chrepo.AgentEventRow) error {
	if len(rows) == 0 {
		return nil
	}

	err := w.inserter.InsertAgentEvents(ctx, rows)
	if w.rowsInserted != nil {
		w.rowsInserted.Add(ctx, int64(len(rows)), metric.WithAttributes(attr.Outcome(o11y.OutcomeFromError(err))))
	}
	if err != nil {
		return fmt.Errorf("insert agent_events: %w", err)
	}
	return nil
}

// AgentEventLogCHWriter consumes normalized gram.otel.v1.LogRecord messages
// and writes them to agent_events.
type AgentEventLogCHWriter struct {
	*agentEventCHWriter
}

func NewAgentEventLogCHWriter(logger *slog.Logger, meterProvider metric.MeterProvider, inserter AgentEventInserter) *AgentEventLogCHWriter {
	return &AgentEventLogCHWriter{agentEventCHWriter: newAgentEventCHWriter(logger, meterProvider, inserter, "agent-event-log-ch-writer")}
}

var _ streams.BatchHandler[*otelv1.LogRecord] = (*AgentEventLogCHWriter)(nil)

func (w *AgentEventLogCHWriter) HandleBatch(ctx context.Context, messages []*otelv1.LogRecord, _ []gcp.MessageMetadata) error {
	observedAt := w.now().UnixNano()
	rows := make([]chrepo.AgentEventRow, 0, len(messages))
	for _, message := range messages {
		row, skipReason := agentEventRowFromLog(message, observedAt)
		if skipReason != "" {
			w.skip(ctx, skipReason, message.GetRecordId())
			continue
		}
		rows = append(rows, row)
	}
	return w.write(ctx, rows)
}

// AgentEventSpanCHWriter consumes normalized gram.otel.v1.Span messages and
// writes them to agent_events.
type AgentEventSpanCHWriter struct {
	*agentEventCHWriter
}

func NewAgentEventSpanCHWriter(logger *slog.Logger, meterProvider metric.MeterProvider, inserter AgentEventInserter) *AgentEventSpanCHWriter {
	return &AgentEventSpanCHWriter{agentEventCHWriter: newAgentEventCHWriter(logger, meterProvider, inserter, "agent-event-span-ch-writer")}
}

var _ streams.BatchHandler[*otelv1.Span] = (*AgentEventSpanCHWriter)(nil)

func (w *AgentEventSpanCHWriter) HandleBatch(ctx context.Context, messages []*otelv1.Span, _ []gcp.MessageMetadata) error {
	observedAt := w.now().UnixNano()
	rows := make([]chrepo.AgentEventRow, 0, len(messages))
	for _, message := range messages {
		row, skipReason := agentEventRowFromSpan(message, observedAt)
		if skipReason != "" {
			w.skip(ctx, skipReason, hexEventID(message.GetSpanId()))
			continue
		}
		rows = append(rows, row)
	}
	return w.write(ctx, rows)
}
