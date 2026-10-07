// Package enrich holds every enricher the OTel transform handlers run over an
// inbound record before it is published to the normalized topics, the
// interfaces those handlers run them through, and the attribute keys the
// enrichers write under.
//
// An enricher reads the inbound record and returns attributes to add to it;
// it never removes or rewrites what the producer sent. The handlers run every
// enricher for a signal concurrently and append what they return in
// registration order, so an enricher must not depend on another's output.
// Returning an error fails the record, which is then redelivered, so an
// enricher returns one only for a real failure and never for a value the
// producer did not state.
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

// MetricEnricher derives bounded resource attributes that describe the entity
// producing a metric. Per-user, per-request, and other unbounded values do not
// belong here because resource and data point attributes identify metric
// streams and increase cardinality.
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

// named is what every enricher kind has in common for the runner.
type named interface {
	Name() string
}

// run is the one runner behind Log, Span and Metric: the enrichers run
// concurrently, bounded by the CPU count, each one's duration and outcome is
// recorded, a panic in one is turned into its error, and the results are
// concatenated in registration order so the output is deterministic.
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
	if err := group.Wait(); err != nil {
		return nil, fmt.Errorf("wait for %s enrichers: %w", signal, err)
	}

	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("enrich %s: %w", signal, err)
	}

	var finalAttrs []attribute.KeyValue
	for _, attrs := range allAttrs {
		finalAttrs = append(finalAttrs, attrs...)
	}

	return finalAttrs, nil
}
