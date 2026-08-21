package otel

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	otelattr "go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const normalizedInstrumentationScopeName = "com.speakeasy.ai.tracing"

type SpanTransformHandler struct {
	logger        *slog.Logger
	metrics       *metrics
	spanPublisher gcp.Publisher[*otelv1.Span]
	enrichers     []SpanEnricher
}

func NewSpanTransformHandler(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	spanPublisher gcp.Publisher[*otelv1.Span],
	db database.DBTX,
	cacheImpl cache.Cache,
) *SpanTransformHandler {
	logger = logger.With(attr.SlogComponent("span-transform-handler"))

	return &SpanTransformHandler{
		logger:        logger,
		metrics:       newMetrics(logger, meterProvider),
		spanPublisher: spanPublisher,
		enrichers: []SpanEnricher{
			&enrichTenancy{},
			NewEnrichSpeakeasyTokens(),
			NewEnrichDirectory(logger, db, cacheImpl),
		},
	}
}

func (h *SpanTransformHandler) Handle(ctx context.Context, m *otelv1.InboundSpan, _ gcp.MessageMetadata) error {
	out, err := spanFromInboundSpan(m)
	if err != nil {
		return fmt.Errorf("convert inbound span: %w", o11y.LogError(ctx, h.logger, err, "failed to convert inbound span"))
	}
	if err := rewriteInstrumentationScope(out); err != nil {
		return fmt.Errorf("rewrite instrumentation scope: %w", err)
	}

	enrichments, err := enrichSpan(ctx, h.metrics, m, h.enrichers)
	if err != nil {
		return fmt.Errorf("enrich span: %w", o11y.LogError(ctx, h.logger, err, "failed to enrich span"))
	}
	if err := applySpanEnrichments(out, enrichments); err != nil {
		return fmt.Errorf("apply span enrichments: %w", o11y.LogError(ctx, h.logger, err, "failed to apply span enrichments"))
	}

	res := h.spanPublisher.Publish(ctx, out)
	_, err = res.Get(ctx)
	if err != nil {
		return fmt.Errorf("publish span: %w", o11y.LogError(ctx, h.logger, err, "failed to publish span"))
	}

	return nil
}

func rewriteInstrumentationScope(span *otelv1.Span) error {
	scope := span.GetScope()
	if scope == nil {
		name := normalizedInstrumentationScopeName
		span.SetScope((&otelv1.Span_InstrumentationScope_builder{Name: &name}).Build())
		return nil
	}

	originalName := scope.GetName()
	if originalName == normalizedInstrumentationScopeName {
		return nil
	}
	scope.SetName(normalizedInstrumentationScopeName)
	if originalName == "" {
		return nil
	}

	return applySpanEnrichments(span, []otelattr.KeyValue{
		OriginalInstrumentationScopeName(originalName),
	})
}

func applySpanEnrichments(out *otelv1.Span, enrichments []otelattr.KeyValue) error {
	if len(enrichments) == 0 {
		return nil
	}

	attributes := slices.Grow(out.GetAttributes(), len(enrichments))
	for _, enrichment := range enrichments {
		value, err := spanAnyValue(enrichment.Value)
		if err != nil {
			return fmt.Errorf("convert enrichment %q: %w", enrichment.Key, err)
		}

		key := string(enrichment.Key)
		attributes = append(attributes, (&otelv1.Span_KeyValue_builder{
			Key:   &key,
			Value: value,
		}).Build())
	}

	out.SetAttributes(attributes)
	return nil
}

func spanAnyValue(value otelattr.Value) (*otelv1.Span_AnyValue, error) {
	switch value.Type() {
	case otelattr.BOOL:
		v := value.AsBool()
		return (&otelv1.Span_AnyValue_builder{BoolValue: &v}).Build(), nil
	case otelattr.INT64:
		v := value.AsInt64()
		return (&otelv1.Span_AnyValue_builder{IntValue: &v}).Build(), nil
	case otelattr.FLOAT64:
		v := value.AsFloat64()
		return (&otelv1.Span_AnyValue_builder{DoubleValue: &v}).Build(), nil
	case otelattr.STRING:
		v := value.AsString()
		return (&otelv1.Span_AnyValue_builder{StringValue: &v}).Build(), nil
	case otelattr.BYTESLICE:
		return (&otelv1.Span_AnyValue_builder{BytesValue: value.AsByteSlice()}).Build(), nil
	case otelattr.BOOLSLICE:
		input := value.AsBoolSlice()
		values := make([]*otelv1.Span_AnyValue, len(input))
		for i, item := range input {
			values[i] = (&otelv1.Span_AnyValue_builder{BoolValue: &item}).Build()
		}
		return (&otelv1.Span_AnyValue_builder{
			ArrayValue: (&otelv1.Span_ArrayValue_builder{Values: values}).Build(),
		}).Build(), nil
	case otelattr.INT64SLICE:
		input := value.AsInt64Slice()
		values := make([]*otelv1.Span_AnyValue, len(input))
		for i, item := range input {
			values[i] = (&otelv1.Span_AnyValue_builder{IntValue: &item}).Build()
		}
		return (&otelv1.Span_AnyValue_builder{
			ArrayValue: (&otelv1.Span_ArrayValue_builder{Values: values}).Build(),
		}).Build(), nil
	case otelattr.FLOAT64SLICE:
		input := value.AsFloat64Slice()
		values := make([]*otelv1.Span_AnyValue, len(input))
		for i, item := range input {
			values[i] = (&otelv1.Span_AnyValue_builder{DoubleValue: &item}).Build()
		}
		return (&otelv1.Span_AnyValue_builder{
			ArrayValue: (&otelv1.Span_ArrayValue_builder{Values: values}).Build(),
		}).Build(), nil
	case otelattr.STRINGSLICE:
		input := value.AsStringSlice()
		values := make([]*otelv1.Span_AnyValue, len(input))
		for i, item := range input {
			values[i] = (&otelv1.Span_AnyValue_builder{StringValue: &item}).Build()
		}
		return (&otelv1.Span_AnyValue_builder{
			ArrayValue: (&otelv1.Span_ArrayValue_builder{Values: values}).Build(),
		}).Build(), nil
	case otelattr.SLICE:
		input := value.AsSlice()
		values := make([]*otelv1.Span_AnyValue, len(input))
		for i, item := range input {
			converted, err := spanAnyValue(item)
			if err != nil {
				return nil, fmt.Errorf("convert slice item %d: %w", i, err)
			}
			values[i] = converted
		}
		return (&otelv1.Span_AnyValue_builder{
			ArrayValue: (&otelv1.Span_ArrayValue_builder{Values: values}).Build(),
		}).Build(), nil
	default:
		return nil, fmt.Errorf("unsupported OpenTelemetry attribute type %v", value.Type())
	}
}

func spanFromInboundSpan(inbound *otelv1.InboundSpan) (*otelv1.Span, error) {
	encoded, err := proto.Marshal(inbound)
	if err != nil {
		return nil, fmt.Errorf("marshal inbound span: %w", err)
	}

	span := &otelv1.Span{}
	if err := proto.Unmarshal(encoded, span); err != nil {
		return nil, fmt.Errorf("unmarshal inbound span as gram.otel.v1.Span: %w", err)
	}

	return span, nil
}
