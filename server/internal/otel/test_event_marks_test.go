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
	return append(otlpTestMarks("spoofed"), &commonv1.KeyValue{Key: "kept", Value: otlpStringValue("yes"), KeyStrindex: 0})
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

func TestTransformsStripProducerTestMarks(t *testing.T) {
	t.Parallel()

	instruments := enrich.NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
	projectID := "01900000-0000-7000-8000-000000000404"

	t.Run("log", func(t *testing.T) {
		t.Parallel()
		export := &collectorlogsv1.ExportLogsServiceRequest{ResourceLogs: []*logsv1.ResourceLogs{{
			Resource:  spoofedResource(),
			ScopeLogs: []*logsv1.ScopeLogs{{LogRecords: []*logsv1.LogRecord{{Body: otlpStringValue(TestEventMessage), Attributes: spoofedMarks()}}}},
		}}}
		provenance := (&otelv1.InboundLogRecord_Provenance_builder{Source: new(ProvenanceSource), OrganizationId: new("org"), ProjectId: &projectID}).Build()
		inbound, err := inboundLogRecordsFromExport(export, provenance)
		require.NoError(t, err)
		record, err := transformInboundLog(t.Context(), instruments, inbound[0], nil)
		require.NoError(t, err)
		requireNoTestMarks(t, record.GetAttributes())
		requireNoTestMarks(t, record.GetResource().GetAttributes())

		// Without the marker, the exact test text is redacted like any body.
		request, err := newLogRelayExportRequest([]*otelv1.LogRecord{record}, false)
		require.NoError(t, err)
		require.Equal(t, redactedSensitiveDataValue, request.GetResourceLogs()[0].GetScopeLogs()[0].GetLogRecords()[0].GetBody().GetStringValue())
	})

	t.Run("span", func(t *testing.T) {
		t.Parallel()
		export := &collectortracev1.ExportTraceServiceRequest{ResourceSpans: []*tracev1.ResourceSpans{{
			Resource: spoofedResource(),
			ScopeSpans: []*tracev1.ScopeSpans{{Spans: []*tracev1.Span{{
				TraceId: make([]byte, otlpTraceIDSize), SpanId: make([]byte, otlpSpanIDSize), Name: "real", Attributes: spoofedMarks(),
			}}}},
		}}}
		raw, err := proto.Marshal(export)
		require.NoError(t, err)
		provenance := (&otelv1.InboundSpan_Provenance_builder{Source: new(ProvenanceSource), OrganizationId: new("org"), ProjectId: &projectID}).Build()
		inbound, err := decodeOTLPTraceExport(raw, provenance)
		require.NoError(t, err)
		span, err := transformInboundSpan(t.Context(), instruments, inbound[0], nil)
		require.NoError(t, err)
		requireNoTestMarks(t, span.GetAttributes())
		requireNoTestMarks(t, span.GetResource().GetAttributes())
	})

	t.Run("metric", func(t *testing.T) {
		t.Parallel()
		export := &collectormetricsv1.ExportMetricsServiceRequest{ResourceMetrics: []*metricsv1.ResourceMetrics{{
			Resource: spoofedResource(),
			ScopeMetrics: []*metricsv1.ScopeMetrics{{Metrics: []*metricsv1.Metric{
				{Name: "gauge", Data: &metricsv1.Metric_Gauge{Gauge: &metricsv1.Gauge{DataPoints: []*metricsv1.NumberDataPoint{{Attributes: spoofedMarks(), Value: &metricsv1.NumberDataPoint_AsInt{AsInt: 1}}}}}},
				{Name: "sum", Data: &metricsv1.Metric_Sum{Sum: &metricsv1.Sum{DataPoints: []*metricsv1.NumberDataPoint{{Attributes: spoofedMarks(), Value: &metricsv1.NumberDataPoint_AsInt{AsInt: 1}}}}}},
				{Name: "histogram", Data: &metricsv1.Metric_Histogram{Histogram: &metricsv1.Histogram{DataPoints: []*metricsv1.HistogramDataPoint{{Attributes: spoofedMarks()}}}}},
			}}},
		}}}
		provenance := (&otelv1.InboundMetric_Provenance_builder{Source: new(ProvenanceSource), OrganizationId: new("org"), ProjectId: &projectID}).Build()
		inbound, err := inboundMetricsFromExport(export, provenance)
		require.NoError(t, err)
		require.Len(t, inbound, 3)
		for _, item := range inbound {
			out, err := transformInboundMetric(t.Context(), instruments, item, nil)
			require.NoError(t, err)
			requireNoTestMarks(t, out.GetResource().GetAttributes())
			for _, point := range out.GetGauge().GetDataPoints() {
				requireNoTestMarks(t, point.GetAttributes())
			}
			for _, point := range out.GetSum().GetDataPoints() {
				requireNoTestMarks(t, point.GetAttributes())
			}
			for _, point := range out.GetHistogram().GetDataPoints() {
				requireNoTestMarks(t, point.GetAttributes())
			}
		}
	})
}

func TestRedactionKeepsOnlyTheMarkedTestMessage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		body   string
		marked bool
		kept   bool
	}{
		{name: "marked test message", body: TestEventMessage, marked: true, kept: true},
		{name: "marked real text", body: "Speakeasy test event: user said something private", marked: true, kept: false},
		{name: "unmarked test message", body: TestEventMessage, marked: false, kept: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			record := &logsv1.LogRecord{Body: otlpStringValue(tc.body)}
			if tc.marked {
				record.Attributes = []*commonv1.KeyValue{otlpTestMarker()}
			}
			redactSensitiveOTLP(&collectorlogsv1.ExportLogsServiceRequest{ResourceLogs: []*logsv1.ResourceLogs{{
				ScopeLogs: []*logsv1.ScopeLogs{{LogRecords: []*logsv1.LogRecord{record}}},
			}}})
			if tc.kept {
				require.Equal(t, tc.body, record.GetBody().GetStringValue())
			} else {
				require.Equal(t, redactedSensitiveDataValue, record.GetBody().GetStringValue())
			}
		})
	}
}
