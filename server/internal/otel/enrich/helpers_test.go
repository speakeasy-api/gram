package enrich

import (
	"context"
	"maps"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const (
	claudeCodeScopeName = "com.anthropic.claude_code.events"
	codexScopeName      = "codex_otel.log_only"
)

func logStringAttribute(key, value string) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &value}).Build(),
	}).Build()
}

func inboundTestIntAttribute(key string, value int64) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{IntValue: &value}).Build(),
	}).Build()
}

func inboundTestDoubleAttribute(key string, value float64) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{DoubleValue: &value}).Build(),
	}).Build()
}

func inboundTestBoolAttribute(key string, value bool) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{BoolValue: &value}).Build(),
	}).Build()
}

// inboundTestLog builds an inbound log record as a producer sends it: its
// own scope, its resource's service name, and its attributes, with tenancy
// stamped by the ingest edge.
func inboundTestLog(scope, serviceName, eventName string, attributes ...*otelv1.InboundLogRecord_KeyValue) *otelv1.InboundLogRecord {
	var resourceAttributes []*otelv1.InboundLogRecord_KeyValue
	if serviceName != "" {
		resourceAttributes = append(resourceAttributes, logStringAttribute("service.name", serviceName))
	}
	builder := &otelv1.InboundLogRecord_builder{
		RecordId: new("record-1"),
		Scope:    (&otelv1.InboundLogRecord_InstrumentationScope_builder{Name: &scope}).Build(),
		Resource: (&otelv1.InboundLogRecord_Resource_builder{Attributes: resourceAttributes}).Build(),
		Provenance: (&otelv1.InboundLogRecord_Provenance_builder{
			Source:         new("speakeasy"),
			OrganizationId: new("org-1"),
			ProjectId:      new("project-1"),
		}).Build(),
		Attributes: attributes,
	}
	if eventName != "" {
		builder.EventName = &eventName
	}
	return builder.Build()
}

// inboundTestSpan builds an inbound span as a producer sends it: its own
// scope, its resource's service name, its name, status and attributes, with
// tenancy stamped by the ingest edge. It lasts 500 nanoseconds.
func inboundTestSpan(scope, serviceName, name string, status otelv1.InboundSpan_StatusCode, attributes ...*otelv1.InboundSpan_KeyValue) *otelv1.InboundSpan {
	var resourceAttributes []*otelv1.InboundSpan_KeyValue
	if serviceName != "" {
		resourceAttributes = append(resourceAttributes, spanStringAttribute("service.name", serviceName))
	}
	return (&otelv1.InboundSpan_builder{
		TraceId:           []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanId:            []byte{1, 2, 3, 4, 5, 6, 7, 8},
		Name:              &name,
		StartTimeUnixNano: new(uint64(1_724_500_000_000_000_001)),
		EndTimeUnixNano:   new(uint64(1_724_500_000_000_000_501)),
		Scope:             (&otelv1.InboundSpan_InstrumentationScope_builder{Name: &scope}).Build(),
		Resource:          (&otelv1.InboundSpan_Resource_builder{Attributes: resourceAttributes}).Build(),
		Status:            (&otelv1.InboundSpan_Status_builder{Code: status.Enum(), Message: new("boom")}).Build(),
		Provenance: (&otelv1.InboundSpan_Provenance_builder{
			Source:         new("speakeasy"),
			OrganizationId: new("org-1"),
			ProjectId:      new("project-1"),
		}).Build(),
		Attributes: attributes,
	}).Build()
}

// enriched runs one log enricher and indexes what it wrote by key.
func enriched(t *testing.T, enricher LogEnricher, record *otelv1.InboundLogRecord) map[attribute.Key]attribute.Value {
	t.Helper()
	out, err := enricher.Enrich(t.Context(), record)
	require.NoError(t, err)
	return indexed(out)
}

// enrichedSpan runs one span enricher and indexes what it wrote by key.
func enrichedSpan(t *testing.T, enricher SpanEnricher, span *otelv1.InboundSpan) map[attribute.Key]attribute.Value {
	t.Helper()
	out, err := enricher.Enrich(t.Context(), span)
	require.NoError(t, err)
	return indexed(out)
}

func indexed(out []attribute.KeyValue) map[attribute.Key]attribute.Value {
	m := make(map[attribute.Key]attribute.Value, len(out))
	for _, kv := range out {
		m[kv.Key] = kv.Value
	}
	return m
}

// identity, operation and usage run one family's log enricher over a record.
func identity(t *testing.T, in *Instruments, record *otelv1.InboundLogRecord) map[attribute.Key]attribute.Value {
	t.Helper()
	return enriched(t, &logIdentity{instruments: in}, record)
}

func operation(t *testing.T, in *Instruments, record *otelv1.InboundLogRecord) map[attribute.Key]attribute.Value {
	t.Helper()
	return enriched(t, &logOperation{instruments: in}, record)
}

func usage(t *testing.T, in *Instruments, record *otelv1.InboundLogRecord) map[attribute.Key]attribute.Value {
	t.Helper()
	return enriched(t, &logUsage{instruments: in}, record)
}

// agentAttributes runs every agent attribute enricher over one record or
// span, as the transform does, and indexes what they wrote by key.
func agentAttributes(t *testing.T, in *Instruments, record *otelv1.InboundLogRecord) map[attribute.Key]attribute.Value {
	t.Helper()
	out := map[attribute.Key]attribute.Value{}
	for _, enricher := range LogAgentAttributes(in) {
		maps.Copy(out, enriched(t, enricher, record))
	}
	return out
}

func spanAgentAttributes(t *testing.T, in *Instruments, span *otelv1.InboundSpan) map[attribute.Key]attribute.Value {
	t.Helper()
	out := map[attribute.Key]attribute.Value{}
	for _, enricher := range SpanAgentAttributes(in) {
		maps.Copy(out, enrichedSpan(t, enricher, span))
	}
	return out
}

// readableMeter is a meter provider whose counters a test can read back, so
// a counter is asserted rather than trusted. The noop provider testenv hands
// out would let a broken wiring pass.
func readableMeter(t *testing.T) (*sdkmetric.ManualReader, *sdkmetric.MeterProvider) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	return reader, provider
}

// counterValue reads the one data point of a counter that carries every
// given attribute, or zero when none was recorded. Asking for several
// attributes at once proves they were recorded on the same point rather
// than scattered across points that each carry one.
func counterValue(t *testing.T, reader *sdkmetric.ManualReader, metricName string, want ...attribute.KeyValue) int64 {
	t.Helper()
	var resourceMetrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &resourceMetrics))
	for _, scopeMetrics := range resourceMetrics.ScopeMetrics {
		for _, candidate := range scopeMetrics.Metrics {
			if candidate.Name != metricName {
				continue
			}
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			require.True(t, ok)
		points:
			for _, point := range sum.DataPoints {
				for _, kv := range want {
					if got, ok := point.Attributes.Value(kv.Key); !ok || got != kv.Value {
						continue points
					}
				}
				return point.Value
			}
		}
	}
	return 0
}
