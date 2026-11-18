package otel

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	telemetryv1 "github.com/speakeasy-api/gram/infra/gen/gram/telemetry/v1"
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

func errUnsupportedTestSignal(source string, signal dataexports.Signal) error {
	return fmt.Errorf("%s does not export %s", source, signal)
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

func (s *toolCallLogTestSource) BuildTestRequest(_ context.Context, signal dataexports.Signal, destination dataexports.TestDestination) (proto.Message, error) {
	if signal != dataexports.SignalLogs {
		return nil, errUnsupportedTestSignal(s.DataSource(), signal)
	}
	attributes, err := json.Marshal(map[string]any{
		string(attr.EventSourceKey):    toolCallLogEventSource,
		string(attr.OrganizationIDKey): destination.OrganizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("encode tool call log test attributes: %w", err)
	}
	resourceAttributes, err := json.Marshal(map[string]any{string(attr.ServiceNameKey): toolCallLogServiceName})
	if err != nil {
		return nil, fmt.Errorf("encode tool call log test resource: %w", err)
	}

	now := s.now().UTC()
	// No Id: a test row has no telemetry_logs row to name.
	row := (&telemetryv1.LogRecord_builder{
		TimeUnixNano:           new(now.UnixNano()),
		ObservedTimeUnixNano:   new(now.UnixNano()),
		SeverityText:           new("INFO"),
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

	request, err := buildToolCallLogRelayExport([]toolCallLogRelayMessage{{record: row, fail: nil}}, TestEventName, now, destination.IncludeSensitiveData)
	if err != nil {
		return nil, fmt.Errorf("build tool call log test export: %w", err)
	}
	return request, nil
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

func (s *riskFindingTestSource) BuildTestRequest(_ context.Context, signal dataexports.Signal, destination dataexports.TestDestination) (proto.Message, error) {
	if signal != dataexports.SignalLogs {
		return nil, errUnsupportedTestSignal(s.DataSource(), signal)
	}
	now := uint64(s.now().UnixNano())
	record := &logsv1.LogRecord{
		TimeUnixNano:         now,
		ObservedTimeUnixNano: now,
		SeverityNumber:       logsv1.SeverityNumber_SEVERITY_NUMBER_INFO,
		SeverityText:         "INFO",
		Body:                 nil,
		Attributes: []*commonv1.KeyValue{
			{Key: string(attr.OrganizationIDKey), Value: otlpStringValue(destination.OrganizationID), KeyStrindex: 0},
			{Key: string(attr.ProjectIDKey), Value: otlpStringValue(destination.ProjectID.String()), KeyStrindex: 0},
		},
		DroppedAttributesCount: 0,
		Flags:                  0,
		TraceId:                nil,
		SpanId:                 nil,
		EventName:              TestEventName,
	}
	return buildRiskFindingRelayExport([]*logsv1.LogRecord{record}), nil
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

func (s *productTelemetryTestSource) BuildTestRequest(ctx context.Context, signal dataexports.Signal, destination dataexports.TestDestination) (proto.Message, error) {
	switch signal {
	case dataexports.SignalLogs:
		return s.logRequest(ctx, destination, s.now())
	case dataexports.SignalTraces:
		return s.traceRequest(ctx, destination, s.now())
	case dataexports.SignalMetrics:
		return s.metricRequest(ctx, destination, s.now())
	default:
		return nil, errUnsupportedTestSignal(s.DataSource(), signal)
	}
}

func productTelemetryTestResource() *resourcev1.Resource {
	return &resourcev1.Resource{
		Attributes:             []*commonv1.KeyValue{{Key: string(attr.ServiceNameKey), Value: otlpStringValue(productTelemetryTestServiceName), KeyStrindex: 0}},
		DroppedAttributesCount: 0,
		EntityRefs:             nil,
	}
}

func (s *productTelemetryTestSource) logRequest(ctx context.Context, destination dataexports.TestDestination, now time.Time) (proto.Message, error) {
	export := &collectorlogsv1.ExportLogsServiceRequest{ResourceLogs: []*logsv1.ResourceLogs{{
		Resource: productTelemetryTestResource(),
		ScopeLogs: []*logsv1.ScopeLogs{{
			Scope: nil,
			LogRecords: []*logsv1.LogRecord{{
				TimeUnixNano:           uint64(now.UnixNano()),
				ObservedTimeUnixNano:   uint64(now.UnixNano()),
				SeverityNumber:         logsv1.SeverityNumber_SEVERITY_NUMBER_INFO,
				SeverityText:           "INFO",
				Body:                   nil,
				Attributes:             nil,
				DroppedAttributesCount: 0,
				Flags:                  0,
				TraceId:                nil,
				SpanId:                 nil,
				EventName:              TestEventName,
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
		return nil, fmt.Errorf("convert product telemetry test log: %w", err)
	}
	if len(inbound) != 1 {
		return nil, fmt.Errorf("product telemetry test log converted to %d records", len(inbound))
	}
	if err := ValidateInboundLogRecord(inbound[0]); err != nil {
		return nil, fmt.Errorf("validate product telemetry test log: %w", err)
	}
	return buildTestLogRequest(ctx, s.instruments, inbound[0], destination, func(record *otelv1.LogRecord) error {
		if isConnectionLogRecord(record) || logOriginalScopeName(record) == dialect.GatewayLogScope {
			return errors.New("product telemetry test log would not reach the product telemetry route")
		}
		return nil
	})
}

func (s *productTelemetryTestSource) traceRequest(ctx context.Context, destination dataexports.TestDestination, now time.Time) (proto.Message, error) {
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
				Name:              TestEventName,
				Kind:              tracev1.Span_SPAN_KIND_INTERNAL,
				StartTimeUnixNano: uint64(now.UnixNano()),
				EndTimeUnixNano:   uint64(now.UnixNano()),
			}},
		}},
	}}}
	raw, err := proto.Marshal(export)
	if err != nil {
		return nil, fmt.Errorf("encode product telemetry test span: %w", err)
	}
	provenance := (&otelv1.InboundSpan_Provenance_builder{
		Source:         new(ProvenanceSource),
		OrganizationId: &destination.OrganizationID,
		ProjectId:      new(destination.ProjectID.String()),
	}).Build()
	inbound, err := decodeOTLPTraceExport(raw, provenance)
	if err != nil {
		return nil, fmt.Errorf("convert product telemetry test span: %w", err)
	}
	if len(inbound) != 1 {
		return nil, fmt.Errorf("product telemetry test span converted to %d spans", len(inbound))
	}
	if err := validateSpan(inbound[0]); err != nil {
		return nil, fmt.Errorf("validate product telemetry test span: %w", err)
	}
	span, err := transformInboundSpan(ctx, s.instruments, inbound[0], []enrich.SpanEnricher{enrich.NewSpanTenancy()})
	if err != nil {
		return nil, fmt.Errorf("transform product telemetry test span: %w", err)
	}
	request, err := newRelayExportRequest([]*otelv1.Span{span}, destination.IncludeSensitiveData)
	if err != nil {
		return nil, fmt.Errorf("build product telemetry test span export: %w", err)
	}
	return request, nil
}

func (s *productTelemetryTestSource) metricRequest(ctx context.Context, destination dataexports.TestDestination, now time.Time) (proto.Message, error) {
	export := &collectormetricsv1.ExportMetricsServiceRequest{ResourceMetrics: []*metricsv1.ResourceMetrics{{
		Resource: productTelemetryTestResource(),
		ScopeMetrics: []*metricsv1.ScopeMetrics{{
			Scope: nil,
			Metrics: []*metricsv1.Metric{{
				Name: TestEventName,
				Data: &metricsv1.Metric_Gauge{Gauge: &metricsv1.Gauge{DataPoints: []*metricsv1.NumberDataPoint{{
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
		return nil, fmt.Errorf("convert product telemetry test metric: %w", err)
	}
	if len(inbound) != 1 {
		return nil, fmt.Errorf("product telemetry test metric converted to %d metrics", len(inbound))
	}
	if err := validateInboundMetric(inbound[0]); err != nil {
		return nil, fmt.Errorf("validate product telemetry test metric: %w", err)
	}
	item, err := transformInboundMetric(ctx, s.instruments, inbound[0], nil)
	if err != nil {
		return nil, fmt.Errorf("transform product telemetry test metric: %w", err)
	}
	request, err := newMetricRelayExportRequest([]*otelv1.Metric{item}, destination.IncludeSensitiveData)
	if err != nil {
		return nil, fmt.Errorf("build product telemetry test metric export: %w", err)
	}
	return request, nil
}

// buildTestLogRequest runs an inbound test log through the log transform with
// tenancy enrichment only, checks it reaches the expected route, and builds the
// logs request.
func buildTestLogRequest(ctx context.Context, instruments *enrich.Instruments, inbound *otelv1.InboundLogRecord, destination dataexports.TestDestination, routeCheck func(*otelv1.LogRecord) error) (proto.Message, error) {
	// Tenancy only: the directory enricher would add a person.
	record, err := transformInboundLog(ctx, instruments, inbound, []enrich.LogEnricher{enrich.NewLogTenancy()})
	if err != nil {
		return nil, fmt.Errorf("transform test log: %w", err)
	}
	if err := routeCheck(record); err != nil {
		return nil, err
	}
	request, err := newLogRelayExportRequest([]*otelv1.LogRecord{record}, destination.IncludeSensitiveData)
	if err != nil {
		return nil, fmt.Errorf("build test log export: %w", err)
	}
	return request, nil
}

// ConnectionLogTestRequestBuilder turns a connection test record, built by the
// connection recorder's sink, into what the connection logs route sends.
type ConnectionLogTestRequestBuilder struct {
	instruments *enrich.Instruments
}

func NewConnectionLogTestRequestBuilder(logger *slog.Logger, meterProvider metric.MeterProvider) *ConnectionLogTestRequestBuilder {
	return &ConnectionLogTestRequestBuilder{instruments: enrich.NewInstruments(logger, meterProvider)}
}

func (c *ConnectionLogTestRequestBuilder) Build(ctx context.Context, inbound *otelv1.InboundLogRecord, destination dataexports.TestDestination) (proto.Message, error) {
	return buildTestLogRequest(ctx, c.instruments, inbound, destination, func(record *otelv1.LogRecord) error {
		if !isConnectionLogRecord(record) {
			return errors.New("connection log test record has the wrong provenance source")
		}
		return nil
	})
}
