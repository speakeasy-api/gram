package otel

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/errgroup"
)

const meterLogEnricherDuration = "gram.otel_log_enricher.duration"

type LogEnricher interface {
	Name() string
	Enrich(ctx context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error)
}

func enrichLog(
	ctx context.Context,
	metrics *metrics,
	record *otelv1.InboundLogRecord,
	enrichers []LogEnricher,
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
					outcomeErr = fmt.Errorf("panic in log enricher %s: %v", enricher.Name(), recovered)
					errs[i] = outcomeErr
				}

				metrics.recordLogEnricherDuration(
					context.WithoutCancel(ctx),
					enricher.Name(),
					time.Since(start).Seconds(),
					o11y.OutcomeFromError(outcomeErr),
				)
			}(time.Now())

			enrichedAttrs, err := enricher.Enrich(ctx, record)
			if err != nil {
				outcomeErr = fmt.Errorf("%s: %w", enricher.Name(), err)
				errs[i] = outcomeErr
			}

			allAttrs[i] = enrichedAttrs
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, fmt.Errorf("wait for log enrichers: %w", err)
	}

	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("enrich log: %w", err)
	}

	var finalAttrs []attribute.KeyValue
	for _, attrs := range allAttrs {
		finalAttrs = append(finalAttrs, attrs...)
	}

	return finalAttrs, nil
}
