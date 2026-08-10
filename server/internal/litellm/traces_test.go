package litellm

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/litellm/callcache"
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
	metricProcessor := newMetricProcessor(testenv.NewLogger(t), meterProvider, logBulk, traceProcessorWorkers, traceProcessorQueueSize)
	resolver := NewInstanceResolver(testenv.NewLogger(t), nil)
	processor.SetInstanceResolver(resolver)
	metricProcessor.SetInstanceResolver(resolver)
	processor.Start(t.Context())
	metricProcessor.Start(t.Context())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, processor.Shutdown(ctx))
		require.NoError(t, metricProcessor.Shutdown(ctx))
	})
	return &Service{
		tracer:    testenv.NewTracerProvider(t).Tracer("test"),
		logger:    testenv.NewLogger(t),
		auth:      authorizer,
		hooks:     nil,
		calls:     nil,
		traces:    processor,
		metrics:   metricProcessor,
		health:    newDisabledHealthProcessor(t),
		db:        nil,
		telemetry: nil,
		instances: resolver,
		authz:     nil,
		audit:     nil,
		keyPrefix: "",
	}, processor
}

func mountedTraceMux(service *Service) http.Handler {
	mux := goahttp.NewMuxer()
	Attach(mux, service)
	return mux
}

func serveTraceRequest(t *testing.T, mux http.Handler, body []byte, contentType, contentEncoding, key, project string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/rpc/hooks.otel/v1/traces", bytes.NewReader(body))
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

func repeatedJSONObjects(count int) string {
	return strings.TrimSuffix(strings.Repeat("{},", count), ",")
}

func appendProtobufMessages(data []byte, field protowire.Number, message []byte, count int) []byte {
	for range count {
		data = protowire.AppendTag(data, field, protowire.BytesType)
		data = protowire.AppendBytes(data, message)
	}
	return data
}

func jsonArrayAnyValue(count int) string {
	return `{"arrayValue":{"values":[` + repeatedJSONObjects(count) + `]}}`
}

func nestedJSONAnyValue(depth int) string {
	value := `{}`
	for level := 1; level < depth; level++ {
		if level%2 == 0 {
			value = `{"kvlistValue":{"values":[{"value":` + value + `}]}}`
		} else {
			value = `{"arrayValue":{"values":[` + value + `]}}`
		}
	}
	return value
}

func jsonTraceWithAnyValues(resourceValue, scopeValue, spanValue string) []byte {
	return []byte(`{"resourceSpans":[{"resource":{"attributes":[{"key":"unknown.resource","value":` + resourceValue + `}]},"scopeSpans":[{"scope":{"attributes":[{"key":"unknown.scope","value":` + scopeValue + `}]},"spans":[{"attributes":[{"key":"unknown.span","value":` + spanValue + `}]}]}]}]}`)
}

func repeatedJSONKeyValues(count int) string {
	return strings.TrimSuffix(strings.Repeat(`{"key":"unknown","value":{}},`, count), ",")
}

func jsonTraceWithAttributeCounts(resourceCount, scopeCount, spanCount int) []byte {
	return []byte(`{"resourceSpans":[{"resource":{"attributes":[` + repeatedJSONKeyValues(resourceCount) + `]},"scopeSpans":[{"scope":{"attributes":[` + repeatedJSONKeyValues(scopeCount) + `]},"spans":[{"attributes":[` + repeatedJSONKeyValues(spanCount) + `]}]}]}]}`)
}

func protobufArrayAnyValue(count int) []byte {
	return appendProtobufMessages(nil, 5, appendProtobufMessages(nil, 1, nil, count), 1)
}

func nestedProtobufAnyValue(depth int) []byte {
	var value []byte
	for level := 1; level < depth; level++ {
		if level%2 == 0 {
			keyValue := appendProtobufMessages(nil, 2, value, 1)
			value = appendProtobufMessages(nil, 6, appendProtobufMessages(nil, 1, keyValue, 1), 1)
		} else {
			value = appendProtobufMessages(nil, 5, appendProtobufMessages(nil, 1, value, 1), 1)
		}
	}
	return value
}

func protobufTraceWithAnyValues(resourceValue, scopeValue, spanValue []byte) []byte {
	resource := appendProtobufMessages(nil, 1, appendProtobufMessages(nil, 2, resourceValue, 1), 1)
	scope := appendProtobufMessages(nil, 3, appendProtobufMessages(nil, 2, scopeValue, 1), 1)
	span := appendProtobufMessages(nil, 9, appendProtobufMessages(nil, 2, spanValue, 1), 1)
	scopeSpans := appendProtobufMessages(nil, 1, scope, 1)
	scopeSpans = appendProtobufMessages(scopeSpans, 2, span, 1)
	resourceSpans := appendProtobufMessages(nil, 1, resource, 1)
	resourceSpans = appendProtobufMessages(resourceSpans, 2, scopeSpans, 1)
	return appendProtobufMessages(nil, 1, resourceSpans, 1)
}

func protobufTraceWithAttributeCounts(resourceCount, scopeCount, spanCount int) []byte {
	resource := appendProtobufMessages(nil, 1, nil, resourceCount)
	scope := appendProtobufMessages(nil, 3, nil, scopeCount)
	span := appendProtobufMessages(nil, 9, nil, spanCount)
	scopeSpans := appendProtobufMessages(nil, 1, scope, 1)
	scopeSpans = appendProtobufMessages(scopeSpans, 2, span, 1)
	resourceSpans := appendProtobufMessages(nil, 1, resource, 1)
	resourceSpans = appendProtobufMessages(resourceSpans, 2, scopeSpans, 1)
	return appendProtobufMessages(nil, 1, resourceSpans, 1)
}

func TestPreflightProtobufAggregatesMergedResourceAttributes(t *testing.T) {
	t.Parallel()

	resourceFragment := appendProtobufMessages(nil, 1, nil, maxOTLPAttributes/2+1)
	resourceSpans := appendProtobufMessages(nil, 1, resourceFragment, 2)
	require.ErrorContains(t, preflightOTLPProtobuf(appendProtobufMessages(nil, 1, resourceSpans, 1)), "too many KeyValues")
}

func TestPreflightProtobufAggregatesMergedScopeAttributes(t *testing.T) {
	t.Parallel()

	scopeFragment := appendProtobufMessages(nil, 3, nil, maxOTLPAttributes/2+1)
	scopeSpans := appendProtobufMessages(nil, 1, scopeFragment, 2)
	resourceSpans := appendProtobufMessages(nil, 2, scopeSpans, 1)
	require.ErrorContains(t, preflightOTLPProtobuf(appendProtobufMessages(nil, 1, resourceSpans, 1)), "too many KeyValues")
}

func TestTraceHTTPAuthenticatesBeforeReadingBody(t *testing.T) {
	t.Parallel()

	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	service, _ := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	mux := mountedTraceMux(service)

	malformed := serveTraceRequest(t, mux, []byte("{"), "application/json", "", "", "project-test")
	require.Equal(t, http.StatusUnauthorized, malformed.Code)

	oversized := serveTraceRequest(t, mux, bytes.Repeat([]byte("x"), maxOTLPBodyBytes+1), "application/json", "", "", "project-test")
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
	require.Equal(t, http.StatusUnsupportedMediaType, serveTraceRequest(t, mux, valid, "", "", "valid-key", "project-test").Code)
	require.Equal(t, http.StatusUnsupportedMediaType, serveTraceRequest(t, mux, valid, "text/plain", "", "valid-key", "project-test").Code)
	require.Equal(t, http.StatusUnsupportedMediaType, serveTraceRequest(t, mux, valid, "application/json", "br", "valid-key", "project-test").Code)
	require.Equal(t, http.StatusRequestEntityTooLarge, serveTraceRequest(t, mux, bytes.Repeat([]byte("x"), maxOTLPBodyBytes+1), "application/json", "", "valid-key", "project-test").Code)
	require.Equal(t, http.StatusRequestEntityTooLarge, serveTraceRequest(t, mux, gzipBody(t, bytes.Repeat([]byte(" "), maxOTLPBodyBytes+1)), "application/json", "gzip", "valid-key", "project-test").Code)
	malformedProtobuf := []byte{0x0a, 0x80}
	require.Error(t, preflightOTLPProtobuf(malformedProtobuf))
	require.Equal(t, http.StatusBadRequest, serveTraceRequest(t, mux, malformedProtobuf, "application/x-protobuf", "", "valid-key", "project-test").Code)

	req := httptest.NewRequest(http.MethodPost, "/rpc/hooks.otel/v1/traces", bytes.NewReader(valid))
	req.ContentLength = maxOTLPBodyBytes + 100
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Gram-Key", "valid-key")
	req.Header.Set("Gram-Project", "project-test")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusAccepted, recorder.Code)
}

func TestTraceHTTPRejectsRepeatedContentHeaders(t *testing.T) {
	t.Parallel()

	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	service, _ := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	mux := mountedTraceMux(service)
	for _, header := range []string{"Content-Type", "Content-Encoding"} {
		reader := &trackingReader{}
		req := httptest.NewRequest(http.MethodPost, "/rpc/hooks.otel/v1/traces", reader)
		req.Header.Set(constants.APIKeyHeader, "valid-key")
		req.Header.Set(constants.ProjectHeader, "project-test")
		if header == "Content-Encoding" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Add(header, "application/json")
		req.Header.Add(header, "gzip")
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)
		require.Equal(t, http.StatusBadRequest, recorder.Code, header)
		require.Zero(t, reader.reads, header)
	}
}

func TestTraceHTTPRejectsEncodingBeforeReadingBody(t *testing.T) {
	t.Parallel()

	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	service, _ := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	reader := &trackingReader{}
	req := httptest.NewRequest(http.MethodPost, "/rpc/hooks.otel/v1/traces", reader)
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

func TestTraceHTTPRejectsJSONCollectionsOverLimit(t *testing.T) {
	t.Parallel()

	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	service, _ := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error {
		require.Fail(t, "oversized export must not reach LogBulk")
		return nil
	})
	mux := mountedTraceMux(service)
	tests := []struct {
		name   string
		body   []byte
		status int
	}{
		{name: "resource groups", body: []byte(`{"resourceSpans":[` + repeatedJSONObjects(maxOTLPResourceGroups+1) + `]}`), status: http.StatusBadRequest},
		{name: "scope groups", body: []byte(`{"resourceSpans":[{"scopeSpans":[` + repeatedJSONObjects(maxOTLPScopeGroups+1) + `]}]}`), status: http.StatusBadRequest},
		{name: "spans", body: []byte(`{"resourceSpans":[{"scopeSpans":[{"spans":[` + repeatedJSONObjects(maxOTLPSpansPerExport+1) + `]}]}]}`), status: http.StatusRequestEntityTooLarge},
		{name: "case-insensitive spans", body: []byte(`{"ResourceSpans":[{"ScopeSpans":[{"Spans":[` + repeatedJSONObjects(maxOTLPSpansPerExport+1) + `]}]}]}`), status: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		require.Error(t, preflightOTLPJSON(test.body), test.name)
		response := serveTraceRequest(t, mux, test.body, "application/json", "", "valid-key", "project-test")
		require.Equal(t, test.status, response.Code, test.name)
	}
}

func TestTraceHTTPRejectsProtobufCollectionsOverLimit(t *testing.T) {
	t.Parallel()

	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	service, _ := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error {
		require.Fail(t, "oversized export must not reach LogBulk")
		return nil
	})
	mux := mountedTraceMux(service)
	scopeGroups := appendProtobufMessages(nil, 2, nil, maxOTLPScopeGroups+1)
	spans := appendProtobufMessages(nil, 2, nil, maxOTLPSpansPerExport+1)
	tests := []struct {
		name   string
		body   []byte
		status int
	}{
		{name: "resource groups", body: appendProtobufMessages(nil, 1, nil, maxOTLPResourceGroups+1), status: http.StatusBadRequest},
		{name: "scope groups", body: appendProtobufMessages(nil, 1, scopeGroups, 1), status: http.StatusBadRequest},
		{name: "spans", body: appendProtobufMessages(nil, 1, appendProtobufMessages(nil, 2, spans, 1), 1), status: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		require.Error(t, preflightOTLPProtobuf(test.body), test.name)
		response := serveTraceRequest(t, mux, test.body, "application/x-protobuf", "", "valid-key", "project-test")
		require.Equal(t, test.status, response.Code, test.name)
	}
}

func TestTraceHTTPEnforcesJSONAttributeLimits(t *testing.T) {
	t.Parallel()

	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	var persisted atomic.Int64
	service, processor := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error {
		persisted.Add(1)
		return nil
	})
	mux := mountedTraceMux(service)
	tests := []struct {
		name   string
		body   []byte
		status int
	}{
		{name: "boundary", body: jsonTraceWithAttributeCounts(maxOTLPAttributes, maxOTLPAttributes, maxOTLPAttributes), status: http.StatusAccepted},
		{name: "resource excess", body: jsonTraceWithAttributeCounts(maxOTLPAttributes+1, 0, 0), status: http.StatusBadRequest},
		{name: "scope excess", body: jsonTraceWithAttributeCounts(0, maxOTLPAttributes+1, 0), status: http.StatusBadRequest},
		{name: "span excess", body: jsonTraceWithAttributeCounts(0, 0, maxOTLPAttributes+1), status: http.StatusBadRequest},
	}
	for _, test := range tests {
		if test.status == http.StatusAccepted {
			require.NoError(t, preflightOTLPJSON(test.body), test.name)
		} else {
			require.Error(t, preflightOTLPJSON(test.body), test.name)
		}
		response := serveTraceRequest(t, mux, test.body, "application/json", "", "valid-key", "project-test")
		require.Equal(t, test.status, response.Code, test.name)
	}
	require.NoError(t, processor.Shutdown(t.Context()))
	require.EqualValues(t, 1, persisted.Load())
}

func TestTraceHTTPEnforcesProtobufAttributeLimits(t *testing.T) {
	t.Parallel()

	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	var persisted atomic.Int64
	service, processor := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error {
		persisted.Add(1)
		return nil
	})
	mux := mountedTraceMux(service)
	tests := []struct {
		name   string
		body   []byte
		status int
	}{
		{name: "boundary", body: protobufTraceWithAttributeCounts(maxOTLPAttributes, maxOTLPAttributes, maxOTLPAttributes), status: http.StatusAccepted},
		{name: "resource excess", body: protobufTraceWithAttributeCounts(maxOTLPAttributes+1, 0, 0), status: http.StatusBadRequest},
		{name: "scope excess", body: protobufTraceWithAttributeCounts(0, maxOTLPAttributes+1, 0), status: http.StatusBadRequest},
		{name: "span excess", body: protobufTraceWithAttributeCounts(0, 0, maxOTLPAttributes+1), status: http.StatusBadRequest},
	}
	for _, test := range tests {
		if test.status == http.StatusAccepted {
			require.NoError(t, preflightOTLPProtobuf(test.body), test.name)
		} else {
			require.Error(t, preflightOTLPProtobuf(test.body), test.name)
		}
		response := serveTraceRequest(t, mux, test.body, "application/x-protobuf", "", "valid-key", "project-test")
		require.Equal(t, test.status, response.Code, test.name)
	}
	require.NoError(t, processor.Shutdown(t.Context()))
	require.EqualValues(t, 1, persisted.Load())
}

func TestTraceHTTPEnforcesJSONNestedAnyValueLimits(t *testing.T) {
	t.Parallel()

	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	var persisted atomic.Int64
	service, processor := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error {
		persisted.Add(1)
		return nil
	})
	mux := mountedTraceMux(service)
	resourceNodes := maxOTLPNestedValueNodes / 3
	scopeNodes := maxOTLPNestedValueNodes / 3
	spanNodes := maxOTLPNestedValueNodes - resourceNodes - scopeNodes
	tests := []struct {
		name   string
		body   []byte
		status int
	}{
		{name: "node boundary", body: jsonTraceWithAnyValues(jsonArrayAnyValue(resourceNodes), jsonArrayAnyValue(scopeNodes), jsonArrayAnyValue(spanNodes)), status: http.StatusAccepted},
		{name: "node excess", body: jsonTraceWithAnyValues(jsonArrayAnyValue(resourceNodes), jsonArrayAnyValue(scopeNodes), jsonArrayAnyValue(spanNodes+1)), status: http.StatusBadRequest},
		{name: "depth boundary", body: jsonTraceWithAnyValues(`{}`, `{}`, nestedJSONAnyValue(maxOTLPAnyValueDepth)), status: http.StatusAccepted},
		{name: "depth excess", body: jsonTraceWithAnyValues(`{}`, `{}`, nestedJSONAnyValue(maxOTLPAnyValueDepth+1)), status: http.StatusBadRequest},
	}
	for _, test := range tests {
		if test.status == http.StatusAccepted {
			require.NoError(t, preflightOTLPJSON(test.body), test.name)
		} else {
			require.Error(t, preflightOTLPJSON(test.body), test.name)
		}
		response := serveTraceRequest(t, mux, test.body, "application/json", "", "valid-key", "project-test")
		require.Equal(t, test.status, response.Code, test.name)
	}
	require.NoError(t, processor.Shutdown(t.Context()))
	require.EqualValues(t, 2, persisted.Load())
}

func TestTraceHTTPEnforcesProtobufNestedAnyValueLimits(t *testing.T) {
	t.Parallel()

	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	var persisted atomic.Int64
	service, processor := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error {
		persisted.Add(1)
		return nil
	})
	mux := mountedTraceMux(service)
	resourceNodes := maxOTLPNestedValueNodes / 3
	scopeNodes := maxOTLPNestedValueNodes / 3
	spanNodes := maxOTLPNestedValueNodes - resourceNodes - scopeNodes
	tests := []struct {
		name   string
		body   []byte
		status int
	}{
		{name: "node boundary", body: protobufTraceWithAnyValues(protobufArrayAnyValue(resourceNodes), protobufArrayAnyValue(scopeNodes), protobufArrayAnyValue(spanNodes)), status: http.StatusAccepted},
		{name: "node excess", body: protobufTraceWithAnyValues(protobufArrayAnyValue(resourceNodes), protobufArrayAnyValue(scopeNodes), protobufArrayAnyValue(spanNodes+1)), status: http.StatusBadRequest},
		{name: "depth boundary", body: protobufTraceWithAnyValues(nil, nil, nestedProtobufAnyValue(maxOTLPAnyValueDepth)), status: http.StatusAccepted},
		{name: "depth excess", body: protobufTraceWithAnyValues(nil, nil, nestedProtobufAnyValue(maxOTLPAnyValueDepth+1)), status: http.StatusBadRequest},
	}
	for _, test := range tests {
		if test.status == http.StatusAccepted {
			require.NoError(t, preflightOTLPProtobuf(test.body), test.name)
		} else {
			require.Error(t, preflightOTLPProtobuf(test.body), test.name)
		}
		response := serveTraceRequest(t, mux, test.body, "application/x-protobuf", "", "valid-key", "project-test")
		require.Equal(t, test.status, response.Code, test.name)
	}
	require.NoError(t, processor.Shutdown(t.Context()))
	require.EqualValues(t, 2, persisted.Load())
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

func TestTraceHTTPAcceptsEmptyAnyValues(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(t.Context())) })
	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	jobs := make(chan []telemetry.LogParams, 2)
	service, processor := newTraceTestService(t, authorizer, meterProvider, func(_ context.Context, params []telemetry.LogParams) error {
		jobs <- params
		return nil
	})
	mux := mountedTraceMux(service)
	jsonBody := []byte(`{"resourceSpans":[{"scopeSpans":[{"spans":[{"attributes":[{"key":"gen_ai.request.model","value":{}},{"key":"unknown.attribute","value":{}}]}]}]}]}`)
	require.Equal(t, http.StatusAccepted, serveTraceRequest(t, mux, jsonBody, "application/json", "", "valid-key", "project-test").Code)
	require.EqualValues(t, 1, metricCounterValue(t, reader, "litellm.otel.attributes.truncated"))

	allowed := protowire.AppendTag(nil, 1, protowire.BytesType)
	allowed = protowire.AppendString(allowed, "gen_ai.request.model")
	allowed = appendProtobufMessages(allowed, 2, nil, 1)
	unknown := protowire.AppendTag(nil, 1, protowire.BytesType)
	unknown = protowire.AppendString(unknown, "unknown.attribute")
	unknown = appendProtobufMessages(unknown, 2, nil, 1)
	span := appendProtobufMessages(nil, 9, allowed, 1)
	span = appendProtobufMessages(span, 9, unknown, 1)
	scope := appendProtobufMessages(nil, 2, span, 1)
	resource := appendProtobufMessages(nil, 2, scope, 1)
	protobufBody := appendProtobufMessages(nil, 1, resource, 1)
	require.Equal(t, http.StatusAccepted, serveTraceRequest(t, mux, protobufBody, "application/x-protobuf", "", "valid-key", "project-test").Code)
	require.EqualValues(t, 2, metricCounterValue(t, reader, "litellm.otel.attributes.truncated"))

	require.NoError(t, processor.Shutdown(t.Context()))
	for range 2 {
		params := <-jobs
		require.Len(t, params, 1)
		require.NotContains(t, params[0].Attributes, attr.GenAIRequestModelKey)
		require.NotContains(t, params[0].Attributes, "unknown.attribute")
	}
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
	instanceID := uuid.New()
	service.instances.Remember(authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.APIKeyID, instanceID)
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
		require.Equal(t, authCtx.APIKeyID, params[0].Attributes[attr.APIKeyIDKey])
		require.Equal(t, instanceID.String(), params[0].Attributes[attr.LiteLLMInstanceIDKey])
	}
}

func TestTraceHTTPAcceptsEmptyExportWithoutEnqueue(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(t.Context())) })
	var callbackCalls atomic.Int64
	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	service, processor := newTraceTestService(t, authorizer, meterProvider, func(context.Context, []telemetry.LogParams) error {
		callbackCalls.Add(1)
		return nil
	})

	response := serveTraceRequest(t, mountedTraceMux(service), []byte(`{"resourceSpans":[{"scopeSpans":[]}]}`), "application/json", "", "valid-key", "project-test")
	require.Equal(t, http.StatusAccepted, response.Code)
	require.NoError(t, processor.Shutdown(t.Context()))
	require.Zero(t, callbackCalls.Load())
	require.Zero(t, metricCounterValueOrZero(t, reader, "litellm.otel.spans.accepted"))
}

func TestTracesRejectsNilPayload(t *testing.T) {
	t.Parallel()

	service, _ := newTraceTestService(t, fixedAuthorizer{authCtx: testAuthContext()}, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	err := service.Traces(t.Context(), nil)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Equal(t, http.StatusBadRequest, oopsErr.HTTPStatus(t.Context()))
}

func TestTraceHTTPAcceptsCollectionLimitBoundaries(t *testing.T) {
	t.Parallel()

	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	service, _ := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	mux := mountedTraceMux(service)

	jsonScopes := make([]string, maxOTLPScopeGroups)
	for index := range jsonScopes {
		jsonScopes[index] = `{}`
	}
	jsonScopes[0] = `{"spans":[` + repeatedJSONObjects(maxOTLPSpansPerExport) + `]}`
	jsonResources := make([]string, maxOTLPResourceGroups)
	for index := range jsonResources {
		jsonResources[index] = `{}`
	}
	jsonResources[0] = `{"scopeSpans":[` + strings.Join(jsonScopes, ",") + `]}`
	jsonBody := []byte(`{"resourceSpans":[` + strings.Join(jsonResources, ",") + `]}`)
	require.NoError(t, preflightOTLPJSON(jsonBody))
	require.Equal(t, http.StatusAccepted, serveTraceRequest(t, mux, jsonBody, "application/json", "", "valid-key", "project-test").Code)

	protobufSpans := appendProtobufMessages(nil, 2, nil, maxOTLPSpansPerExport)
	protobufResource := appendProtobufMessages(nil, 2, protobufSpans, 1)
	protobufResource = appendProtobufMessages(protobufResource, 2, nil, maxOTLPScopeGroups-1)
	protobufBody := appendProtobufMessages(nil, 1, protobufResource, 1)
	protobufBody = appendProtobufMessages(protobufBody, 1, nil, maxOTLPResourceGroups-1)
	require.NoError(t, preflightOTLPProtobuf(protobufBody))
	require.Equal(t, http.StatusAccepted, serveTraceRequest(t, mux, protobufBody, "application/x-protobuf", "", "valid-key", "project-test").Code)
}

func TestTraceHTTPAcceptsProtobufUnknownFields(t *testing.T) {
	t.Parallel()

	unknown := protowire.AppendTag(nil, 100, protowire.VarintType)
	unknown = protowire.AppendVarint(unknown, 1)
	unknown = protowire.AppendTag(unknown, 101, protowire.Fixed32Type)
	unknown = protowire.AppendFixed32(unknown, 2)
	unknown = protowire.AppendTag(unknown, 102, protowire.Fixed64Type)
	unknown = protowire.AppendFixed64(unknown, 3)
	unknown = protowire.AppendTag(unknown, 103, protowire.BytesType)
	unknown = protowire.AppendBytes(unknown, []byte("unknown"))
	unknown = protowire.AppendTag(unknown, 104, protowire.StartGroupType)
	unknown = protowire.AppendTag(unknown, 105, protowire.VarintType)
	unknown = protowire.AppendVarint(unknown, 4)
	unknown = protowire.AppendTag(unknown, 104, protowire.EndGroupType)
	require.NoError(t, preflightOTLPProtobuf(unknown))

	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	service, _ := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	response := serveTraceRequest(t, mountedTraceMux(service), unknown, "application/x-protobuf", "", "valid-key", "project-test")
	require.Equal(t, http.StatusAccepted, response.Code)
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
		require.NotContains(t, param.Attributes, attr.OTelSpanDurationMSKey)
	}
	require.Equal(t, uint64(math.MaxUint64), params[1].Attributes[attr.OTelSpanStartTimeUnixNanoKey])
	require.NoError(t, processor.Shutdown(t.Context()))
}

func TestTraceHTTPQueueSaturationDropsWithoutBlocking(t *testing.T) {
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
	var releaseOnce sync.Once
	releaseWorkers := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() {
		releaseWorkers()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, processor.Shutdown(ctx))
	})

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
		tracer:    testenv.NewTracerProvider(t).Tracer("test"),
		logger:    testenv.NewLogger(t),
		auth:      authorizer,
		hooks:     nil,
		calls:     nil,
		traces:    processor,
		metrics:   nil,
		health:    newDisabledHealthProcessor(t),
		db:        nil,
		telemetry: nil,
		instances: NewInstanceResolver(testenv.NewLogger(t), nil),
		authz:     nil,
		audit:     nil,
		keyPrefix: "",
	}
	response := serveTraceRequest(t, mountedTraceMux(service), []byte(`{"resourceSpans":[{"scopeSpans":[{"spans":[{"traceId":"0123456789abcdef0123456789abcdef","spanId":"0123456789abcdef","name":"queue test"}]}]}]}`), "application/json", "", "valid-key", "project-test")
	require.Equal(t, http.StatusAccepted, response.Code)
	require.EqualValues(t, 1, metricCounterValue(t, reader, "litellm.otel.spans.dropped"))

	releaseWorkers()
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

func TestTraceProcessorBoundsPersistenceAttempts(t *testing.T) {
	t.Parallel()

	deadline := make(chan struct {
		value time.Time
		ok    bool
	}, 1)
	processor := newTraceProcessor(testenv.NewLogger(t), testenv.NewMeterProvider(t), func(ctx context.Context, _ []telemetry.LogParams) error {
		value, ok := ctx.Deadline()
		deadline <- struct {
			value time.Time
			ok    bool
		}{value: value, ok: ok}
		return nil
	}, 1, 1)
	processor.Start(t.Context())
	require.True(t, processor.Enqueue(t.Context(), []telemetry.LogParams{}))
	got := <-deadline
	require.True(t, got.ok)
	require.WithinDuration(t, time.Now().Add(tracePersistenceTimeout), got.value, time.Second)
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

func TestTraceProcessorRecoversPersistencePanic(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(t.Context())) })
	calls := 0
	persisted := make(chan struct{}, 1)
	processor := newTraceProcessor(testenv.NewLogger(t), meterProvider, func(context.Context, []telemetry.LogParams) error {
		calls++
		if calls == 1 {
			panic("persistence panic")
		}
		persisted <- struct{}{}
		return nil
	}, 1, 2)
	processor.Start(t.Context())

	require.True(t, processor.Enqueue(t.Context(), make([]telemetry.LogParams, 3)))
	require.True(t, processor.Enqueue(t.Context(), make([]telemetry.LogParams, 1)))
	require.NoError(t, processor.Shutdown(t.Context()))
	require.Equal(t, 2, calls)
	require.Len(t, persisted, 1)
	require.EqualValues(t, 3, metricCounterValueForReason(t, reader, "litellm.otel.spans.persistence_failed", "log_bulk_panic"))
}

func metricCounterValue(t *testing.T, reader *sdkmetric.ManualReader, name string) int64 {
	t.Helper()
	result, found := metricCounterValueIfPresent(t, reader, name)
	require.True(t, found, "metric not found: %s", name)
	return result
}

func metricCounterValueOrZero(t *testing.T, reader *sdkmetric.ManualReader, name string) int64 {
	t.Helper()
	result, _ := metricCounterValueIfPresent(t, reader, name)
	return result
}

func metricCounterValueIfPresent(t *testing.T, reader *sdkmetric.ManualReader, name string) (int64, bool) {
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
			return result, true
		}
	}
	return 0, false
}

func metricCounterValueForReason(t *testing.T, reader *sdkmetric.ManualReader, name, reason string) int64 {
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
				value, ok := point.Attributes.Value(attr.ReasonKey)
				if ok && value.AsString() == reason {
					result += point.Value
				}
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

	for _, log := range logs[:2] {
		require.Equal(t, "0123456789abcdef0123456789abcdef", gjson.Get(log.Attributes, "trace.id").String())
		require.Equal(t, "fixture-logical-trace", gjson.Get(log.Attributes, "gram.litellm.trace_id").String())
		require.Equal(t, "fixture-call-id", gjson.Get(log.Attributes, "gram.litellm.call_id").String())
		require.Equal(t, "fixture-logical-trace", gjson.Get(log.Attributes, "gen_ai.conversation.id").String())
		require.Equal(t, "openai", gjson.Get(log.Attributes, "gen_ai.provider.name").String())
		require.Equal(t, "fixture-model", gjson.Get(log.Attributes, "gen_ai.request.model").String())
		require.Equal(t, "fixture-model-v2", gjson.Get(log.Attributes, "gen_ai.response.model").String())
		require.Equal(t, "fixture-response-id", gjson.Get(log.Attributes, "gen_ai.response.id").String())
		require.EqualValues(t, 11, gjson.Get(log.Attributes, "gen_ai.usage.input_tokens").Int())
		require.EqualValues(t, 7, gjson.Get(log.Attributes, "gen_ai.usage.output_tokens").Int())
		require.EqualValues(t, 18, gjson.Get(log.Attributes, "gen_ai.usage.total_tokens").Int())
		require.InDelta(t, 0.00125, gjson.Get(log.Attributes, "gen_ai.usage.cost").Float(), 0.0000001)
		require.InDelta(t, 125, gjson.Get(log.Attributes, "otel.span.duration_ms").Float(), 0.000001)
		require.True(t, gjson.Get(log.Attributes, "gen_ai.request.is_streaming").Bool())
		require.Equal(t, "urn:telemetry:provider_otel:span:chat", gjson.Get(log.Attributes, "gram.event.urn").String())
		require.False(t, gjson.Get(log.Attributes, "gen_ai.response.finish_reasons").Exists())
	}
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
	require.Empty(t, recursive)

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
	require.Equal(t, "arbitrary high-cardinality span", unknownOperation[0].Attributes[attr.OTelSpanNameKey])

	_, validID := normalizeOTLPID(strings.Repeat("0", 32), 32)
	require.False(t, validID)
	_, validID = normalizeOTLPID(strings.Repeat("0", 16), 16)
	require.False(t, validID)
	require.NoError(t, processor.Shutdown(t.Context()))
}

func TestOTLPJSONScalarDecodersAcceptEscapedStringsAndRawNumbers(t *testing.T) {
	t.Parallel()

	var signed jsonInt64
	require.NoError(t, json.Unmarshal([]byte(`"1\u0032"`), &signed))
	require.Equal(t, jsonInt64(12), signed)
	require.NoError(t, json.Unmarshal([]byte(`13`), &signed))
	require.Equal(t, jsonInt64(13), signed)

	var unsigned jsonUint64
	require.NoError(t, json.Unmarshal([]byte(`"1\u0034"`), &unsigned))
	require.Equal(t, jsonUint64(14), unsigned)
	require.NoError(t, json.Unmarshal([]byte(`15`), &unsigned))
	require.Equal(t, jsonUint64(15), unsigned)

	var decimal jsonFloat64
	require.NoError(t, json.Unmarshal([]byte(`"1.\u0036"`), &decimal))
	require.InDelta(t, 1.6, float64(decimal), 0.000001)
	require.NoError(t, json.Unmarshal([]byte(`1.7`), &decimal))
	require.InDelta(t, 1.7, float64(decimal), 0.000001)

	var kind jsonInt32
	require.NoError(t, json.Unmarshal([]byte(`"\u0031"`), &kind))
	require.Equal(t, jsonInt32(1), kind)
	require.NoError(t, json.Unmarshal([]byte(`2`), &kind))
	require.Equal(t, jsonInt32(2), kind)
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
	streaming := true
	tokens := jsonInt64(42)
	cost := jsonFloat64(0.125)
	result := service.sanitizeOTLPAttributes(t.Context(), []otlpKeyValue{
		{Key: "gen_ai.request.model", Value: otlpAnyValue{StringValue: &model}},
		{Key: "gen_ai.request.is_streaming", Value: otlpAnyValue{BoolValue: &streaming}},
		{Key: "gen_ai.usage.input_tokens", Value: otlpAnyValue{IntValue: &tokens}},
		{Key: "gen_ai.usage.cost", Value: otlpAnyValue{DoubleValue: &cost}},
	}, spanAttributeAllowlist)
	require.Equal(t, "safe-model", result[attr.GenAIRequestModelKey])
	require.Equal(t, true, result[attr.GenAIRequestIsStreamingKey])
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
	require.NoError(t, processor.Shutdown(t.Context()))
}

func TestOTLPV2AttributesNormalizeToCanonicalUsage(t *testing.T) {
	t.Parallel()

	service, processor := newTraceTestService(t, fixedAuthorizer{authCtx: testAuthContext()}, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	stringsByKey := map[string]string{
		"litellm.provider.model":                    "openai/gpt-4o",
		"litellm.team.id":                           "team-id",
		"litellm.team.alias":                        "engineering",
		"litellm.api_key.hash":                      "key-hash",
		"litellm.end_user.id":                       "metadata-end-user",
		"litellm.metadata.user_api_key_user_id":     "user-id",
		"litellm.metadata.user_api_key_user_email":  "member@example.test",
		"litellm.metadata.user_api_key_org_id":      "provider-org",
		"litellm.metadata.user_api_key_alias":       "key-alias",
		"litellm.metadata.user_api_key_end_user_id": "metadata-end-user",
	}
	values := make([]otlpKeyValue, 0, len(stringsByKey)+6)
	for key, value := range stringsByKey {
		current := value
		values = append(values, otlpKeyValue{Key: key, Value: otlpAnyValue{StringValue: &current}})
	}
	streaming := true
	totalCost := jsonFloat64(0.12)
	inputCost := jsonFloat64(0.07)
	outputCost := jsonFloat64(0.05)
	cacheReadCost := jsonFloat64(0.01)
	cacheWriteCost := jsonFloat64(0.02)
	values = append(values,
		otlpKeyValue{Key: "litellm.request.streaming", Value: otlpAnyValue{BoolValue: &streaming}},
		otlpKeyValue{Key: "litellm.cost.total", Value: otlpAnyValue{DoubleValue: &totalCost}},
		otlpKeyValue{Key: "litellm.cost.input", Value: otlpAnyValue{DoubleValue: &inputCost}},
		otlpKeyValue{Key: "litellm.cost.output", Value: otlpAnyValue{DoubleValue: &outputCost}},
		otlpKeyValue{Key: "litellm.cost.cache_read", Value: otlpAnyValue{DoubleValue: &cacheReadCost}},
		otlpKeyValue{Key: "litellm.cost.cache_creation", Value: otlpAnyValue{DoubleValue: &cacheWriteCost}},
	)

	result := service.sanitizeOTLPAttributes(t.Context(), values, spanAttributeAllowlist)
	require.Equal(t, "openai/gpt-4o", result[attr.GenAIResponseModelKey])
	require.Equal(t, true, result[attr.GenAIRequestIsStreamingKey])
	require.InDelta(t, 0.12, result[attr.GenAIUsageCostKey], 1e-12)
	require.InDelta(t, 0.07, result[attr.LiteLLMInputCostKey], 1e-12)
	require.InDelta(t, 0.05, result[attr.LiteLLMOutputCostKey], 1e-12)
	require.InDelta(t, 0.01, result[attr.LiteLLMCacheReadCostKey], 1e-12)
	require.InDelta(t, 0.02, result[attr.LiteLLMCacheWriteCostKey], 1e-12)
	require.Equal(t, "team-id", result[attr.LiteLLMTeamIDKey])
	require.Equal(t, "engineering", result[attr.LiteLLMTeamAliasKey])
	require.Equal(t, "key-hash", result[attr.LiteLLMAPIKeyHashKey])
	require.Equal(t, "metadata-end-user", result[attr.LiteLLMEndUserIDKey])
	require.Equal(t, "user-id", result[attr.LiteLLMUserIDKey])
	require.Equal(t, "member@example.test", result[attr.LiteLLMUserEmailKey])
	require.Equal(t, "provider-org", result[attr.LiteLLMOrganizationIDKey])
	require.Equal(t, "key-alias", result[attr.LiteLLMAPIKeyAliasKey])
	require.NoError(t, processor.Shutdown(t.Context()))
}

func TestLiteLLMModelOperationExcludesNonModelSpans(t *testing.T) {
	t.Parallel()

	attributes := map[attr.Key]any{
		attr.GenAIOperationNameKey: "chat",
		attr.GenAIRequestModelKey:  "model",
	}
	require.Equal(t, "chat", liteLLMModelOperation(attributes, int32(tracev1.Span_SPAN_KIND_CLIENT)))
	require.Equal(t, "unknown", liteLLMModelOperation(attributes, int32(tracev1.Span_SPAN_KIND_INTERNAL)))
	require.Equal(t, "unknown", liteLLMModelOperation(map[attr.Key]any{attr.GenAIOperationNameKey: "chat"}, int32(tracev1.Span_SPAN_KIND_CLIENT)))
	require.Equal(t, "unknown", liteLLMModelOperation(map[attr.Key]any{attr.GenAIOperationNameKey: "execute_guardrail"}, int32(tracev1.Span_SPAN_KIND_INTERNAL)))
}

func TestTraceLogParamsKeepsGuardrailSpanOperationalOnly(t *testing.T) {
	t.Parallel()

	service, processor := newTraceTestService(t, fixedAuthorizer{authCtx: testAuthContext()}, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	operation := "execute_guardrail"
	model := "must-not-be-usage"
	cost := jsonFloat64(1.25)
	callID := "guardrail-call"
	request := &otlpExportRequest{ResourceSpans: []otlpResourceSpans{{
		Resource: nil,
		ScopeSpans: []otlpScopeSpans{{
			Scope: nil,
			Spans: []otlpSpan{{
				TraceID: "", SpanID: "", ParentSpanID: "", Name: "execute_guardrail policy", Kind: jsonInt32(tracev1.Span_SPAN_KIND_INTERNAL),
				StartTimeUnixNano: 1000000, EndTimeUnixNano: 2000000,
				Attributes: []otlpKeyValue{
					{Key: "gen_ai.operation.name", Value: otlpAnyValue{StringValue: &operation}},
					{Key: "gen_ai.request.model", Value: otlpAnyValue{StringValue: &model}},
					{Key: "litellm.cost.total", Value: otlpAnyValue{DoubleValue: &cost}},
					{Key: "litellm.call_id", Value: otlpAnyValue{StringValue: &callID}},
				},
				DroppedAttributesCount: 0, Status: nil,
			}},
		}},
	}}}
	params := service.traceLogParams(t.Context(), request, "org-id", uuid.NewString())
	require.Len(t, params, 1)
	require.Equal(t, "urn:telemetry:provider_otel:span:unknown", params[0].Attributes[attr.EventURNKey])
	require.Equal(t, "execute_guardrail", params[0].Attributes[attr.GenAIOperationNameKey])
	require.Equal(t, "execute_guardrail policy", params[0].Attributes[attr.OTelSpanNameKey])
	require.Equal(t, "internal", params[0].Attributes[attr.OTelSpanKindKey])
	require.InDelta(t, 1, params[0].Attributes[attr.OTelSpanDurationMSKey], 0.000001)
	require.Equal(t, "guardrail-call", params[0].Attributes[attr.LiteLLMCallIDKey])
	require.NotContains(t, params[0].Attributes, attr.GenAIRequestModelKey)
	require.NotContains(t, params[0].Attributes, attr.GenAIUsageCostKey)
	require.NoError(t, processor.Shutdown(t.Context()))
}

func TestTraceLogParamsUsesOTLPFallbackWithoutCachedCall(t *testing.T) {
	t.Parallel()

	service, processor := newTraceTestService(t, fixedAuthorizer{authCtx: testAuthContext()}, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	operation := "embeddings"
	model := "embedding-model"
	callID := "fallback-call"
	traceID := "fallback-trace"
	email := "fallback@example.test"
	inputTokens := jsonInt64(11)
	outputTokens := jsonInt64(7)
	request := &otlpExportRequest{ResourceSpans: []otlpResourceSpans{{
		Resource: nil,
		ScopeSpans: []otlpScopeSpans{{
			Scope: nil,
			Spans: []otlpSpan{{
				TraceID: "", SpanID: "", ParentSpanID: "", Name: "embeddings model", Kind: jsonInt32(tracev1.Span_SPAN_KIND_CLIENT),
				StartTimeUnixNano: 0, EndTimeUnixNano: 0,
				Attributes: []otlpKeyValue{
					{Key: "gen_ai.operation.name", Value: otlpAnyValue{StringValue: &operation}},
					{Key: "gen_ai.request.model", Value: otlpAnyValue{StringValue: &model}},
					{Key: "litellm.call_id", Value: otlpAnyValue{StringValue: &callID}},
					{Key: "litellm.trace_id", Value: otlpAnyValue{StringValue: &traceID}},
					{Key: "litellm.metadata.user_api_key_user_email", Value: otlpAnyValue{StringValue: &email}},
					{Key: "gen_ai.usage.input_tokens", Value: otlpAnyValue{IntValue: &inputTokens}},
					{Key: "gen_ai.usage.output_tokens", Value: otlpAnyValue{IntValue: &outputTokens}},
				},
				DroppedAttributesCount: 0, Status: nil,
			}},
		}},
	}}}
	projectID := uuid.NewString()
	params := service.traceLogParams(t.Context(), request, "org-id", projectID)
	require.Len(t, params, 1)
	require.Equal(t, projectID, params[0].ToolInfo.ProjectID)
	require.Equal(t, "org-id", params[0].ToolInfo.OrganizationID)
	require.Equal(t, "fallback@example.test", params[0].UserInfo.Email())
	require.Empty(t, params[0].UserInfo.UserID())
	require.Equal(t, "fallback-trace", params[0].Attributes[attr.GenAIConversationIDKey])
	require.EqualValues(t, 18, params[0].Attributes[attr.GenAIUsageTotalTokensKey])
	require.Equal(t, "urn:telemetry:provider_otel:span:embeddings", params[0].Attributes[attr.EventURNKey])
	require.NoError(t, processor.Shutdown(t.Context()))
}

func TestEnrichTraceAttributionUsesPreCallCache(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	calls := callcache.New(newMemoryCache())
	require.NoError(t, calls.Store(t.Context(), callcache.Record{
		ProjectID: projectID,
		CallID:    "call-id",
		TraceID:   "cached-trace",
		SessionID: "cached-session",
		UserID:    "cached-user",
		Email:     "cached@example.test",
	}))
	spans := []telemetry.LogParams{{
		Timestamp: time.Time{},
		ToolInfo: telemetry.ToolInfo{
			ID: "", URN: litellmOTLPResourceURN, Name: "litellm", ProjectID: projectID.String(), DeploymentID: "", FunctionID: nil, OrganizationID: "org-id",
		},
		UserInfo: telemetry.UserInfoByEmail("otel@example.test"),
		Attributes: map[attr.Key]any{
			attr.LiteLLMCallIDKey:       "call-id",
			attr.LiteLLMTraceIDKey:      "otel-trace",
			attr.GenAIConversationIDKey: "otel-session",
			attr.EventURNKey:            liteLLMEventURN("chat"),
		},
	}, {
		Timestamp: time.Time{},
		ToolInfo: telemetry.ToolInfo{
			ID: "", URN: litellmOTLPResourceURN, Name: "litellm", ProjectID: projectID.String(), DeploymentID: "", FunctionID: nil, OrganizationID: "org-id",
		},
		UserInfo: telemetry.UserInfoByID(""),
		Attributes: map[attr.Key]any{
			attr.LiteLLMCallIDKey: "call-id",
			attr.EventURNKey:      liteLLMEventURN("unknown"),
		},
	}}

	enrichTraceAttribution(t.Context(), testenv.NewLogger(t), calls, spans)
	require.Equal(t, "cached-user", spans[0].UserInfo.UserID())
	require.Equal(t, "cached@example.test", spans[0].UserInfo.Email())
	require.Equal(t, "cached-session", spans[0].Attributes[attr.GenAIConversationIDKey])
	require.Equal(t, "cached-trace", spans[0].Attributes[attr.LiteLLMTraceIDKey])
	require.Empty(t, spans[1].UserInfo.UserID())
	require.NotContains(t, spans[1].Attributes, attr.GenAIConversationIDKey)
	require.NotContains(t, spans[1].Attributes, attr.LiteLLMTraceIDKey)
}

func TestEnrichTraceAttributionPreservesOTLPActorWhenCachedActorIsEmpty(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	calls := callcache.New(newMemoryCache())
	require.NoError(t, calls.Store(t.Context(), callcache.Record{
		ProjectID: projectID,
		CallID:    "call-id",
		TraceID:   "cached-trace",
		SessionID: "cached-session",
		UserID:    "",
		Email:     "",
	}))
	spans := []telemetry.LogParams{{
		Timestamp: time.Time{},
		ToolInfo: telemetry.ToolInfo{
			ID: "", URN: litellmOTLPResourceURN, Name: "litellm", ProjectID: projectID.String(), DeploymentID: "", FunctionID: nil, OrganizationID: "org-id",
		},
		UserInfo: telemetry.UserInfoByEmail("otel@example.test"),
		Attributes: map[attr.Key]any{
			attr.LiteLLMCallIDKey: "call-id",
			attr.EventURNKey:      liteLLMEventURN("chat"),
		},
	}}

	enrichTraceAttribution(t.Context(), testenv.NewLogger(t), calls, spans)
	require.Empty(t, spans[0].UserInfo.UserID())
	require.Equal(t, "otel@example.test", spans[0].UserInfo.Email())
	require.Equal(t, "cached-session", spans[0].Attributes[attr.GenAIConversationIDKey])
	require.Equal(t, "cached-trace", spans[0].Attributes[attr.LiteLLMTraceIDKey])
}

func TestOTLPResourceAttributeBudgetDropsOverflow(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(t.Context())) })
	service, processor := newTraceTestService(t, fixedAuthorizer{authCtx: testAuthContext()}, meterProvider, func(context.Context, []telemetry.LogParams) error { return nil })
	safe := "litellm"
	large := strings.Repeat("x", maxOTLPResourceBytes/3)
	values := make([]otlpKeyValue, 0, len(resourceAttributeAllowlist))
	values = append(values, otlpKeyValue{Key: "service.name", Value: otlpAnyValue{StringValue: &safe}})
	for _, key := range []string{
		"service.namespace", "service.version", "service.instance.id", "deployment.environment",
		"deployment.environment.name", "telemetry.sdk.name", "telemetry.sdk.language", "telemetry.sdk.version",
	} {
		value := large
		values = append(values, otlpKeyValue{Key: key, Value: otlpAnyValue{StringValue: &value}})
	}

	result := service.sanitizeOTLPResourceAttributes(t.Context(), values)
	require.Equal(t, "litellm", result[attr.ServiceNameKey])
	require.Less(t, len(result), len(values))
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), maxOTLPResourceBytes)
	require.EqualValues(t, len(values)-len(result), metricCounterValue(t, reader, "litellm.otel.attributes.truncated"))
	require.NoError(t, processor.Shutdown(t.Context()))
}
