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

const normalizedLogInstrumentationScopeName = "com.speakeasy.ai.logging"

type LogTransformHandler struct {
	logger       *slog.Logger
	metrics      *metrics
	logPublisher gcp.Publisher[*otelv1.LogRecord]
	enrichers    []LogEnricher
}

func NewLogTransformHandler(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	logPublisher gcp.Publisher[*otelv1.LogRecord],
	replicaDB database.DBTX,
	cacheImpl cache.Cache,
) *LogTransformHandler {
	logger = logger.With(attr.SlogComponent("log-transform-handler"))

	return &LogTransformHandler{
		logger:       logger,
		metrics:      newMetrics(logger, meterProvider),
		logPublisher: logPublisher,
		enrichers: []LogEnricher{
			&enrichLogTenancy{},
			newEnrichLogSpeakeasyTokens(),
			newEnrichLogDirectory(logger, replicaDB, cacheImpl),
		},
	}
}

func (h *LogTransformHandler) Handle(ctx context.Context, record *otelv1.InboundLogRecord, _ gcp.MessageMetadata) error {
	out, err := logRecordFromInbound(record)
	if err != nil {
		return fmt.Errorf("convert inbound log record: %w", o11y.LogError(ctx, h.logger, err, "failed to convert inbound log record"))
	}
	if err := rewriteLogInstrumentationScope(out); err != nil {
		return fmt.Errorf("rewrite instrumentation scope: %w", err)
	}

	enrichments, err := enrichLog(ctx, h.metrics, record, h.enrichers)
	if err != nil {
		return fmt.Errorf("enrich log record: %w", o11y.LogError(ctx, h.logger, err, "failed to enrich log record"))
	}
	if err := applyLogEnrichments(out, enrichments); err != nil {
		return fmt.Errorf("apply log enrichments: %w", o11y.LogError(ctx, h.logger, err, "failed to apply log enrichments"))
	}

	result := h.logPublisher.Publish(ctx, out)
	if _, err := result.Get(ctx); err != nil {
		return fmt.Errorf("publish log record: %w", o11y.LogError(ctx, h.logger, err, "failed to publish log record"))
	}

	return nil
}

func rewriteLogInstrumentationScope(record *otelv1.LogRecord) error {
	scope := record.GetScope()
	if scope == nil {
		name := normalizedLogInstrumentationScopeName
		record.SetScope((&otelv1.LogRecord_InstrumentationScope_builder{Name: &name}).Build())
		return nil
	}

	originalName := scope.GetName()
	if originalName == normalizedLogInstrumentationScopeName {
		return nil
	}
	scope.SetName(normalizedLogInstrumentationScopeName)
	if originalName == "" {
		return nil
	}

	return applyLogEnrichments(record, []otelattr.KeyValue{
		OriginalInstrumentationScopeName(originalName),
	})
}

func applyLogEnrichments(out *otelv1.LogRecord, enrichments []otelattr.KeyValue) error {
	if len(enrichments) == 0 {
		return nil
	}

	attributes := slices.Grow(out.GetAttributes(), len(enrichments))
	for _, enrichment := range enrichments {
		value, err := logAnyValue(enrichment.Value)
		if err != nil {
			return fmt.Errorf("convert enrichment %q: %w", enrichment.Key, err)
		}

		key := string(enrichment.Key)
		attributes = append(attributes, (&otelv1.LogRecord_KeyValue_builder{
			Key:   &key,
			Value: value,
		}).Build())
	}

	out.SetAttributes(attributes)
	return nil
}

func logAnyValue(value otelattr.Value) (*otelv1.LogRecord_AnyValue, error) {
	switch value.Type() {
	case otelattr.BOOL:
		v := value.AsBool()
		return (&otelv1.LogRecord_AnyValue_builder{BoolValue: &v}).Build(), nil
	case otelattr.INT64:
		v := value.AsInt64()
		return (&otelv1.LogRecord_AnyValue_builder{IntValue: &v}).Build(), nil
	case otelattr.FLOAT64:
		v := value.AsFloat64()
		return (&otelv1.LogRecord_AnyValue_builder{DoubleValue: &v}).Build(), nil
	case otelattr.STRING:
		v := value.AsString()
		return (&otelv1.LogRecord_AnyValue_builder{StringValue: &v}).Build(), nil
	case otelattr.BYTESLICE:
		return (&otelv1.LogRecord_AnyValue_builder{BytesValue: value.AsByteSlice()}).Build(), nil
	case otelattr.BOOLSLICE:
		input := value.AsBoolSlice()
		values := make([]*otelv1.LogRecord_AnyValue, len(input))
		for i, item := range input {
			values[i] = (&otelv1.LogRecord_AnyValue_builder{BoolValue: &item}).Build()
		}
		return (&otelv1.LogRecord_AnyValue_builder{
			ArrayValue: (&otelv1.LogRecord_ArrayValue_builder{Values: values}).Build(),
		}).Build(), nil
	case otelattr.INT64SLICE:
		input := value.AsInt64Slice()
		values := make([]*otelv1.LogRecord_AnyValue, len(input))
		for i, item := range input {
			values[i] = (&otelv1.LogRecord_AnyValue_builder{IntValue: &item}).Build()
		}
		return (&otelv1.LogRecord_AnyValue_builder{
			ArrayValue: (&otelv1.LogRecord_ArrayValue_builder{Values: values}).Build(),
		}).Build(), nil
	case otelattr.FLOAT64SLICE:
		input := value.AsFloat64Slice()
		values := make([]*otelv1.LogRecord_AnyValue, len(input))
		for i, item := range input {
			values[i] = (&otelv1.LogRecord_AnyValue_builder{DoubleValue: &item}).Build()
		}
		return (&otelv1.LogRecord_AnyValue_builder{
			ArrayValue: (&otelv1.LogRecord_ArrayValue_builder{Values: values}).Build(),
		}).Build(), nil
	case otelattr.STRINGSLICE:
		input := value.AsStringSlice()
		values := make([]*otelv1.LogRecord_AnyValue, len(input))
		for i, item := range input {
			values[i] = (&otelv1.LogRecord_AnyValue_builder{StringValue: &item}).Build()
		}
		return (&otelv1.LogRecord_AnyValue_builder{
			ArrayValue: (&otelv1.LogRecord_ArrayValue_builder{Values: values}).Build(),
		}).Build(), nil
	case otelattr.SLICE:
		input := value.AsSlice()
		values := make([]*otelv1.LogRecord_AnyValue, len(input))
		for i, item := range input {
			converted, err := logAnyValue(item)
			if err != nil {
				return nil, fmt.Errorf("convert slice item %d: %w", i, err)
			}
			values[i] = converted
		}
		return (&otelv1.LogRecord_AnyValue_builder{
			ArrayValue: (&otelv1.LogRecord_ArrayValue_builder{Values: values}).Build(),
		}).Build(), nil
	case otelattr.EMPTY:
		return (&otelv1.LogRecord_AnyValue_builder{}).Build(), nil
	}
	return nil, fmt.Errorf("unsupported OpenTelemetry attribute type %v", value.Type())
}

func logRecordFromInbound(inbound *otelv1.InboundLogRecord) (*otelv1.LogRecord, error) {
	encoded, err := proto.Marshal(inbound)
	if err != nil {
		return nil, fmt.Errorf("marshal inbound log record: %w", err)
	}

	record := &otelv1.LogRecord{}
	if err := proto.Unmarshal(encoded, record); err != nil {
		return nil, fmt.Errorf("unmarshal inbound log record as gram.otel.v1.LogRecord: %w", err)
	}

	return record, nil
}
