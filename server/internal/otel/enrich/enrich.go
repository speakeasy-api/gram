// Package enrich holds the enrichers the OTel transform handlers run over an
// inbound record before it is published to the normalized topics, and the
// attribute keys they write under.
//
// An enricher returns attributes to add and never rewrites what the producer
// sent. Enrichers run concurrently and must not depend on each other's
// output. An error fails the record for redelivery, so an enricher returns
// one only for a real failure, never for a value the producer did not state.
package enrich

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/errgroup"
)

const (
	meterLogEnricherDuration    = "gram.otel_log_enricher.duration"
	meterSpanEnricherDuration   = "gram.otel_span_enricher.duration"
	meterMetricEnricherDuration = "gram.otel_metric_enricher.duration"
)

// LogEnricher adds attributes to an inbound log record.
type LogEnricher interface {
	Name() string
	Enrich(ctx context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error)
}

// SpanEnricher adds attributes to an inbound span.
type SpanEnricher interface {
	Name() string
	Enrich(ctx context.Context, span *otelv1.InboundSpan) ([]attribute.KeyValue, error)
}

// MetricEnricher derives bounded resource attributes for a metric; unbounded
// values would multiply metric streams.
type MetricEnricher interface {
	Name() string
	Enrich(ctx context.Context, metric *otelv1.InboundMetric, metricDialect dialect.MetricDialect) ([]attribute.KeyValue, error)
}

// Log runs every log enricher over one record and returns what they wrote,
// in registration order. Any enricher failing fails the record.
func Log(
	ctx context.Context,
	instruments *Instruments,
	record *otelv1.InboundLogRecord,
	enrichers []LogEnricher,
) ([]attribute.KeyValue, error) {
	return run(ctx, "log", enrichers, func(enricher LogEnricher) ([]attribute.KeyValue, error) {
		return enricher.Enrich(ctx, record)
	}, func(ctx context.Context, name string, seconds float64, outcome o11y.Outcome) {
		instruments.recordLogEnricherDuration(ctx, name, seconds, outcome)
	})
}

// Span runs every span enricher over one span and returns what they wrote,
// in registration order. Any enricher failing fails the span.
func Span(
	ctx context.Context,
	instruments *Instruments,
	span *otelv1.InboundSpan,
	enrichers []SpanEnricher,
) ([]attribute.KeyValue, error) {
	return run(ctx, "span", enrichers, func(enricher SpanEnricher) ([]attribute.KeyValue, error) {
		return enricher.Enrich(ctx, span)
	}, func(ctx context.Context, name string, seconds float64, outcome o11y.Outcome) {
		instruments.recordSpanEnricherDuration(ctx, name, seconds, outcome)
	})
}

// Metric runs every metric enricher over one metric and returns what they
// wrote, in registration order. Any enricher failing fails the metric.
func Metric(
	ctx context.Context,
	instruments *Instruments,
	item *otelv1.InboundMetric,
	enrichers []MetricEnricher,
) ([]attribute.KeyValue, error) {
	if len(enrichers) == 0 {
		return nil, nil
	}

	metricDialect := dialect.ForMetric(item)
	return run(ctx, "metric", enrichers, func(enricher MetricEnricher) ([]attribute.KeyValue, error) {
		return enricher.Enrich(ctx, item, metricDialect)
	}, func(ctx context.Context, name string, seconds float64, outcome o11y.Outcome) {
		instruments.recordMetricEnricherDuration(ctx, name, seconds, outcome)
	})
}

type named interface {
	Name() string
}

// run executes the enrichers concurrently, records each one's duration and
// outcome, turns a panic into that enricher's error, and concatenates the
// results in registration order.
func run[E named](
	ctx context.Context,
	signal string,
	enrichers []E,
	enrich func(E) ([]attribute.KeyValue, error),
	record func(context.Context, string, float64, o11y.Outcome),
) ([]attribute.KeyValue, error) {
	group := new(errgroup.Group)
	group.SetLimit(runtime.NumCPU())

	errs := make([]error, len(enrichers))
	allAttrs := make([][]attribute.KeyValue, len(enrichers))

	for i, enricher := range enrichers {
		group.Go(func() error {
			var outcomeErr error
			defer func(start time.Time) {
				if recovered := recover(); recovered != nil {
					outcomeErr = fmt.Errorf("panic in %s enricher %s: %v", signal, enricher.Name(), recovered)
					errs[i] = outcomeErr
				}

				record(
					context.WithoutCancel(ctx),
					enricher.Name(),
					time.Since(start).Seconds(),
					o11y.OutcomeFromError(outcomeErr),
				)
			}(time.Now())

			enrichedAttrs, err := enrich(enricher)
			if err != nil {
				outcomeErr = fmt.Errorf("%s: %w", enricher.Name(), err)
				errs[i] = outcomeErr
			}

			allAttrs[i] = enrichedAttrs
			return nil
		})
	}
	// Every goroutine returns nil and records its failure in errs.
	_ = group.Wait()

	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("enrich %s: %w", signal, err)
	}

	var finalAttrs []attribute.KeyValue
	for _, attrs := range allAttrs {
		finalAttrs = append(finalAttrs, attrs...)
	}

	return finalAttrs, nil
}
