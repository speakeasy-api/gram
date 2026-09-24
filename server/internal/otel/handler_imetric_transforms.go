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
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type MetricTransformHandler struct {
	logger          *slog.Logger
	metrics         *metrics
	metricPublisher gcp.Publisher[*otelv1.Metric]
	enrichers       []MetricEnricher
}

func NewMetricTransformHandler(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	metricPublisher gcp.Publisher[*otelv1.Metric],
) *MetricTransformHandler {
	logger = logger.With(attr.SlogComponent("metric-transform-handler"))

	return &MetricTransformHandler{
		logger:          logger,
		metrics:         newMetrics(logger, meterProvider),
		metricPublisher: metricPublisher,
		enrichers:       nil,
	}
}

func (h *MetricTransformHandler) Handle(ctx context.Context, item *otelv1.InboundMetric, _ gcp.MessageMetadata) error {
	out, err := metricFromInbound(item)
	if err != nil {
		return fmt.Errorf("convert inbound metric: %w", o11y.LogError(ctx, h.logger, err, "failed to convert inbound metric"))
	}

	enrichments, err := enrichMetric(ctx, h.metrics, item, h.enrichers)
	if err != nil {
		return fmt.Errorf("enrich metric: %w", o11y.LogError(ctx, h.logger, err, "failed to enrich metric"))
	}
	if err := applyMetricResourceEnrichments(out, enrichments); err != nil {
		return fmt.Errorf("apply metric enrichments: %w", o11y.LogError(ctx, h.logger, err, "failed to apply metric enrichments"))
	}

	result := h.metricPublisher.Publish(ctx, out)
	if _, err := result.Get(ctx); err != nil {
		return fmt.Errorf("publish metric: %w", o11y.LogError(ctx, h.logger, err, "failed to publish metric"))
	}

	return nil
}

func applyMetricResourceEnrichments(out *otelv1.Metric, enrichments []otelattr.KeyValue) error {
	if len(enrichments) == 0 {
		return nil
	}

	resource := out.GetResource()
	if resource == nil {
		resource = (&otelv1.Metric_Resource_builder{Attributes: nil}).Build()
		out.SetResource(resource)
	}
	attributes := slices.Grow(resource.GetAttributes(), len(enrichments))
	for _, enrichment := range enrichments {
		value, err := metricAnyValue(enrichment.Value)
		if err != nil {
			return fmt.Errorf("convert enrichment %q: %w", enrichment.Key, err)
		}

		key := string(enrichment.Key)
		attributes = append(attributes, (&otelv1.Metric_KeyValue_builder{
			Key:   &key,
			Value: value,
		}).Build())
	}

	resource.SetAttributes(attributes)
	return nil
}

func metricAnyValue(value otelattr.Value) (*otelv1.Metric_AnyValue, error) {
	switch value.Type() {
	case otelattr.BOOL:
		v := value.AsBool()
		return (&otelv1.Metric_AnyValue_builder{BoolValue: &v}).Build(), nil
	case otelattr.INT64:
		v := value.AsInt64()
		return (&otelv1.Metric_AnyValue_builder{IntValue: &v}).Build(), nil
	case otelattr.FLOAT64:
		v := value.AsFloat64()
		return (&otelv1.Metric_AnyValue_builder{DoubleValue: &v}).Build(), nil
	case otelattr.STRING:
		v := value.AsString()
		return (&otelv1.Metric_AnyValue_builder{StringValue: &v}).Build(), nil
	case otelattr.BYTESLICE:
		return (&otelv1.Metric_AnyValue_builder{BytesValue: value.AsByteSlice()}).Build(), nil
	case otelattr.BOOLSLICE:
		input := value.AsBoolSlice()
		values := make([]*otelv1.Metric_AnyValue, len(input))
		for i, item := range input {
			values[i] = (&otelv1.Metric_AnyValue_builder{BoolValue: &item}).Build()
		}
		return (&otelv1.Metric_AnyValue_builder{
			ArrayValue: (&otelv1.Metric_ArrayValue_builder{Values: values}).Build(),
		}).Build(), nil
	case otelattr.INT64SLICE:
		input := value.AsInt64Slice()
		values := make([]*otelv1.Metric_AnyValue, len(input))
		for i, item := range input {
			values[i] = (&otelv1.Metric_AnyValue_builder{IntValue: &item}).Build()
		}
		return (&otelv1.Metric_AnyValue_builder{
			ArrayValue: (&otelv1.Metric_ArrayValue_builder{Values: values}).Build(),
		}).Build(), nil
	case otelattr.FLOAT64SLICE:
		input := value.AsFloat64Slice()
		values := make([]*otelv1.Metric_AnyValue, len(input))
		for i, item := range input {
			values[i] = (&otelv1.Metric_AnyValue_builder{DoubleValue: &item}).Build()
		}
		return (&otelv1.Metric_AnyValue_builder{
			ArrayValue: (&otelv1.Metric_ArrayValue_builder{Values: values}).Build(),
		}).Build(), nil
	case otelattr.STRINGSLICE:
		input := value.AsStringSlice()
		values := make([]*otelv1.Metric_AnyValue, len(input))
		for i, item := range input {
			values[i] = (&otelv1.Metric_AnyValue_builder{StringValue: &item}).Build()
		}
		return (&otelv1.Metric_AnyValue_builder{
			ArrayValue: (&otelv1.Metric_ArrayValue_builder{Values: values}).Build(),
		}).Build(), nil
	case otelattr.SLICE:
		input := value.AsSlice()
		values := make([]*otelv1.Metric_AnyValue, len(input))
		for i, item := range input {
			converted, err := metricAnyValue(item)
			if err != nil {
				return nil, fmt.Errorf("convert slice item %d: %w", i, err)
			}
			values[i] = converted
		}
		return (&otelv1.Metric_AnyValue_builder{
			ArrayValue: (&otelv1.Metric_ArrayValue_builder{Values: values}).Build(),
		}).Build(), nil
	case otelattr.EMPTY:
		return (&otelv1.Metric_AnyValue_builder{}).Build(), nil
	}
	return nil, fmt.Errorf("unsupported OpenTelemetry attribute type %v", value.Type())
}

func metricFromInbound(inbound *otelv1.InboundMetric) (*otelv1.Metric, error) {
	encoded, err := proto.Marshal(inbound)
	if err != nil {
		return nil, fmt.Errorf("marshal inbound metric: %w", err)
	}

	item := new(otelv1.Metric)
	if err := proto.Unmarshal(encoded, item); err != nil {
		return nil, fmt.Errorf("unmarshal inbound metric as gram.otel.v1.Metric: %w", err)
	}

	return item, nil
}
