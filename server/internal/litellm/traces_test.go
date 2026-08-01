package litellm

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type traceTestAuthorizer struct {
	authCtx *contextvalues.AuthContext
	key     string
	project string
	mu      sync.Mutex
	schemes []string
}

func (a *traceTestAuthorizer) Authorize(ctx context.Context, value string, scheme *security.APIKeyScheme) (context.Context, error) {
	a.mu.Lock()
	a.schemes = append(a.schemes, scheme.Name)
	a.mu.Unlock()
	switch scheme.Name {
	case constants.KeySecurityScheme:
		if value != a.key {
			return ctx, oops.E(oops.CodeUnauthorized, nil, "invalid API key")
		}
		return contextvalues.SetAuthContext(ctx, a.authCtx), nil
	case constants.ProjectSlugSecuritySchema:
		if value != a.project {
			return ctx, oops.E(oops.CodeForbidden, nil, "project does not match API key")
		}
		return ctx, nil
	default:
		return ctx, oops.E(oops.CodeUnauthorized, nil, "unsupported security scheme")
	}
}

func newTraceTestService(t *testing.T, authorizer authorizer, meterProvider metric.MeterProvider, logBulk func(context.Context, []telemetry.LogParams) error) (*Service, *TraceProcessor) {
	t.Helper()
	processor := newTraceProcessor(testenv.NewLogger(t), meterProvider, logBulk, traceProcessorWorkers, traceProcessorQueueSize)
	processor.Start(t.Context())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, processor.Shutdown(ctx))
	})
	return &Service{
		tracer: testenv.NewTracerProvider(t).Tracer("test"),
		logger: testenv.NewLogger(t),
		auth:   authorizer,
		hooks:  nil,
		calls:  nil,
		traces: processor,
	}, processor
}

func mountedTraceMux(service *Service) http.Handler {
	mux := goahttp.NewMuxer()
	Attach(mux, service)
	return mux
}

func serveTraceRequest(t *testing.T, mux http.Handler, body []byte, contentType, contentEncoding, key, project string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/rpc/litellm.otel/v1/traces", bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if contentEncoding != "" {
		req.Header.Set("Content-Encoding", contentEncoding)
	}
	if key != "" {
		req.Header.Set("Gram-Key", key)
	}
	if project != "" {
		req.Header.Set("Gram-Project", project)
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	return recorder
}

func gzipBody(t *testing.T, body []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write(body)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return compressed.Bytes()
}

func TestTraceHTTPAuthenticatesBeforeReadingBody(t *testing.T) {
	t.Parallel()

	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	service, _ := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	mux := mountedTraceMux(service)

	malformed := serveTraceRequest(t, mux, []byte("{"), "application/json", "", "", "project-test")
	require.Equal(t, http.StatusUnauthorized, malformed.Code)

	oversized := serveTraceRequest(t, mux, bytes.Repeat([]byte("x"), maxTraceBodyBytes+1), "application/json", "", "", "project-test")
	require.Equal(t, http.StatusUnauthorized, oversized.Code)

	wrongKey := serveTraceRequest(t, mux, []byte("{"), "application/json", "", "wrong-key", "project-test")
	require.Equal(t, http.StatusUnauthorized, wrongKey.Code)

	missingProject := serveTraceRequest(t, mux, []byte("{"), "application/json", "", "valid-key", "")
	require.Equal(t, http.StatusUnauthorized, missingProject.Code)

	mismatchedProject := serveTraceRequest(t, mux, []byte("{"), "application/json", "", "valid-key", "other-project")
	require.Equal(t, http.StatusForbidden, mismatchedProject.Code)

	authorizer.mu.Lock()
	require.Equal(t, []string{
		constants.KeySecurityScheme,
		constants.KeySecurityScheme,
		constants.KeySecurityScheme,
		constants.ProjectSlugSecuritySchema,
	}, authorizer.schemes)
	authorizer.mu.Unlock()
}

func TestTraceHTTPValidatesMediaEncodingAndBodyLimits(t *testing.T) {
	t.Parallel()

	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	service, _ := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	mux := mountedTraceMux(service)
	valid := []byte(`{"resourceSpans":[]}`)

	require.Equal(t, http.StatusBadRequest, serveTraceRequest(t, mux, []byte("{"), "application/json", "", "valid-key", "project-test").Code)
	require.Equal(t, http.StatusUnsupportedMediaType, serveTraceRequest(t, mux, valid, "text/plain", "", "valid-key", "project-test").Code)
	require.Equal(t, http.StatusUnsupportedMediaType, serveTraceRequest(t, mux, valid, "application/json", "br", "valid-key", "project-test").Code)
	require.Equal(t, http.StatusRequestEntityTooLarge, serveTraceRequest(t, mux, bytes.Repeat([]byte("x"), maxTraceBodyBytes+1), "application/json", "", "valid-key", "project-test").Code)
	require.Equal(t, http.StatusRequestEntityTooLarge, serveTraceRequest(t, mux, gzipBody(t, bytes.Repeat([]byte(" "), maxTraceBodyBytes+1)), "application/json", "gzip", "valid-key", "project-test").Code)

	req := httptest.NewRequest(http.MethodPost, "/rpc/litellm.otel/v1/traces", bytes.NewReader(valid))
	req.ContentLength = maxTraceBodyBytes + 100
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Gram-Key", "valid-key")
	req.Header.Set("Gram-Project", "project-test")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusAccepted, recorder.Code)
}

func TestTraceHTTPRejectsEncodingBeforeReadingBody(t *testing.T) {
	t.Parallel()

	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	service, _ := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	reader := &trackingReader{}
	req := httptest.NewRequest(http.MethodPost, "/rpc/litellm.otel/v1/traces", reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "br")
	req.Header.Set(constants.APIKeyHeader, "valid-key")
	req.Header.Set(constants.ProjectHeader, "project-test")
	recorder := httptest.NewRecorder()
	mountedTraceMux(service).ServeHTTP(recorder, req)
	require.Equal(t, http.StatusUnsupportedMediaType, recorder.Code)
	require.Zero(t, reader.reads)
}

type trackingReader struct {
	reads int
}

func (r *trackingReader) Read([]byte) (int, error) {
	r.reads++
	return 0, errors.New("body must not be read")
}

func TestTraceHTTPRejectsExportOverSpanLimit(t *testing.T) {
	t.Parallel()

	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	service, _ := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error {
		require.Fail(t, "oversized export must not reach LogBulk")
		return nil
	})
	var body strings.Builder
	body.WriteString(`{"resourceSpans":[{"scopeSpans":[{"spans":[`)
	for index := range maxOTLPSpansPerExport + 1 {
		if index > 0 {
			body.WriteByte(',')
		}
		body.WriteString(`{}`)
	}
	body.WriteString(`]}]}]}`)
	response := serveTraceRequest(t, mountedTraceMux(service), []byte(body.String()), "application/json", "", "valid-key", "project-test")
	require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
}

func TestTraceHTTPRejectsNestedAnyValueWithMultipleFields(t *testing.T) {
	t.Parallel()

	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	service, _ := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	body := []byte(`{"resourceSpans":[{"scopeSpans":[{"spans":[{"attributes":[{"key":"unknown","value":{"arrayValue":{"values":[{"stringValue":"one","intValue":"2"}]}}}]}]}]}]}`)
	response := serveTraceRequest(t, mountedTraceMux(service), body, "application/json", "", "valid-key", "project-test")
	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestTraceHTTPAcceptsJSONAndProtobufPlainAndGzip(t *testing.T) {
	t.Parallel()

	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	jobs := make(chan []telemetry.LogParams, 5)
	service, processor := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(_ context.Context, params []telemetry.LogParams) error {
		jobs <- params
		return nil
	})
	mux := mountedTraceMux(service)
	jsonFixture := testenv.ReadFixture(t, contractFixtureDir+"otlp-traces.json")
	protobufFixture := testenv.ReadFixture(t, contractFixtureDir+"otlp-traces.pb")

	require.Equal(t, http.StatusAccepted, serveTraceRequest(t, mux, jsonFixture, "application/json", "", "valid-key", "project-test").Code)
	require.Equal(t, http.StatusAccepted, serveTraceRequest(t, mux, gzipBody(t, jsonFixture), "application/json; charset=utf-8", "gzip", "valid-key", "project-test").Code)
	require.Equal(t, http.StatusAccepted, serveTraceRequest(t, mux, protobufFixture, "application/x-protobuf", "", "valid-key", "project-test").Code)
	require.Equal(t, http.StatusAccepted, serveTraceRequest(t, mux, gzipBody(t, protobufFixture), "application/x-protobuf", "gzip", "valid-key", "project-test").Code)
	require.Equal(t, http.StatusAccepted, serveTraceRequest(t, mux, protobufFixture, "application/protobuf", "", "valid-key", "project-test").Code)
	require.NoError(t, processor.Shutdown(t.Context()))
	require.Len(t, jobs, 5)

	for range 5 {
		params := <-jobs
		require.Len(t, params, 1)
		require.Equal(t, "0123456789abcdef0123456789abcdef", params[0].Attributes["trace.id"])
		require.Equal(t, "fixture-logical-trace", params[0].Attributes["gram.litellm.trace_id"])
	}
}

func TestTraceLogParamsUsesObservedTimeForMissingOrOverflowingStartTime(t *testing.T) {
	t.Parallel()

	service, processor := newTraceTestService(t, fixedAuthorizer{authCtx: testAuthContext()}, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	request := &otlpExportRequest{ResourceSpans: []otlpResourceSpans{{
		Resource: nil,
		ScopeSpans: []otlpScopeSpans{{
			Scope: nil,
			Spans: []otlpSpan{
				{TraceID: "", SpanID: "", ParentSpanID: "", Name: "missing timestamp", Kind: 0, StartTimeUnixNano: 0, EndTimeUnixNano: 0, Attributes: nil, DroppedAttributesCount: 0, Status: nil},
				{TraceID: "", SpanID: "", ParentSpanID: "", Name: "overflowing timestamp", Kind: 0, StartTimeUnixNano: jsonUint64(math.MaxUint64), EndTimeUnixNano: 0, Attributes: nil, DroppedAttributesCount: 0, Status: nil},
			},
		}},
	}}}
	before := time.Now().UTC()
	params := service.traceLogParams(t.Context(), request, "org-test", uuid.NewString())
	after := time.Now().UTC()

	require.Len(t, params, 2)
	for _, param := range params {
		require.WithinRange(t, param.Timestamp, before, after)
		require.NotContains(t, param.Attributes, "otel.span.duration_ms")
	}
	require.Equal(t, uint64(math.MaxUint64), params[1].Attributes["otel.span.start_time_unix_nano"])
	require.NoError(t, processor.Shutdown(t.Context()))
}

func TestTraceProcessorQueueSaturationDropsWithoutBlocking(t *testing.T) {
	t.Parallel()
	require.Equal(t, 4, traceProcessorWorkers)
	require.Equal(t, 16, traceProcessorQueueSize)
	require.Equal(t, 256, maxOTLPSpansPerExport)

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(t.Context())) })
	entered := make(chan struct{}, traceProcessorWorkers)
	release := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	processor := newTraceProcessor(testenv.NewLogger(t), meterProvider, func(context.Context, []telemetry.LogParams) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		mu.Lock()
		calls++
		mu.Unlock()
		return nil
	}, traceProcessorWorkers, traceProcessorQueueSize)
	processor.Start(t.Context())

	span := []telemetry.LogParams{{
		Timestamp: time.Unix(0, 1),
		ToolInfo: telemetry.ToolInfo{
			ID: "", URN: litellmOTLPResourceURN, Name: "litellm", ProjectID: uuid.NewString(), DeploymentID: "", FunctionID: nil, OrganizationID: "org-test",
		},
		UserInfo:   telemetry.UserInfoByID(""),
		Attributes: map[attr.Key]any{},
	}}
	for range traceProcessorWorkers {
		require.True(t, processor.Enqueue(t.Context(), span))
	}
	for range traceProcessorWorkers {
		<-entered
	}
	for range traceProcessorQueueSize {
		require.True(t, processor.Enqueue(t.Context(), span))
	}
	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	service := &Service{
		tracer: testenv.NewTracerProvider(t).Tracer("test"),
		logger: testenv.NewLogger(t),
		auth:   authorizer,
		hooks:  nil,
		calls:  nil,
		traces: processor,
	}
	response := serveTraceRequest(t, mountedTraceMux(service), []byte(`{"resourceSpans":[{"scopeSpans":[{"spans":[{"traceId":"0123456789abcdef0123456789abcdef","spanId":"0123456789abcdef","name":"queue test"}]}]}]}`), "application/json", "", "valid-key", "project-test")
	require.Equal(t, http.StatusAccepted, response.Code)
	require.EqualValues(t, 1, metricCounterValue(t, reader, "litellm.otel.spans.dropped"))

	close(release)
	require.NoError(t, processor.Shutdown(t.Context()))
	mu.Lock()
	require.Equal(t, traceProcessorWorkers+traceProcessorQueueSize, calls)
	mu.Unlock()
}

func TestTraceProcessorShutdownRetriesWaitForWorkerCompletion(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	processor := newTraceProcessor(testenv.NewLogger(t), testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error {
		entered <- struct{}{}
		<-release
		return nil
	}, 1, 1)
	processor.Start(t.Context())
	require.True(t, processor.Enqueue(t.Context(), []telemetry.LogParams{}))
	<-entered

	timedOut, cancel := context.WithTimeout(t.Context(), 0)
	defer cancel()
	require.ErrorIs(t, processor.Shutdown(timedOut), context.DeadlineExceeded)
	close(release)
	require.NoError(t, processor.Shutdown(t.Context()))
}

func TestTraceProcessorRecordsPersistenceFailuresBySpanCount(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(t.Context())) })
	wantErr := errors.New("persistence unavailable")
	processor := newTraceProcessor(testenv.NewLogger(t), meterProvider, func(context.Context, []telemetry.LogParams) error {
		return wantErr
	}, 1, 1)
	processor.Start(t.Context())

	require.True(t, processor.Enqueue(t.Context(), make([]telemetry.LogParams, 3)))
	require.NoError(t, processor.Shutdown(t.Context()))
	require.EqualValues(t, 3, metricCounterValue(t, reader, "litellm.otel.spans.persistence_failed"))
}

func metricCounterValue(t *testing.T, reader *sdkmetric.ManualReader, name string) int64 {
	t.Helper()
	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &metrics))
	for _, scope := range metrics.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != name {
				continue
			}
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			var result int64
			for _, point := range sum.DataPoints {
				result += point.Value
			}
			return result
		}
	}
	require.Fail(t, "metric not found", name)
	return 0
}

func TestTracePersistenceKeepsOnlyAllowlistedAttributes(t *testing.T) {
	t.Parallel()

	ctx, instance := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	instance.service.auth = fixedAuthorizer{authCtx: authCtx}
	mux := mountedTraceMux(instance.service)
	jsonFixture := testenv.ReadFixture(t, contractFixtureDir+"otlp-traces.json")
	protobufFixture := testenv.ReadFixture(t, contractFixtureDir+"otlp-traces.pb")

	require.Equal(t, http.StatusAccepted, serveTraceRequest(t, mux, jsonFixture, "application/json", "", "fixture-key", "fixture-project").Code)
	require.Equal(t, http.StatusAccepted, serveTraceRequest(t, mux, gzipBody(t, protobufFixture), "application/x-protobuf", "gzip", "fixture-key", "fixture-project").Code)
	invalidBatch := []byte(`{"resourceSpans":[{"scopeSpans":[{"spans":[{"traceId":"0123456789abcdef0123456789abcdef","spanId":"fedcba9876543210","name":"valid sibling","startTimeUnixNano":"1785542401000000000","endTimeUnixNano":"1785542401000000001"},{"traceId":"not-a-trace-id","spanId":"bad-span","parentSpanId":"bad-parent","name":"invalid ids","startTimeUnixNano":"1785542402000000000","endTimeUnixNano":"1785542402000000001"}]}]}]}`)
	require.Equal(t, http.StatusAccepted, serveTraceRequest(t, mux, invalidBatch, "application/json", "", "fixture-key", "fixture-project").Code)
	require.NoError(t, instance.service.traces.Shutdown(t.Context()))

	query := telemetryrepo.New(instance.chConn)
	var logs []telemetryrepo.TelemetryLog
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		testenv.FlushClickHouseAsyncInserts(t, instance.chConn)
		var err error
		logs, err = query.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC).UnixNano(),
			TimeEnd:       time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC).UnixNano(),
			GramURNs:      []string{litellmOTLPResourceURN},
			SortOrder:     "asc",
			Cursor:        "",
			Limit:         10,
		})
		assert.NoError(collect, err)
		assert.Len(collect, logs, 4)
	}, 10*time.Second, 50*time.Millisecond)

	invalidFound := false
	for _, log := range logs {
		require.Equal(t, authCtx.ProjectID.String(), log.GramProjectID)
		require.Equal(t, litellmOTLPResourceURN, log.GramURN)
		require.Equal(t, "litellm", gjson.Get(log.Attributes, "gram.hook.source").String())
		require.Equal(t, string(telemetry.EventSourceHook), gjson.Get(log.Attributes, "gram.event.source").String())
		require.Equal(t, litellmOTLPResourceURN, gjson.Get(log.Attributes, "gram.resource.urn").String())
		require.True(t, strings.HasPrefix(gjson.Get(log.Attributes, "gram.event.urn").String(), "urn:telemetry:provider_otel:span:"))
		for _, sensitive := range []string{
			"gen_ai.input.messages", "gen_ai.output.messages", "gen_ai.system_instructions", "gen_ai.tool.call.arguments", "gen_ai.tool.call.result",
			"http.request.header.authorization", "metadata.customer_payload", "fixture-sensitive-prompt", "fixture-sensitive-output", "fixture-sensitive-system",
			"fixture-sensitive-tool-args", "fixture-sensitive-tool-result", "fixture-sensitive-header", "fixture-sensitive-metadata", "raw.provider.payload", "fixture-sensitive-resource",
		} {
			require.NotContains(t, log.Attributes, sensitive)
			require.NotContains(t, log.ResourceAttributes, sensitive)
		}
		for _, path := range []string{
			"gen_ai.input.messages", "gen_ai.output.messages", "gen_ai.system_instructions", "gen_ai.tool.call.arguments", "gen_ai.tool.call.result",
			"http.request.header.authorization", "metadata.customer_payload",
		} {
			require.False(t, gjson.Get(log.Attributes, path).Exists(), path)
		}
		require.False(t, gjson.Get(log.ResourceAttributes, "raw.provider.payload").Exists())
		if gjson.Get(log.Attributes, "otel.span.name").String() == "invalid ids" {
			invalidFound = true
			require.Nil(t, log.TraceID)
			require.Nil(t, log.SpanID)
		}
	}
	require.True(t, invalidFound)

	require.Equal(t, "0123456789abcdef0123456789abcdef", gjson.Get(logs[0].Attributes, "trace.id").String())
	require.Equal(t, "fixture-logical-trace", gjson.Get(logs[0].Attributes, "gram.litellm.trace_id").String())
	require.Equal(t, "openai", gjson.Get(logs[0].Attributes, "gen_ai.provider.name").String())
	require.EqualValues(t, 11, gjson.Get(logs[0].Attributes, "gen_ai.usage.input_tokens").Int())
}

func TestOTLPAttributeLimitsAndRecursiveValues(t *testing.T) {
	t.Parallel()

	service, processor := newTraceTestService(t, fixedAuthorizer{authCtx: testAuthContext()}, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	values := make([]otlpKeyValue, maxOTLPAttributes+1)
	for index := range maxOTLPAttributes {
		value := "ignored"
		values[index] = otlpKeyValue{Key: "unknown.attribute", Value: otlpAnyValue{StringValue: &value}}
	}
	model := "must-be-dropped-after-limit"
	values[maxOTLPAttributes] = otlpKeyValue{Key: "gen_ai.request.model", Value: otlpAnyValue{StringValue: &model}}
	result := service.sanitizeOTLPAttributes(t.Context(), values, spanAttributeAllowlist)
	require.NotContains(t, result, "gen_ai.request.model")

	overlong := strings.Repeat("x", maxOTLPAttributeBytes)
	bounded := service.sanitizeOTLPAttributes(t.Context(), []otlpKeyValue{{Key: "gen_ai.operation.name", Value: otlpAnyValue{StringValue: &overlong}}}, spanAttributeAllowlist)
	require.NotContains(t, bounded, "gen_ai.operation.name")

	finish := "stop"
	recursive := service.sanitizeOTLPAttributes(t.Context(), []otlpKeyValue{{
		Key:   "gen_ai.response.finish_reasons",
		Value: otlpAnyValue{ArrayValue: &otlpArrayValue{Values: []otlpAnyValue{{StringValue: &finish}}}},
	}}, spanAttributeAllowlist)
	require.Equal(t, []any{"stop"}, recursive["gen_ai.response.finish_reasons"])

	nonFinite, err := decodeOTLPJSON([]byte(`{"resourceSpans":[{"scopeSpans":[{"spans":[{"attributes":[{"key":"gen_ai.usage.cost","value":{"doubleValue":"NaN"}}]}]}]}]}`))
	require.NoError(t, err)
	params := service.traceLogParams(t.Context(), nonFinite, "org-test", uuid.NewString())
	require.Len(t, params, 1)
	require.NotContains(t, params[0].Attributes, "gen_ai.usage.cost")

	aliasValues := map[string]string{
		"metadata.user_api_key_user_id":     "alias-user",
		"metadata.user_api_key_user_email":  "alias@example.test",
		"metadata.user_api_key_team_id":     "alias-team",
		"metadata.user_api_key_team_alias":  "alias-team-name",
		"metadata.user_api_key_org_id":      "alias-org",
		"metadata.user_api_key_end_user_id": "alias-end-user",
		"metadata.unknown":                  "must-drop",
	}
	aliasAttrs := make([]otlpKeyValue, 0, len(aliasValues))
	for key, value := range aliasValues {
		current := value
		aliasAttrs = append(aliasAttrs, otlpKeyValue{Key: key, Value: otlpAnyValue{StringValue: &current}})
	}
	aliases := service.sanitizeOTLPAttributes(t.Context(), aliasAttrs, spanAttributeAllowlist)
	require.Equal(t, "alias-user", aliases[attr.LiteLLMUserIDKey])
	require.Equal(t, "alias@example.test", aliases[attr.LiteLLMUserEmailKey])
	require.Equal(t, "alias-team", aliases[attr.LiteLLMTeamIDKey])
	require.Equal(t, "alias-team-name", aliases[attr.LiteLLMTeamAliasKey])
	require.Equal(t, "alias-org", aliases[attr.LiteLLMOrganizationIDKey])
	require.Equal(t, "alias-end-user", aliases[attr.LiteLLMEndUserIDKey])
	require.NotContains(t, aliases, attr.UserEmailKey)
	require.NotContains(t, aliases, "metadata.unknown")

	request := &otlpExportRequest{ResourceSpans: []otlpResourceSpans{{
		Resource: nil,
		ScopeSpans: []otlpScopeSpans{{
			Scope: nil,
			Spans: []otlpSpan{{
				TraceID: "0123456789abcdef0123456789abcdef", SpanID: "0123456789abcdef", ParentSpanID: "", Name: "arbitrary high-cardinality span", Kind: 0,
				StartTimeUnixNano: 0, EndTimeUnixNano: 0, Attributes: nil, DroppedAttributesCount: 0, Status: nil,
			}},
		}},
	}}}
	unknownOperation := service.traceLogParams(t.Context(), request, "org-test", uuid.NewString())
	require.Len(t, unknownOperation, 1)
	require.Equal(t, "urn:telemetry:provider_otel:span:unknown", unknownOperation[0].Attributes[attr.EventURNKey])
	require.Equal(t, "arbitrary high-cardinality span", unknownOperation[0].Attributes["otel.span.name"])

	_, validID := normalizeOTLPID(strings.Repeat("0", 32), 32)
	require.False(t, validID)
	_, validID = normalizeOTLPID(strings.Repeat("0", 16), 16)
	require.False(t, validID)
	require.NoError(t, processor.Shutdown(t.Context()))
}

func TestOTLPAttributeTypeAllowlistRejectsNestedValues(t *testing.T) {
	t.Parallel()

	service, processor := newTraceTestService(t, fixedAuthorizer{authCtx: testAuthContext()}, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	sensitive := "fixture-sensitive-nested"
	encodedSensitive := "Zml4dHVyZS1zZW5zaXRpdmUtbmVzdGVk"
	nested := otlpAnyValue{KvlistValue: &otlpKeyValueList{Values: []otlpKeyValue{{Key: "content", Value: otlpAnyValue{StringValue: &sensitive}}}}}
	result := service.sanitizeOTLPAttributes(t.Context(), []otlpKeyValue{
		{Key: "gen_ai.request.model", Value: otlpAnyValue{ArrayValue: &otlpArrayValue{Values: []otlpAnyValue{{StringValue: &sensitive}}}}},
		{Key: "gen_ai.usage.cost", Value: nested},
		{Key: "gen_ai.usage.input_tokens", Value: otlpAnyValue{BytesValue: &encodedSensitive}},
		{Key: "gen_ai.request.is_streaming", Value: otlpAnyValue{ArrayValue: &otlpArrayValue{Values: []otlpAnyValue{nested}}}},
		{Key: "gen_ai.response.finish_reasons", Value: otlpAnyValue{ArrayValue: &otlpArrayValue{Values: []otlpAnyValue{nested}}}},
	}, spanAttributeAllowlist)
	require.Empty(t, result)

	resources := service.sanitizeOTLPAttributes(t.Context(), []otlpKeyValue{{
		Key: "service.name", Value: otlpAnyValue{KvlistValue: &otlpKeyValueList{Values: []otlpKeyValue{{Key: "content", Value: otlpAnyValue{StringValue: &sensitive}}}}},
	}}, resourceAttributeAllowlist)
	require.Empty(t, resources)
	require.NoError(t, processor.Shutdown(t.Context()))
}

func TestOTLPAttributeTypeAllowlistAcceptsSafeValues(t *testing.T) {
	t.Parallel()

	service, processor := newTraceTestService(t, fixedAuthorizer{authCtx: testAuthContext()}, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	model := "safe-model"
	finishReason := "stop"
	streaming := true
	tokens := jsonInt64(42)
	cost := jsonFloat64(0.125)
	result := service.sanitizeOTLPAttributes(t.Context(), []otlpKeyValue{
		{Key: "gen_ai.request.model", Value: otlpAnyValue{StringValue: &model}},
		{Key: "gen_ai.response.finish_reasons", Value: otlpAnyValue{ArrayValue: &otlpArrayValue{Values: []otlpAnyValue{{StringValue: &finishReason}}}}},
		{Key: "gen_ai.request.is_streaming", Value: otlpAnyValue{BoolValue: &streaming}},
		{Key: "gen_ai.usage.input_tokens", Value: otlpAnyValue{IntValue: &tokens}},
		{Key: "gen_ai.usage.cost", Value: otlpAnyValue{DoubleValue: &cost}},
	}, spanAttributeAllowlist)
	require.Equal(t, "safe-model", result[attr.GenAIRequestModelKey])
	require.Equal(t, []any{"stop"}, result[attr.GenAIResponseFinishReasonsKey])
	require.Equal(t, true, result["gen_ai.request.is_streaming"])
	require.Equal(t, int64(42), result[attr.GenAIUsageInputTokensKey])
	require.InDelta(t, 0.125, result[attr.GenAIUsageCostKey], 1e-12)
	require.NoError(t, processor.Shutdown(t.Context()))
}

func TestOTLPAttributeTypeAllowlistRejectsNegativeUsageAndAcceptsZero(t *testing.T) {
	t.Parallel()

	service, processor := newTraceTestService(t, fixedAuthorizer{authCtx: testAuthContext()}, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	negativeTokens := jsonInt64(-1)
	negativeCost := jsonFloat64(-0.125)
	negativeIntegerCost := jsonInt64(-1)
	negative := service.sanitizeOTLPAttributes(t.Context(), []otlpKeyValue{
		{Key: "gen_ai.usage.input_tokens", Value: otlpAnyValue{IntValue: &negativeTokens}},
		{Key: "gen_ai.usage.cost", Value: otlpAnyValue{DoubleValue: &negativeCost}},
		{Key: "litellm.response.cost", Value: otlpAnyValue{IntValue: &negativeIntegerCost}},
	}, spanAttributeAllowlist)
	require.Empty(t, negative)

	zeroTokens := jsonInt64(0)
	zeroCost := jsonFloat64(0)
	zeroIntegerCost := jsonInt64(0)
	zero := service.sanitizeOTLPAttributes(t.Context(), []otlpKeyValue{
		{Key: "gen_ai.usage.input_tokens", Value: otlpAnyValue{IntValue: &zeroTokens}},
		{Key: "gen_ai.usage.cost", Value: otlpAnyValue{DoubleValue: &zeroCost}},
		{Key: "litellm.response.cost", Value: otlpAnyValue{IntValue: &zeroIntegerCost}},
	}, spanAttributeAllowlist)
	require.Equal(t, int64(0), zero[attr.GenAIUsageInputTokensKey])
	require.InDelta(t, 0, zero[attr.GenAIUsageCostKey], 0)
	require.Equal(t, int64(0), zero["litellm.response.cost"])
	require.NoError(t, processor.Shutdown(t.Context()))
}
