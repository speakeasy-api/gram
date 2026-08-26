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
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/errgroup"
)

const (
	meterMetricEnricherDuration = "gram.otel_metric_enricher.duration"
	maxMetricRelayExportBytes   = 4 * constants.MiB

	// Metric resource and scope context is flattened into each Pub/Sub message.
	// Reserve enough space for that context and the OTLP export wrappers rebuilt
	// at relay time so every accepted metric fits one destination request.
	metricRelayEnvelopeHeadroom = 256 * constants.KiB
	maxOTLPMetricBytes          = maxMetricRelayExportBytes - metricRelayEnvelopeHeadroom
)

// MetricEnricher derives bounded resource attributes that describe the entity
// producing a metric. Per-user, per-request, and other unbounded values do not
// belong here because resource and data point attributes identify metric
// streams and increase cardinality.
type MetricEnricher interface {
	Name() string
	Enrich(ctx context.Context, metric *otelv1.InboundMetric) ([]attribute.KeyValue, error)
}

func enrichMetric(
	ctx context.Context,
	metrics *metrics,
	item *otelv1.InboundMetric,
	enrichers []MetricEnricher,
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

			enrichedAttrs, err := enricher.Enrich(ctx, item)
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
