package otel

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	telemetryv1 "github.com/speakeasy-api/gram/infra/gen/gram/telemetry/v1"
	otelattr "go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetricsv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	metricsv1 "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/otel/enrich"
)

const (
	toolCallLogTestEventName = "gram.tool_call.test"
	riskFindingTestEventName = "gram.risk.finding.test"

	// productTelemetryTestName is the log event, span and metric name of a product telemetry test.
	productTelemetryTestName = "speakeasy.test_event"

	// toolCallLogServiceName is the service.name telemetry.Logger stamps on tool call rows.
	toolCallLogServiceName = "gram-server"

	productTelemetryTestServiceName = "speakeasy"
)

// NewRouteTestSources returns the test sources this package owns. The MCP
// connection logs source lives with the connection recorder's sink, so
// production adds it; NewRouteTester refuses a list without it.
func NewRouteTestSources(logger *slog.Logger, meterProvider metric.MeterProvider) []RouteTestSource {
	return []RouteTestSource{
		newToolCallLogTestSource(),
		newRiskFindingTestSource(),
		newProductTelemetryTestSource(logger, meterProvider),
	}
}

// testMarker is the marker attribute every test record carries.
func testMarker() otelattr.KeyValue { return attr.TestEvent(true) }

func otlpTestMarker() *commonv1.KeyValue {
	return &commonv1.KeyValue{
		Key:         string(attr.TestEventKey),
		Value:       &commonv1.AnyValue{Value: &commonv1.AnyValue_BoolValue{BoolValue: true}},
		KeyStrindex: 0,
	}
}

func otlpStringValue(value string) *commonv1.AnyValue {
	return &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: value}}
}

// toolCallLogTestSource builds a tool call row the way telemetry.Logger mirrors
// one, and runs it through the tool call log relay's request builder.
type toolCallLogTestSource struct {
	now func() time.Time
}

var _ RouteTestSource = (*toolCallLogTestSource)(nil)

func newToolCallLogTestSource() *toolCallLogTestSource {
	return &toolCallLogTestSource{now: time.Now}
}

func (s *toolCallLogTestSource) DataSource() string { return dataexports.DataSourceToolCallLogs }

func (s *toolCallLogTestSource) BuildTestRequests(_ context.Context, destination dataexports.TestDestination) ([]RouteTestRequest, error) {
	attributes, err := json.Marshal(map[string]any{
		string(attr.EventSourceKey):    toolCallLogEventSource,
		string(attr.OrganizationIDKey): destination.OrganizationID,
		string(attr.TestEventKey):      true,
	})
	if err != nil {
		return nil, fmt.Errorf("encode tool call log test attributes: %w", err)
	}
	resourceAttributes, err := json.Marshal(map[string]any{string(attr.ServiceNameKey): toolCallLogServiceName})
	if err != nil {
		return nil, fmt.Errorf("encode tool call log test resource: %w", err)
	}

	now := s.now().UTC()
	row := (&telemetryv1.LogRecord_builder{
		Id:                     new(uuid.NewString()),
		TimeUnixNano:           new(now.UnixNano()),
		ObservedTimeUnixNano:   new(now.UnixNano()),
		SeverityText:           new("INFO"),
		Body:                   new(routeTestMessage),
		AttributesJson:         new(string(attributes)),
		ResourceAttributesJson: new(string(resourceAttributes)),
		GramProjectId:          new(destination.ProjectID.String()),
		ServiceName:            new(toolCallLogServiceName),
	}).Build()
	key, reason, ok := toolCallLogRouteKey(row)
	if !ok {
		return nil, fmt.Errorf("tool call log test record does not route: %s", reason)
	}
	if key.organizationID != destination.OrganizationID || key.projectID != destination.ProjectID {
		return nil, errors.New("tool call log test record routes to another project")
	}

	request, err := buildToolCallLogRelayExport([]toolCallLogRelayMessage{{record: row, fail: nil}}, toolCallLogTestEventName, now, destination.IncludeSensitiveData)
	if err != nil {
		return nil, fmt.Errorf("build tool call log test export: %w", err)
	}
	return []RouteTestRequest{{Signal: dataexports.SignalLogs, Message: request, MaxBytes: maxLogRelayExportBytes}}, nil
}

// riskFindingTestSource builds a finding-shaped record with no finding in it, in
// the resource and scope every risk finding export carries. Real findings are
// not redacted, so neither is this.
type riskFindingTestSource struct {
	now func() time.Time
}

var _ RouteTestSource = (*riskFindingTestSource)(nil)

func newRiskFindingTestSource() *riskFindingTestSource {
	return &riskFindingTestSource{now: time.Now}
}

func (s *riskFindingTestSource) DataSource() string { return dataexports.DataSourceRiskFindings }

func (s *riskFindingTestSource) BuildTestRequests(_ context.Context, destination dataexports.TestDestination) ([]RouteTestRequest, error) {
	now := uint64(s.now().UnixNano())
	record := &logsv1.LogRecord{
		TimeUnixNano:         now,
		ObservedTimeUnixNano: now,
		SeverityNumber:       logsv1.SeverityNumber_SEVERITY_NUMBER_INFO,
		SeverityText:         "INFO",
		Body:                 otlpStringValue(routeTestMessage),
		Attributes: []*commonv1.KeyValue{
			{Key: string(attr.OrganizationIDKey), Value: otlpStringValue(destination.OrganizationID), KeyStrindex: 0},
			{Key: string(attr.ProjectIDKey), Value: otlpStringValue(destination.ProjectID.String()), KeyStrindex: 0},
			otlpTestMarker(),
		},
		DroppedAttributesCount: 0,
		Flags:                  0,
		TraceId:                nil,
		SpanId:                 nil,
		EventName:              riskFindingTestEventName,
	}
	return []RouteTestRequest{{Signal: dataexports.SignalLogs, Message: buildRiskFindingRelayExport([]*logsv1.LogRecord{record}), MaxBytes: maxLogRelayExportBytes}}, nil
}

// productTelemetryTestSource sends what a producer would send to the OTLP ingest
// endpoints: one log, one span and one metric. Each goes through the ingest
// conversion, the transform with tenancy enrichment only, and its relay's
// request builder.
type productTelemetryTestSource struct {
	instruments *enrich.Instruments
	now         func() time.Time
}

var _ RouteTestSource = (*productTelemetryTestSource)(nil)

func newProductTelemetryTestSource(logger *slog.Logger, meterProvider metric.MeterProvider) *productTelemetryTestSource {
	return &productTelemetryTestSource{instruments: enrich.NewInstruments(logger, meterProvider), now: time.Now}
}

func (s *productTelemetryTestSource) DataSource() string {
	return dataexports.DataSourceProductTelemetry
}

func (s *productTelemetryTestSource) BuildTestRequests(ctx context.Context, destination dataexports.TestDestination) ([]RouteTestRequest, error) {
	now := s.now()
	logs, err := s.logRequest(ctx, destination, now)
	if err != nil {
		return nil, err
	}
	traces, err := s.traceRequest(ctx, destination, now)
	if err != nil {
		return nil, err
	}
	metrics, err := s.metricRequest(ctx, destination, now)
	if err != nil {
		return nil, err
	}
	return []RouteTestRequest{logs, traces, metrics}, nil
}

func productTelemetryTestResource() *resourcev1.Resource {
	return &resourcev1.Resource{
		Attributes:             []*commonv1.KeyValue{{Key: string(attr.ServiceNameKey), Value: otlpStringValue(productTelemetryTestServiceName), KeyStrindex: 0}},
		DroppedAttributesCount: 0,
		EntityRefs:             nil,
	}
}

func (s *productTelemetryTestSource) logRequest(ctx context.Context, destination dataexports.TestDestination, now time.Time) (RouteTestRequest, error) {
	var empty RouteTestRequest
	export := &collectorlogsv1.ExportLogsServiceRequest{ResourceLogs: []*logsv1.ResourceLogs{{
		Resource: productTelemetryTestResource(),
		ScopeLogs: []*logsv1.ScopeLogs{{
			Scope: nil,
			LogRecords: []*logsv1.LogRecord{{
				TimeUnixNano:           uint64(now.UnixNano()),
				ObservedTimeUnixNano:   uint64(now.UnixNano()),
				SeverityNumber:         logsv1.SeverityNumber_SEVERITY_NUMBER_INFO,
				SeverityText:           "INFO",
				Body:                   otlpStringValue(routeTestMessage),
				Attributes:             nil,
				DroppedAttributesCount: 0,
				Flags:                  0,
				TraceId:                nil,
				SpanId:                 nil,
				EventName:              productTelemetryTestName,
			}},
			SchemaUrl: "",
		}},
		SchemaUrl: "",
	}}}
	provenance := (&otelv1.InboundLogRecord_Provenance_builder{
		Source:         new(ProvenanceSource),
		OrganizationId: &destination.OrganizationID,
		ProjectId:      new(destination.ProjectID.String()),
	}).Build()
	inbound, err := inboundLogRecordsFromExport(export, provenance)
	if err != nil {
		return empty, fmt.Errorf("convert product telemetry test log: %w", err)
	}
	if len(inbound) != 1 {
		return empty, fmt.Errorf("product telemetry test log converted to %d records", len(inbound))
	}
	if err := ValidateInboundLogRecord(inbound[0]); err != nil {
		return empty, fmt.Errorf("validate product telemetry test log: %w", err)
	}
	record, err := transformInboundLog(ctx, s.instruments, inbound[0], []enrich.LogEnricher{enrich.NewLogTenancy()})
	if err != nil {
		return empty, fmt.Errorf("transform product telemetry test log: %w", err)
	}
	if isConnectionLogRecord(record) || logOriginalScopeName(record) == dialect.GatewayLogScope {
		return empty, errors.New("product telemetry test log would not reach the product telemetry route")
	}
	if err := applyLogEnrichments(record, []otelattr.KeyValue{testMarker()}); err != nil {
		return empty, fmt.Errorf("mark product telemetry test log: %w", err)
	}
	request, err := newLogRelayExportRequest([]*otelv1.LogRecord{record}, destination.IncludeSensitiveData)
	if err != nil {
		return empty, fmt.Errorf("build product telemetry test log export: %w", err)
	}
	return RouteTestRequest{Signal: dataexports.SignalLogs, Message: request, MaxBytes: maxLogRelayExportBytes}, nil
}

func (s *productTelemetryTestSource) traceRequest(ctx context.Context, destination dataexports.TestDestination, now time.Time) (RouteTestRequest, error) {
	var empty RouteTestRequest
	traceID := make([]byte, otlpTraceIDSize)
	spanID := make([]byte, otlpSpanIDSize)
	_, _ = rand.Read(traceID)
	_, _ = rand.Read(spanID)
	export := &collectortracev1.ExportTraceServiceRequest{ResourceSpans: []*tracev1.ResourceSpans{{
		Resource: productTelemetryTestResource(),
		ScopeSpans: []*tracev1.ScopeSpans{{
			Scope: nil,
			Spans: []*tracev1.Span{{
				TraceId:           traceID,
				SpanId:            spanID,
				Name:              productTelemetryTestName,
				Kind:              tracev1.Span_SPAN_KIND_INTERNAL,
				StartTimeUnixNano: uint64(now.UnixNano()),
				EndTimeUnixNano:   uint64(now.UnixNano()),
			}},
		}},
	}}}
	raw, err := proto.Marshal(export)
	if err != nil {
		return empty, fmt.Errorf("encode product telemetry test span: %w", err)
	}
	provenance := (&otelv1.InboundSpan_Provenance_builder{
		Source:         new(ProvenanceSource),
		OrganizationId: &destination.OrganizationID,
		ProjectId:      new(destination.ProjectID.String()),
	}).Build()
	inbound, err := decodeOTLPTraceExport(raw, provenance)
	if err != nil {
		return empty, fmt.Errorf("convert product telemetry test span: %w", err)
	}
	if len(inbound) != 1 {
		return empty, fmt.Errorf("product telemetry test span converted to %d spans", len(inbound))
	}
	if err := validateSpan(inbound[0]); err != nil {
		return empty, fmt.Errorf("validate product telemetry test span: %w", err)
	}
	span, err := transformInboundSpan(ctx, s.instruments, inbound[0], []enrich.SpanEnricher{enrich.NewSpanTenancy()})
	if err != nil {
		return empty, fmt.Errorf("transform product telemetry test span: %w", err)
	}
	if err := applySpanEnrichments(span, []otelattr.KeyValue{testMarker()}); err != nil {
		return empty, fmt.Errorf("mark product telemetry test span: %w", err)
	}
	request, err := newRelayExportRequest([]*otelv1.Span{span}, destination.IncludeSensitiveData)
	if err != nil {
		return empty, fmt.Errorf("build product telemetry test span export: %w", err)
	}
	return RouteTestRequest{Signal: dataexports.SignalTraces, Message: request, MaxBytes: 0}, nil
}

func (s *productTelemetryTestSource) metricRequest(ctx context.Context, destination dataexports.TestDestination, now time.Time) (RouteTestRequest, error) {
	var empty RouteTestRequest
	export := &collectormetricsv1.ExportMetricsServiceRequest{ResourceMetrics: []*metricsv1.ResourceMetrics{{
		Resource: productTelemetryTestResource(),
		ScopeMetrics: []*metricsv1.ScopeMetrics{{
			Scope: nil,
			Metrics: []*metricsv1.Metric{{
				Name: productTelemetryTestName,
				Data: &metricsv1.Metric_Gauge{Gauge: &metricsv1.Gauge{DataPoints: []*metricsv1.NumberDataPoint{{
					Attributes:   []*commonv1.KeyValue{otlpTestMarker()},
					TimeUnixNano: uint64(now.UnixNano()),
					Value:        &metricsv1.NumberDataPoint_AsInt{AsInt: 1},
				}}}},
			}},
		}},
	}}}
	provenance := (&otelv1.InboundMetric_Provenance_builder{
		Source:         new(ProvenanceSource),
		OrganizationId: &destination.OrganizationID,
		ProjectId:      new(destination.ProjectID.String()),
	}).Build()
	inbound, err := inboundMetricsFromExport(export, provenance)
	if err != nil {
		return empty, fmt.Errorf("convert product telemetry test metric: %w", err)
	}
	if len(inbound) != 1 {
		return empty, fmt.Errorf("product telemetry test metric converted to %d metrics", len(inbound))
	}
	if err := validateInboundMetric(inbound[0]); err != nil {
		return empty, fmt.Errorf("validate product telemetry test metric: %w", err)
	}
	item, err := transformInboundMetric(ctx, s.instruments, inbound[0], nil)
	if err != nil {
		return empty, fmt.Errorf("transform product telemetry test metric: %w", err)
	}
	request, err := newMetricRelayExportRequest([]*otelv1.Metric{item}, destination.IncludeSensitiveData)
	if err != nil {
		return empty, fmt.Errorf("build product telemetry test metric export: %w", err)
	}
	return RouteTestRequest{Signal: dataexports.SignalMetrics, Message: request, MaxBytes: maxMetricRelayExportBytes}, nil
}

// ConnectionLogTestRequests turns a connection test record, built by the
// connection recorder's sink, into what the connection logs route sends.
type ConnectionLogTestRequests struct {
	instruments *enrich.Instruments
	// enrichers is tenancy only: the directory enricher would add a person.
	enrichers []enrich.LogEnricher
}

func NewConnectionLogTestRequests(logger *slog.Logger, meterProvider metric.MeterProvider) *ConnectionLogTestRequests {
	return &ConnectionLogTestRequests{
		instruments: enrich.NewInstruments(logger, meterProvider),
		enrichers:   []enrich.LogEnricher{enrich.NewLogTenancy()},
	}
}

// Build transforms the record, adds the test marker, which a producer may not
// write, and builds the logs request for the destination.
func (c *ConnectionLogTestRequests) Build(ctx context.Context, inbound *otelv1.InboundLogRecord, destination dataexports.TestDestination) (RouteTestRequest, error) {
	var empty RouteTestRequest
	record, err := transformInboundLog(ctx, c.instruments, inbound, c.enrichers)
	if err != nil {
		return empty, fmt.Errorf("transform connection log test record: %w", err)
	}
	if !isConnectionLogRecord(record) {
		return empty, errors.New("connection log test record has the wrong provenance source")
	}
	if err := applyLogEnrichments(record, []otelattr.KeyValue{testMarker()}); err != nil {
		return empty, fmt.Errorf("mark connection log test record: %w", err)
	}
	request, err := newLogRelayExportRequest([]*otelv1.LogRecord{record}, destination.IncludeSensitiveData)
	if err != nil {
		return empty, fmt.Errorf("build connection log test export: %w", err)
	}
	return RouteTestRequest{Signal: dataexports.SignalLogs, Message: request, MaxBytes: maxLogRelayExportBytes}, nil
}
