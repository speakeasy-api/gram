package otel

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/errgroup"
)

const (
	meterMetricEnricherDuration = "gram.otel_metric_enricher.duration"
	maxMetricRelayExportBytes   = 512 * constants.KiB

	// Keep destination exports within Datadog's 512 KiB compressed intake
	// limit even though the relay sends uncompressed protobuf. Reserve space
	// for resource context, future bounded enrichments, and OTLP wrappers.
	metricRelayEnvelopeHeadroom = 64 * constants.KiB
	maxOTLPMetricBytes          = maxMetricRelayExportBytes - metricRelayEnvelopeHeadroom
)

// MetricEnricher derives bounded resource attributes that describe the entity
// producing a metric. Per-user, per-request, and other unbounded values do not
// belong here because resource and data point attributes identify metric
// streams and increase cardinality.
type MetricEnricher interface {
	Name() string
	Enrich(ctx context.Context, metric *otelv1.InboundMetric, metricDialect dialect.MetricDialect) ([]attribute.KeyValue, error)
}

func enrichMetric(
	ctx context.Context,
	metrics *metrics,
	item *otelv1.InboundMetric,
	enrichers []MetricEnricher,
) ([]attribute.KeyValue, error) {
	if len(enrichers) == 0 {
		return nil, nil
	}

	metricDialect := dialect.ForMetric(item)
	group := new(errgroup.Group)
	group.SetLimit(runtime.NumCPU())

	errs := make([]error, len(enrichers))
	allAttrs := make([][]attribute.KeyValue, len(enrichers))

	for i, enricher := range enrichers {
		group.Go(func() error {
			var outcomeErr error
			defer func(start time.Time) {
				if recovered := recover(); recovered != nil {
					outcomeErr = fmt.Errorf("panic in metric enricher %s: %v", enricher.Name(), recovered)
					errs[i] = outcomeErr
				}

				metrics.recordMetricEnricherDuration(
					context.WithoutCancel(ctx),
					enricher.Name(),
					time.Since(start).Seconds(),
					o11y.OutcomeFromError(outcomeErr),
				)
			}(time.Now())

			enrichedAttrs, err := enricher.Enrich(ctx, item, metricDialect)
			if err != nil {
				outcomeErr = fmt.Errorf("%s: %w", enricher.Name(), err)
				errs[i] = outcomeErr
			}

			allAttrs[i] = enrichedAttrs
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, fmt.Errorf("wait for metric enrichers: %w", err)
	}

	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("enrich metric: %w", err)
	}

	var finalAttrs []attribute.KeyValue
	for _, attrs := range allAttrs {
		finalAttrs = append(finalAttrs, attrs...)
	}

	return finalAttrs, nil
}
