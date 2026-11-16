package otel

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetricsv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	metricsv1 "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/otel/enrich"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// spoofedMarks are the test marks a producer tries to send on real data.
func spoofedMarks() []*commonv1.KeyValue {
	return []*commonv1.KeyValue{
		{Key: "speakeasy.test_event", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_BoolValue{BoolValue: true}}, KeyStrindex: 0},
		{Key: "speakeasy.test_event.id", Value: otlpStringValue("spoofed"), KeyStrindex: 0},
		{Key: "kept", Value: otlpStringValue("yes"), KeyStrindex: 0},
	}
}

func spoofedResource() *resourcev1.Resource {
	return &resourcev1.Resource{Attributes: spoofedMarks(), DroppedAttributesCount: 0, EntityRefs: nil}
}

func requireNoTestMarks[KV interface{ GetKey() string }](t *testing.T, attributes []KV) {
	t.Helper()
	keys := make([]string, len(attributes))
	for i, kv := range attributes {
		keys[i] = kv.GetKey()
	}
	require.NotContains(t, keys, "speakeasy.test_event")
	require.NotContains(t, keys, "speakeasy.test_event.id")
	require.Contains(t, keys, "kept")
}

func spoofedScope() *commonv1.InstrumentationScope {
	return &commonv1.InstrumentationScope{Name: "producer", Version: "", Attributes: spoofedMarks(), DroppedAttributesCount: 0}
}

func TestTransformsStripProducerTestMarksBeforeTheRelay(t *testing.T) {
	t.Parallel()

	instruments := enrich.NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
	projectID := "01900000-0000-7000-8000-000000000404"

	t.Run("log", func(t *testing.T) {
		t.Parallel()
		export := &collectorlogsv1.ExportLogsServiceRequest{ResourceLogs: []*logsv1.ResourceLogs{{
			Resource:  spoofedResource(),
			ScopeLogs: []*logsv1.ScopeLogs{{Scope: spoofedScope(), LogRecords: []*logsv1.LogRecord{{Body: otlpStringValue(TestEventMessage), Attributes: spoofedMarks()}}}},
		}}}
		provenance := (&otelv1.InboundLogRecord_Provenance_builder{Source: new(ProvenanceSource), OrganizationId: new("org"), ProjectId: &projectID}).Build()
		inbound, err := inboundLogRecordsFromExport(export, provenance)
		require.NoError(t, err)
		record, err := transformInboundLog(t.Context(), instruments, inbound[0], nil)
		require.NoError(t, err)
		request, err := newLogRelayExportRequest([]*otelv1.LogRecord{record}, false)
		require.NoError(t, err)

		resourceLogs := requireOne(t, request.GetResourceLogs())
		requireNoTestMarks(t, resourceLogs.GetResource().GetAttributes())
		scopeLogs := requireOne(t, resourceLogs.GetScopeLogs())
		requireNoTestMarks(t, scopeLogs.GetScope().GetAttributes())
		relayed := requireOne(t, scopeLogs.GetLogRecords())
		requireNoTestMarks(t, relayed.GetAttributes())
		// Without the marker, the exact test text is redacted like any body.
		require.Equal(t, redactedSensitiveDataValue, relayed.GetBody().GetStringValue())
	})

	t.Run("span", func(t *testing.T) {
		t.Parallel()
		export := &collectortracev1.ExportTraceServiceRequest{ResourceSpans: []*tracev1.ResourceSpans{{
			Resource: spoofedResource(),
			ScopeSpans: []*tracev1.ScopeSpans{{Scope: spoofedScope(), Spans: []*tracev1.Span{{
				TraceId: make([]byte, otlpTraceIDSize), SpanId: make([]byte, otlpSpanIDSize), Name: "real", Attributes: spoofedMarks(),
				Events: []*tracev1.Span_Event{{Name: "event", Attributes: spoofedMarks()}},
				Links:  []*tracev1.Span_Link{{TraceId: make([]byte, otlpTraceIDSize), SpanId: make([]byte, otlpSpanIDSize), Attributes: spoofedMarks()}},
			}}}},
		}}}
		raw, err := proto.Marshal(export)
		require.NoError(t, err)
		provenance := (&otelv1.InboundSpan_Provenance_builder{Source: new(ProvenanceSource), OrganizationId: new("org"), ProjectId: &projectID}).Build()
		inbound, err := decodeOTLPTraceExport(raw, provenance)
		require.NoError(t, err)
		span, err := transformInboundSpan(t.Context(), instruments, inbound[0], nil)
		require.NoError(t, err)
		request, err := newRelayExportRequest([]*otelv1.Span{span}, true)
		require.NoError(t, err)

		resourceSpans := requireOne(t, request.GetResourceSpans())
		requireNoTestMarks(t, resourceSpans.GetResource().GetAttributes())
		scopeSpans := requireOne(t, resourceSpans.GetScopeSpans())
		requireNoTestMarks(t, scopeSpans.GetScope().GetAttributes())
		relayed := requireOne(t, scopeSpans.GetSpans())
		requireNoTestMarks(t, relayed.GetAttributes())
		requireNoTestMarks(t, requireOne(t, relayed.GetEvents()).GetAttributes())
		requireNoTestMarks(t, requireOne(t, relayed.GetLinks()).GetAttributes())
	})

	t.Run("metric", func(t *testing.T) {
		t.Parallel()
		exemplars := func() []*metricsv1.Exemplar {
			return []*metricsv1.Exemplar{{FilteredAttributes: spoofedMarks(), Value: &metricsv1.Exemplar_AsInt{AsInt: 1}}}
		}
		export := &collectormetricsv1.ExportMetricsServiceRequest{ResourceMetrics: []*metricsv1.ResourceMetrics{{
			Resource: spoofedResource(),
			ScopeMetrics: []*metricsv1.ScopeMetrics{{Scope: spoofedScope(), Metrics: []*metricsv1.Metric{
				{Name: "gauge", Metadata: spoofedMarks(), Data: &metricsv1.Metric_Gauge{Gauge: &metricsv1.Gauge{DataPoints: []*metricsv1.NumberDataPoint{{Attributes: spoofedMarks(), Exemplars: exemplars(), Value: &metricsv1.NumberDataPoint_AsInt{AsInt: 1}}}}}},
				{Name: "sum", Metadata: spoofedMarks(), Data: &metricsv1.Metric_Sum{Sum: &metricsv1.Sum{DataPoints: []*metricsv1.NumberDataPoint{{Attributes: spoofedMarks(), Exemplars: exemplars(), Value: &metricsv1.NumberDataPoint_AsInt{AsInt: 1}}}}}},
				{Name: "histogram", Metadata: spoofedMarks(), Data: &metricsv1.Metric_Histogram{Histogram: &metricsv1.Histogram{DataPoints: []*metricsv1.HistogramDataPoint{{Attributes: spoofedMarks(), Exemplars: exemplars()}}}}},
				{Name: "exponential", Metadata: spoofedMarks(), Data: &metricsv1.Metric_ExponentialHistogram{ExponentialHistogram: &metricsv1.ExponentialHistogram{DataPoints: []*metricsv1.ExponentialHistogramDataPoint{{Attributes: spoofedMarks(), Exemplars: exemplars()}}}}},
				{Name: "summary", Metadata: spoofedMarks(), Data: &metricsv1.Metric_Summary{Summary: &metricsv1.Summary{DataPoints: []*metricsv1.SummaryDataPoint{{Attributes: spoofedMarks()}}}}},
			}}},
		}}}
		provenance := (&otelv1.InboundMetric_Provenance_builder{Source: new(ProvenanceSource), OrganizationId: new("org"), ProjectId: &projectID}).Build()
		inbound, err := inboundMetricsFromExport(export, provenance)
		require.NoError(t, err)
		require.Len(t, inbound, 5)
		items := make([]*otelv1.Metric, len(inbound))
		for i, item := range inbound {
			items[i], err = transformInboundMetric(t.Context(), instruments, item, nil)
			require.NoError(t, err)
		}
		request, err := newMetricRelayExportRequest(items, true)
		require.NoError(t, err)

		resourceMetrics := requireOne(t, request.GetResourceMetrics())
		requireNoTestMarks(t, resourceMetrics.GetResource().GetAttributes())
		scopeMetrics := requireOne(t, resourceMetrics.GetScopeMetrics())
		requireNoTestMarks(t, scopeMetrics.GetScope().GetAttributes())
		require.Len(t, scopeMetrics.GetMetrics(), 5)
		for _, item := range scopeMetrics.GetMetrics() {
			requireNoTestMarks(t, item.GetMetadata())
			for _, point := range item.GetGauge().GetDataPoints() {
				requireNoTestMarks(t, point.GetAttributes())
				requireNoTestMarks(t, requireOne(t, point.GetExemplars()).GetFilteredAttributes())
			}
			for _, point := range item.GetSum().GetDataPoints() {
				requireNoTestMarks(t, point.GetAttributes())
				requireNoTestMarks(t, requireOne(t, point.GetExemplars()).GetFilteredAttributes())
			}
			for _, point := range item.GetHistogram().GetDataPoints() {
				requireNoTestMarks(t, point.GetAttributes())
				requireNoTestMarks(t, requireOne(t, point.GetExemplars()).GetFilteredAttributes())
			}
			for _, point := range item.GetExponentialHistogram().GetDataPoints() {
				requireNoTestMarks(t, point.GetAttributes())
				requireNoTestMarks(t, requireOne(t, point.GetExemplars()).GetFilteredAttributes())
			}
			for _, point := range item.GetSummary().GetDataPoints() {
				requireNoTestMarks(t, point.GetAttributes())
			}
		}
	})
}

func TestRedactionRedactsAMarkedTestMessage(t *testing.T) {
	t.Parallel()

	record := &logsv1.LogRecord{Body: otlpStringValue(TestEventMessage), Attributes: spoofedMarks()}
	redactSensitiveOTLP(&collectorlogsv1.ExportLogsServiceRequest{ResourceLogs: []*logsv1.ResourceLogs{{
		ScopeLogs: []*logsv1.ScopeLogs{{LogRecords: []*logsv1.LogRecord{record}}},
	}}})
	require.Equal(t, redactedSensitiveDataValue, record.GetBody().GetStringValue())
}
