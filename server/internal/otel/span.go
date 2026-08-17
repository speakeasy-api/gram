package otel

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/errgroup"
)

const meterSpanEnricherDuration = "gram.otel_span_enricher.duration"

type SpanEnricher interface {
	Name() string
	Enrich(ctx context.Context, span *otelv1.InboundSpan) ([]attribute.KeyValue, error)
}

func enrichSpan(
	ctx context.Context,
	metrics *metrics,
	span *otelv1.InboundSpan,
	enrichers []SpanEnricher,
) ([]attribute.KeyValue, error) {
	g := new(errgroup.Group)
	g.SetLimit(runtime.NumCPU())

	errs := make([]error, len(enrichers))
	allAttrs := make([][]attribute.KeyValue, len(enrichers))

	for i, enricher := range enrichers {
		g.Go(func() error {
			var outcomeErr error
			defer func(start time.Time) {
				if r := recover(); r != nil {
					outcomeErr = fmt.Errorf("panic in span enricher %s: %v", enricher.Name(), r)
					errs[i] = outcomeErr
				}

				duration := time.Since(start).Seconds()
				metrics.recordEnricherDuration(
					context.WithoutCancel(ctx),
					enricher.Name(),
					duration,
					o11y.OutcomeFromError(outcomeErr),
				)
			}(time.Now())

			enrichedAttrs, err := enricher.Enrich(ctx, span)
			if err != nil {
				outcomeErr = fmt.Errorf("%s: %w", enricher.Name(), err)
				errs[i] = outcomeErr
			}

			allAttrs[i] = enrichedAttrs
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("wait for span enrichers: %w", err)
	}

	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("enrich span: %w", err)
	}

	var finalAttrs []attribute.KeyValue
	for _, attrs := range allAttrs {
		finalAttrs = append(finalAttrs, attrs...)
	}

	return finalAttrs, nil
}

type spanLike interface {
	GetTraceId() []byte
	GetSpanId() []byte
	GetName() string
	GetStartTimeUnixNano() uint64
	GetEndTimeUnixNano() uint64
}

func validateSpan(span spanLike) error {
	if span == nil {
		return oops.E(oops.CodeBadRequest, nil, "span is nil")
	}
	if len(span.GetTraceId()) == 0 {
		return oops.E(oops.CodeBadRequest, nil, "span trace_id is empty")
	}
	if len(span.GetSpanId()) == 0 {
		return oops.E(oops.CodeBadRequest, nil, "span span_id is empty")
	}
	if len(span.GetName()) == 0 || strings.TrimSpace(span.GetName()) == "" {
		return oops.E(oops.CodeBadRequest, nil, "span name is empty")
	}
	if span.GetStartTimeUnixNano() == 0 {
		return oops.E(oops.CodeBadRequest, nil, "span start_time_unix_nano is zero")
	}
	if span.GetEndTimeUnixNano() == 0 {
		return oops.E(oops.CodeBadRequest, nil, "span end_time_unix_nano is zero")
	}
	if span.GetEndTimeUnixNano() < span.GetStartTimeUnixNano() {
		return oops.E(oops.CodeBadRequest, nil, "span end_time_unix_nano is before start_time_unix_nano")
	}
	return nil
}
