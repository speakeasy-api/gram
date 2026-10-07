package enrich

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"go.opentelemetry.io/otel/metric"
)

// meterColumnEnricherMissing counts the records where a column enricher's
// table named the event type but the producer stated no value for the
// column, or one the dialect could not read, by surface, event type and
// column. A producer renaming an attribute shows up here as one column
// going quiet on one type for one surface, the same day. The label is the
// surface the dialect recognised rather than the producer's service name,
// so the series stay bounded.
const meterColumnEnricherMissing = "gram.otel_column_enricher.missing"

// MeterReservedAttributesDropped counts the attributes a transform dropped
// because a producer sent them under Gram's reserved speakeasy.agent
// namespace, by signal. Only the column enrichers may write there; a
// producer that does, by accident or on purpose, is otherwise invisible.
const MeterReservedAttributesDropped = "gram.otel_reserved_attributes_dropped"

// Signal names which kind of record a transform handles, for the metrics
// the transforms share.
type Signal string

const (
	SignalLog  Signal = "log"
	SignalSpan Signal = "span"
)

type Instruments struct {
	logEnricherDuration       metric.Float64Histogram
	metricEnricherDuration    metric.Float64Histogram
	spanEnricherDuration      metric.Float64Histogram
	columnValueMissing        metric.Int64Counter
	reservedAttributesDropped metric.Int64Counter
}

func NewInstruments(logger *slog.Logger, meterProvider metric.MeterProvider) *Instruments {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/otel")

	logEnricherDuration, err := meter.Float64Histogram(
		meterLogEnricherDuration,
		metric.WithDescription("Duration of a single log enricher in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.25, 1, 2, 5),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterLogEnricherDuration), attr.SlogError(err))
	}

	metricEnricherDuration, err := meter.Float64Histogram(
		meterMetricEnricherDuration,
		metric.WithDescription("Duration of a single metric enricher in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.25, 1, 2, 5),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterMetricEnricherDuration), attr.SlogError(err))
	}

	spanEnricherDuration, err := meter.Float64Histogram(
		meterSpanEnricherDuration,
		metric.WithDescription("Duration of a single span enricher in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.25, 1, 2, 5),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterSpanEnricherDuration), attr.SlogError(err))
	}

	columnValueMissing, err := meter.Int64Counter(
		meterColumnEnricherMissing,
		metric.WithDescription("Records where a column enricher's table named the event type but the producer stated no value for the column, or one that could not be read"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterColumnEnricherMissing), attr.SlogError(err))
	}

	reservedAttributesDropped, err := meter.Int64Counter(
		MeterReservedAttributesDropped,
		metric.WithDescription("Attributes a transform dropped because a producer sent them under the reserved speakeasy.agent namespace"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(MeterReservedAttributesDropped), attr.SlogError(err))
	}

	return &Instruments{
		logEnricherDuration:       logEnricherDuration,
		metricEnricherDuration:    metricEnricherDuration,
		spanEnricherDuration:      spanEnricherDuration,
		columnValueMissing:        columnValueMissing,
		reservedAttributesDropped: reservedAttributesDropped,
	}
}

// RecordReservedAttributesDropped counts attributes a transform dropped from
// one record because a producer sent them under the reserved namespace.
func (m *Instruments) RecordReservedAttributesDropped(ctx context.Context, signal Signal, count int) {
	if m.reservedAttributesDropped == nil || count <= 0 {
		return
	}

	m.reservedAttributesDropped.Add(
		ctx,
		int64(count),
		metric.WithAttributes(attr.OTELSignal(signal)),
	)
}

func (m *Instruments) recordColumnValueMissing(ctx context.Context, surface, eventType, column string) {
	if m.columnValueMissing == nil {
		return
	}

	m.columnValueMissing.Add(
		ctx,
		1,
		metric.WithAttributes(
			attr.AgentEventSurface(surface),
			attr.AgentEventType(eventType),
			attr.AgentEventColumn(column),
		),
	)
}

func (m *Instruments) recordLogEnricherDuration(ctx context.Context, enricherName string, duration float64, outcome o11y.Outcome) {
	if m.logEnricherDuration == nil {
		return
	}

	m.logEnricherDuration.Record(
		ctx,
		duration,
		metric.WithAttributes(
			attr.OTELLogEnricherName(enricherName),
			attr.Outcome(outcome),
		),
	)
}

func (m *Instruments) recordMetricEnricherDuration(ctx context.Context, enricherName string, duration float64, outcome o11y.Outcome) {
	if m.metricEnricherDuration == nil {
		return
	}

	m.metricEnricherDuration.Record(
		ctx,
		duration,
		metric.WithAttributes(
			attr.OTELMetricEnricherName(enricherName),
			attr.Outcome(outcome),
		),
	)
}

func (m *Instruments) recordSpanEnricherDuration(ctx context.Context, enricherName string, duration float64, outcome o11y.Outcome) {
	if m.spanEnricherDuration == nil {
		return
	}

	m.spanEnricherDuration.Record(
		ctx,
		duration,
		metric.WithAttributes(
			attr.OTELSpanEnricherName(enricherName),
			attr.Outcome(outcome),
		),
	)
}
