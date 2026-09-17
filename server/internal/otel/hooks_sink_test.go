package otel

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetricsv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	metricsv1 "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	hooksgen "github.com/speakeasy-api/gram/server/gen/hooks"
	gen "github.com/speakeasy-api/gram/server/gen/otel"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// sinkEventLog records publish settlements and sink calls in the order they
// happen, so tests can assert the sink runs only once every publish is durable.
type sinkEventLog struct {
	events []string
}

type recordingHooksSink struct {
	events  *sinkEventLog
	logs    []*hooksgen.LogsPayload
	metrics []*hooksgen.MetricsPayload
}

func (r *recordingHooksSink) IngestOTLPLogs(_ context.Context, payload *hooksgen.LogsPayload) {
	r.events.events = append(r.events.events, "sink")
	r.logs = append(r.logs, payload)
}

func (r *recordingHooksSink) IngestOTLPMetrics(_ context.Context, payload *hooksgen.MetricsPayload) {
	r.events.events = append(r.events.events, "sink")
	r.metrics = append(r.metrics, payload)
}

// settledPublishResult reports success only once Get is called, recording
// that settlement so the test can order it against the sink call.
type settledPublishResult struct {
	events *sinkEventLog
}

func (r *settledPublishResult) Ready() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (r *settledPublishResult) Get(_ context.Context) (string, error) {
	r.events.events = append(r.events.events, "publish.get")
	return "settled", nil
}

func requireSinkAfterEveryPublish(t *testing.T, events *sinkEventLog, publishes int) {
	t.Helper()
	expected := make([]string, 0, publishes+1)
	for range publishes {
		expected = append(expected, "publish.get")
	}
	expected = append(expected, "sink")
	require.Equal(t, expected, events.events)
}

func claudeSinkTestExport() *collectorlogsv1.ExportLogsServiceRequest {
	return &collectorlogsv1.ExportLogsServiceRequest{
		ResourceLogs: []*logsv1.ResourceLogs{{
			Resource: &resourcev1.Resource{Attributes: []*commonv1.KeyValue{{
				Key:   "service.name",
				Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "claude-code"}},
			}}},
			ScopeLogs: []*logsv1.ScopeLogs{{
				Scope: &commonv1.InstrumentationScope{Name: "com.anthropic.claude_code.events", Version: "2.1.0"},
				LogRecords: []*logsv1.LogRecord{{
					TimeUnixNano:         1_700_000_000_000_000_123,
					ObservedTimeUnixNano: 1_700_000_000_000_000_456,
					Body:                 &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "claude_code.api_request"}},
					TraceId:              []byte{0x0a, 0xf7, 0x65, 0x19, 0x16, 0xcd, 0x43, 0xdd, 0x84, 0x48, 0xeb, 0x21, 0x1c, 0x80, 0x31, 0x9c},
					SpanId:               []byte{0xb7, 0xad, 0x6b, 0x71, 0x69, 0x20, 0x33, 0x31},
					Attributes: []*commonv1.KeyValue{
						{Key: "session.id", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "session-1"}}},
						{Key: "event.name", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "api_request"}}},
						{Key: "input_tokens", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: 42}}},
						{Key: "cost_usd", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_DoubleValue{DoubleValue: 0.5}}},
					},
				}},
			}},
		}},
	}
}

func TestLogsForwardsExportToHooksSinkAfterPublish(t *testing.T) {
	t.Parallel()

	body, err := proto.Marshal(claudeSinkTestExport())
	require.NoError(t, err)

	events := &sinkEventLog{events: nil}
	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Return(&settledPublishResult{events: events}).Once()
	sink := &recordingHooksSink{events: events, logs: nil, metrics: nil}
	service := &Service{
		logger:          testenv.NewLogger(t),
		tracer:          testenv.NewTracerProvider(t).Tracer("test"),
		auth:            nil,
		authz:           nil,
		chRepo:          nil,
		logsEnabled:     nil,
		logPublisher:    publisher,
		metricPublisher: nil,
		spanPublisher:   nil,
		hooksSink:       sink,
	}
	ctx := contextvalues.SetAuthContext(t.Context(), testOTELAuthContext(uuid.MustParse(testLogProjectID)))

	err = service.Logs(ctx, &gen.LogsPayload{ApikeyToken: nil, ProjectSlugInput: nil, ContentEncoding: nil}, io.NopCloser(bytes.NewReader(body)))
	require.NoError(t, err)
	publisher.AssertExpectations(t)
	requireSinkAfterEveryPublish(t, events, 1)

	require.Len(t, sink.logs, 1)
	payload := sink.logs[0]
	require.Len(t, payload.ResourceLogs, 1)
	resourceLog := payload.ResourceLogs[0]
	require.Equal(t, "service.name", resourceLog.Resource.Attributes[0].Key)
	require.Equal(t, "claude-code", *resourceLog.Resource.Attributes[0].Value.StringValue)
	require.Len(t, resourceLog.ScopeLogs, 1)
	require.Equal(t, "com.anthropic.claude_code.events", *resourceLog.ScopeLogs[0].Scope.Name)
	require.Len(t, resourceLog.ScopeLogs[0].LogRecords, 1)
	record := resourceLog.ScopeLogs[0].LogRecords[0]

	// Same shapes the hooks endpoint decodes from OTLP/JSON producers.
	require.Equal(t, "1700000000000000123", *record.TimeUnixNano)
	require.Equal(t, "1700000000000000456", *record.ObservedTimeUnixNano)
	require.Equal(t, "0af7651916cd43dd8448eb211c80319c", *record.TraceID)
	require.Equal(t, "b7ad6b7169203331", *record.SpanID)
	require.Equal(t, "claude_code.api_request", *record.Body.StringValue)

	attrs := make(map[string]*hooksgen.OTELAttributeValue, len(record.Attributes))
	for _, item := range record.Attributes {
		attrs[item.Key] = item.Value
	}
	require.Equal(t, "session-1", *attrs["session.id"].StringValue)
	require.Equal(t, "api_request", *attrs["event.name"].StringValue)
	require.Equal(t, "42", attrs["input_tokens"].IntValue)
	require.InDelta(t, 0.5, *attrs["cost_usd"].DoubleValue, 0)
}

func TestLogsDoesNotForwardToHooksSinkWhenPublishFails(t *testing.T) {
	t.Parallel()

	body, err := proto.Marshal(claudeSinkTestExport())
	require.NoError(t, err)

	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewErrPublishResult(errors.New("pubsub unavailable"))).Once()
	sink := &recordingHooksSink{events: &sinkEventLog{events: nil}, logs: nil, metrics: nil}
	service := &Service{
		logger:          testenv.NewLogger(t),
		tracer:          testenv.NewTracerProvider(t).Tracer("test"),
		auth:            nil,
		authz:           nil,
		chRepo:          nil,
		logsEnabled:     nil,
		logPublisher:    publisher,
		metricPublisher: nil,
		spanPublisher:   nil,
		hooksSink:       sink,
	}
	ctx := contextvalues.SetAuthContext(t.Context(), testOTELAuthContext(uuid.MustParse(testLogProjectID)))

	err = service.Logs(ctx, &gen.LogsPayload{ApikeyToken: nil, ProjectSlugInput: nil, ContentEncoding: nil}, io.NopCloser(bytes.NewReader(body)))
	require.Error(t, err)
	publisher.AssertExpectations(t)
	require.Empty(t, sink.logs, "a failed publish is retried by the exporter; forwarding it would double-write telemetry")
}

func TestLogsWithoutHooksSinkStillPublishes(t *testing.T) {
	t.Parallel()

	body, err := proto.Marshal(claudeSinkTestExport())
	require.NoError(t, err)

	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult()).Once()
	service := &Service{
		logger:          testenv.NewLogger(t),
		tracer:          testenv.NewTracerProvider(t).Tracer("test"),
		auth:            nil,
		authz:           nil,
		chRepo:          nil,
		logsEnabled:     nil,
		logPublisher:    publisher,
		metricPublisher: nil,
		spanPublisher:   nil,
		hooksSink:       nil,
	}
	ctx := contextvalues.SetAuthContext(t.Context(), testOTELAuthContext(uuid.MustParse(testLogProjectID)))

	err = service.Logs(ctx, &gen.LogsPayload{ApikeyToken: nil, ProjectSlugInput: nil, ContentEncoding: nil}, io.NopCloser(bytes.NewReader(body)))
	require.NoError(t, err)
	publisher.AssertExpectations(t)
}

func TestMetricsForwardsExportToHooksSinkAfterPublish(t *testing.T) {
	t.Parallel()

	request := metricRelayTestExport()
	body, err := proto.Marshal(request)
	require.NoError(t, err)

	events := &sinkEventLog{events: nil}
	publisher := gcp.NewMockPublisher[*otelv1.InboundMetric]()
	publisher.On("Publish", mock.Anything, mock.Anything).Return(&settledPublishResult{events: events}).Once()
	sink := &recordingHooksSink{events: events, logs: nil, metrics: nil}
	service := &Service{
		logger:          testenv.NewLogger(t),
		tracer:          testenv.NewTracerProvider(t).Tracer("test"),
		auth:            nil,
		authz:           nil,
		chRepo:          nil,
		logsEnabled:     nil,
		logPublisher:    nil,
		metricPublisher: publisher,
		spanPublisher:   nil,
		hooksSink:       sink,
	}
	ctx := contextvalues.SetAuthContext(t.Context(), testOTELAuthContext(uuid.MustParse(testMetricProjectID)))

	err = service.Metrics(ctx, &gen.MetricsPayload{ApikeyToken: nil, ProjectSlugInput: nil, ContentEncoding: nil}, io.NopCloser(bytes.NewReader(body)))
	require.NoError(t, err)
	publisher.AssertExpectations(t)
	requireSinkAfterEveryPublish(t, events, 1)

	require.Len(t, sink.metrics, 1)
	payload := sink.metrics[0]
	require.Len(t, payload.ResourceMetrics, 1)
	require.Equal(t, "producer", *payload.ResourceMetrics[0].Resource.Attributes[0].Value.StringValue)
	require.Len(t, payload.ResourceMetrics[0].ScopeMetrics, 1)
	metrics := payload.ResourceMetrics[0].ScopeMetrics[0].Metrics
	require.NotEmpty(t, metrics)
	require.Equal(t, request.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].Name, *metrics[0].Name)
}

func TestHooksLogsPayloadDropsMalformedIDs(t *testing.T) {
	t.Parallel()

	export := claudeSinkTestExport()
	export.ResourceLogs[0].ScopeLogs[0].LogRecords[0].TraceId = nil
	export.ResourceLogs[0].ScopeLogs[0].LogRecords[0].SpanId = nil

	payload, err := hooksLogsPayload(export)
	require.NoError(t, err)
	record := payload.ResourceLogs[0].ScopeLogs[0].LogRecords[0]
	require.Nil(t, record.TraceID)
	require.Nil(t, record.SpanID)
}

func TestHooksLogsPayloadKeepsUndecodableIDs(t *testing.T) {
	t.Parallel()

	export := claudeSinkTestExport()
	// A 3-byte id is not an OTLP id; the hooks writers store what they get,
	// as they do for a malformed hex id on the JSON edge.
	export.ResourceLogs[0].ScopeLogs[0].LogRecords[0].TraceId = []byte{1, 2, 3}

	payload, err := hooksLogsPayload(export)
	require.NoError(t, err)
	require.Equal(t, "010203", *payload.ResourceLogs[0].ScopeLogs[0].LogRecords[0].TraceID)
}

func TestHooksMetricsPayloadRendersClaudeUsageShapes(t *testing.T) {
	t.Parallel()

	export := &collectormetricsv1.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricsv1.ResourceMetrics{{
			Resource: &resourcev1.Resource{Attributes: []*commonv1.KeyValue{{
				Key:   "service.name",
				Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "claude-code"}},
			}}},
			ScopeMetrics: []*metricsv1.ScopeMetrics{{
				Metrics: []*metricsv1.Metric{{
					Name: "claude_code.token.usage",
					Data: &metricsv1.Metric_Sum{Sum: &metricsv1.Sum{
						AggregationTemporality: metricsv1.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
						IsMonotonic:            true,
						DataPoints: []*metricsv1.NumberDataPoint{{
							TimeUnixNano: 1_700_000_000_000_000_000,
							Value:        &metricsv1.NumberDataPoint_AsInt{AsInt: 1234},
							Attributes: []*commonv1.KeyValue{
								{Key: "session.id", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "session-1"}}},
								{Key: "type", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "input"}}},
							},
						}},
					}},
				}},
			}},
		}},
	}

	payload, err := hooksMetricsPayload(export)
	require.NoError(t, err)
	metric := payload.ResourceMetrics[0].ScopeMetrics[0].Metrics[0]
	require.Equal(t, "claude_code.token.usage", *metric.Name)
	require.NotNil(t, metric.Sum)
	// The hooks extractor accepts the enum name and string-encoded ints.
	require.Equal(t, "AGGREGATION_TEMPORALITY_DELTA", metric.Sum.AggregationTemporality)
	require.Equal(t, "1234", metric.Sum.DataPoints[0].AsInt)
	require.Equal(t, "1700000000000000000", *metric.Sum.DataPoints[0].TimeUnixNano)
}

func TestMetricsDoesNotForwardToHooksSinkWhenPublishFails(t *testing.T) {
	t.Parallel()

	body, err := proto.Marshal(metricRelayTestExport())
	require.NoError(t, err)

	publisher := gcp.NewMockPublisher[*otelv1.InboundMetric]()
	publisher.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewErrPublishResult(errors.New("pubsub unavailable"))).Once()
	sink := &recordingHooksSink{events: &sinkEventLog{events: nil}, logs: nil, metrics: nil}
	service := &Service{
		logger:          testenv.NewLogger(t),
		tracer:          testenv.NewTracerProvider(t).Tracer("test"),
		auth:            nil,
		authz:           nil,
		chRepo:          nil,
		logsEnabled:     nil,
		logPublisher:    nil,
		metricPublisher: publisher,
		spanPublisher:   nil,
		hooksSink:       sink,
	}
	ctx := contextvalues.SetAuthContext(t.Context(), testOTELAuthContext(uuid.MustParse(testMetricProjectID)))

	err = service.Metrics(ctx, &gen.MetricsPayload{ApikeyToken: nil, ProjectSlugInput: nil, ContentEncoding: nil}, io.NopCloser(bytes.NewReader(body)))
	require.Error(t, err)
	publisher.AssertExpectations(t)
	require.Empty(t, sink.metrics)
}

func TestHooksPayloadsClearNonFiniteDoubles(t *testing.T) {
	t.Parallel()

	logs := claudeSinkTestExport()
	logs.ResourceLogs[0].ScopeLogs[0].LogRecords[0].Attributes = append(
		logs.ResourceLogs[0].ScopeLogs[0].LogRecords[0].Attributes,
		&commonv1.KeyValue{Key: "ratio", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_DoubleValue{DoubleValue: math.Inf(1)}}},
	)
	logsPayload, err := hooksLogsPayload(logs)
	require.NoError(t, err)
	attrs := logsPayload.ResourceLogs[0].ScopeLogs[0].LogRecords[0].Attributes
	require.Len(t, attrs, 5)
	require.InDelta(t, 0.5, *attrs[3].Value.DoubleValue, 0)
	require.Equal(t, "ratio", attrs[4].Key)
	require.Nil(t, attrs[4].Value.DoubleValue)

	metrics := &collectormetricsv1.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricsv1.ResourceMetrics{{
			Resource: &resourcev1.Resource{Attributes: []*commonv1.KeyValue{{
				Key:   "service.name",
				Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "claude-code"}},
			}}},
			ScopeMetrics: []*metricsv1.ScopeMetrics{{
				Metrics: []*metricsv1.Metric{
					{
						Name: "claude_code.cost.usage",
						Data: &metricsv1.Metric_Sum{Sum: &metricsv1.Sum{
							AggregationTemporality: metricsv1.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
							DataPoints: []*metricsv1.NumberDataPoint{
								{TimeUnixNano: 1, Value: &metricsv1.NumberDataPoint_AsDouble{AsDouble: math.NaN()}},
								{TimeUnixNano: 2, Value: &metricsv1.NumberDataPoint_AsDouble{AsDouble: 0.25}},
							},
						}},
					},
					{
						Name: "claude_code.latency",
						Data: &metricsv1.Metric_Histogram{Histogram: &metricsv1.Histogram{
							AggregationTemporality: metricsv1.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
							DataPoints: []*metricsv1.HistogramDataPoint{{
								TimeUnixNano:   3,
								Count:          2,
								Sum:            new(math.Inf(-1)),
								BucketCounts:   []uint64{1, 1},
								ExplicitBounds: []float64{1, math.Inf(1)},
							}, {
								TimeUnixNano:   4,
								Count:          2,
								Sum:            new(3.0),
								BucketCounts:   []uint64{1, 1},
								ExplicitBounds: []float64{1},
							}},
						}},
					},
				},
			}},
		}},
	}
	metricsPayload, err := hooksMetricsPayload(metrics)
	require.NoError(t, err)
	got := metricsPayload.ResourceMetrics[0].ScopeMetrics[0].Metrics
	require.Len(t, got, 2)
	require.Len(t, got[0].Sum.DataPoints, 2)
	require.Nil(t, got[0].Sum.DataPoints[0].AsDouble)
	require.InDelta(t, 0.25, *got[0].Sum.DataPoints[1].AsDouble, 0)
	histogram, ok := got[1].Histogram.(map[string]any)
	require.True(t, ok)
	points, ok := histogram["dataPoints"].([]any)
	require.True(t, ok)
	// The point with a non-finite bound is dropped whole: removing one bound
	// would leave bucketCounts one element too long.
	require.Len(t, points, 1)
	point, ok := points[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "4", point["timeUnixNano"])
	require.InDelta(t, 3, point["sum"], 0)
	require.Equal(t, []any{float64(1)}, point["explicitBounds"])
	require.Len(t, point["bucketCounts"], 2)
}
