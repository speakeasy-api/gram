package otel

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

type capturedRelayRequest struct {
	customer    string
	path        string
	contentType string
	request     *collectortracev1.ExportTraceServiceRequest
	err         error
}

type relayRequestCapture struct {
	mu       sync.Mutex
	requests []capturedRelayRequest
}

func (c *relayRequestCapture) handler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	request := &collectortracev1.ExportTraceServiceRequest{}
	if err == nil {
		err = proto.Unmarshal(body, request)
	}

	c.mu.Lock()
	c.requests = append(c.requests, capturedRelayRequest{
		customer:    r.Header.Get("X-Customer"),
		path:        r.URL.Path,
		contentType: r.Header.Get("Content-Type"),
		request:     request,
		err:         err,
	})
	c.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (c *relayRequestCapture) snapshot() []capturedRelayRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.requests)
}

func TestSpanRelayHandlerGroupsByProvenanceAndCachesDestinations(t *testing.T) {
	t.Parallel()

	capture := &relayRequestCapture{mu: sync.Mutex{}, requests: nil}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)
	handler := newRelayTestHandler(t, testenv.NewMeterProvider(t))
	destinationA := cacheRelayTestDestination(t, handler, "org-a", server.URL, map[string]string{"X-Customer": "a"})
	cacheRelayTestDestination(t, handler, "org-b", server.URL, map[string]string{"X-Customer": "b"})
	require.Equal(t, 10*time.Second, destinationA.httpClient.Timeout)

	messages, failures := relayTestMessages(
		relayTestSpan("a-1", "org-a", "project-1"),
		relayTestSpan("a-2", "org-a", "project-1"),
		relayTestSpan("a-3", "org-a", "project-2"),
		relayTestSpan("b-1", "org-b", "project-1"),
	)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}

	// A second batch must stay on the in-memory destination cache: this handler
	// has no database connection, so a miss would fail the test.
	messages, failures = relayTestMessages(relayTestSpan("a-4", "org-a", "project-1"))
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	require.NoError(t, failures[0])

	requests := capture.snapshot()
	require.Len(t, requests, 4)
	groupedNames := make(map[string][]string)
	for _, captured := range requests {
		require.NoError(t, captured.err)
		require.Equal(t, "/v1/traces", captured.path)
		require.Equal(t, "application/x-protobuf", captured.contentType)

		names := relayRequestSpanNames(captured.request)
		slices.Sort(names)
		groupedNames[captured.customer] = append(groupedNames[captured.customer], names...)
		if slices.Equal(names, []string{"a-1", "a-2"}) {
			require.Len(t, captured.request.GetResourceSpans(), 1)
			require.Len(t, captured.request.GetResourceSpans()[0].GetScopeSpans(), 1)
			require.Len(t, captured.request.GetResourceSpans()[0].GetScopeSpans()[0].GetSpans(), 2)
		}
	}
	slices.Sort(groupedNames["a"])
	slices.Sort(groupedNames["b"])
	require.Equal(t, []string{"a-1", "a-2", "a-3", "a-4"}, groupedNames["a"])
	require.Equal(t, []string{"b-1"}, groupedNames["b"])
}

func TestNewRelayExportRequestDiscardsGramOnlySpanFields(t *testing.T) {
	t.Parallel()

	organizationID := "internal-organization-id"
	projectID := "internal-project-id"
	span := relayTestSpan("test-span", organizationID, projectID)
	provenance := span.GetProvenance()
	provenance.SetOrganizationSlug("internal-organization-slug")
	provenance.SetProjectSlug("internal-project-slug")
	provenance.SetApiKeyId("internal-api-key-id")
	provenance.SetApiKeyName("internal-api-key-name")

	request, err := newRelayExportRequest([]*otelv1.Span{span})
	require.NoError(t, err)
	require.Len(t, request.GetResourceSpans(), 1)
	require.Len(t, request.GetResourceSpans()[0].GetScopeSpans(), 1)
	require.Len(t, request.GetResourceSpans()[0].GetScopeSpans()[0].GetSpans(), 1)

	converted := request.GetResourceSpans()[0].GetScopeSpans()[0].GetSpans()[0]
	require.Empty(t, converted.ProtoReflect().GetUnknown())

	encoded, err := proto.Marshal(request)
	require.NoError(t, err)
	for _, internalValue := range []string{
		organizationID,
		projectID,
		"internal-organization-slug",
		"internal-project-slug",
		"internal-api-key-id",
		"internal-api-key-name",
	} {
		require.NotContains(t, string(encoded), internalValue)
	}
}

func TestSpanRelayHandlerCountsInvalidAndMissingDestinationDrops(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })
	handler := newRelayTestHandler(t, meterProvider)
	handler.relay.destinationCache["org-a"] = cachedRelayDestination{
		destination: nil,
		expiresAt:   time.Now().Add(time.Hour),
	}

	messages, failures := relayTestMessages(
		nil,
		(&otelv1.Span_builder{}).Build(),
		relayTestSpan("missing-1", "org-a", "project-1"),
		relayTestSpan("missing-2", "org-a", "project-1"),
	)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}

	require.Equal(t, int64(2), relaySpanCount(t, reader, meterSpanRelaySpansDropped, relayReasonInvalid))
	require.Equal(t, int64(2), relaySpanCount(t, reader, meterSpanRelaySpansDropped, relayReasonNoDestination))
}

func TestSpanRelayHandlerFailsOnlyMessagesForFailedDestination(t *testing.T) {
	t.Parallel()

	var failedRequests atomic.Int64
	failedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		failedRequests.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(failedServer.Close)
	successCapture := &relayRequestCapture{mu: sync.Mutex{}, requests: nil}
	successServer := httptest.NewServer(http.HandlerFunc(successCapture.handler))
	t.Cleanup(successServer.Close)

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })
	handler := newRelayTestHandler(t, meterProvider)
	cacheRelayTestDestination(t, handler, "org-a", failedServer.URL, nil)
	cacheRelayTestDestination(t, handler, "org-b", successServer.URL, nil)

	messages, failures := relayTestMessages(
		relayTestSpan("failed", "org-a", "project-1"),
		relayTestSpan("delivered", "org-b", "project-1"),
	)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	require.ErrorContains(t, failures[0], "503 Service Unavailable")
	require.NoError(t, failures[1])
	require.Len(t, successCapture.snapshot(), 1)
	require.Equal(t, int64(2), failedRequests.Load(), "Guardian should make one retry before Pub/Sub stages redelivery")
	require.Equal(t, int64(1), relaySpanCount(t, reader, meterSpanRelaySpansFailed, relayReasonHTTP5xx))
}

func TestSpanRelayHandlerDropsPermanentHTTPFailureWithoutRetry(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })
	handler := newRelayTestHandler(t, meterProvider)
	cacheRelayTestDestination(t, handler, "org-a", server.URL, nil)

	messages, failures := relayTestMessages(relayTestSpan("rejected", "org-a", "project-1"))
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	require.NoError(t, failures[0])
	require.Equal(t, int64(1), requests.Load())
	require.Equal(
		t,
		int64(1),
		relaySpanCount(t, reader, meterSpanRelaySpansDropped, relayReasonPermanentHTTPError),
	)
}

func TestSpanRelayHandlerRetriesRateLimitFailure(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })
	handler := newRelayTestHandler(t, meterProvider)
	cacheRelayTestDestination(t, handler, "org-a", server.URL, nil)

	messages, failures := relayTestMessages(relayTestSpan("rate-limited", "org-a", "project-1"))
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	require.ErrorContains(t, failures[0], "429 Too Many Requests")
	require.Equal(t, int64(2), requests.Load())
	require.Equal(
		t,
		int64(1),
		relaySpanCount(t, reader, meterSpanRelaySpansFailed, relayReasonHTTP4xx),
	)
}

func relayTestMessages(spans ...*otelv1.Span) ([]spanRelayMessage, []error) {
	failures := make([]error, len(spans))
	messages := make([]spanRelayMessage, len(spans))
	for i, span := range spans {
		index := i
		messages[i] = spanRelayMessage{
			span: span,
			fail: func(err error) {
				failures[index] = err
			},
		}
	}
	return messages, failures
}
func newRelayTestHandler(t *testing.T, meterProvider metric.MeterProvider) *SpanRelayHandler {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return NewSpanRelayHandler(
		testenv.NewLogger(t),
		meterProvider,
		nil,
		testenv.NewEncryptionClient(t),
		policy,
	)
}

func cacheRelayTestDestination(
	t *testing.T,
	handler *SpanRelayHandler,
	organizationID string,
	baseURL string,
	headers map[string]string,
) *relayDestination {
	t.Helper()
	destination, err := handler.relay.newDestination(organizationID, baseURL, headers)
	require.NoError(t, err)
	handler.relay.destinationCache[organizationID] = cachedRelayDestination{
		destination: destination,
		expiresAt:   time.Now().Add(time.Hour),
	}
	return destination
}

func relaySpanCount(
	t *testing.T,
	reader *sdkmetric.ManualReader,
	metricName string,
	reason relayReason,
) int64 {
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
			for _, point := range sum.DataPoints {
				value, ok := point.Attributes.Value(attr.ReasonKey)
				if ok && value.AsString() == string(reason) {
					return point.Value
				}
			}
		}
	}
	return 0
}

func relayTestSpan(name, organizationID, projectID string) *otelv1.Span {
	resourceSchemaURL := "https://opentelemetry.io/schemas/1.27.0"
	scopeSchemaURL := "https://opentelemetry.io/schemas/1.28.0"
	return (&otelv1.Span_builder{
		TraceId:           []byte{1},
		SpanId:            []byte(name),
		Name:              &name,
		StartTimeUnixNano: new(uint64(1)),
		EndTimeUnixNano:   new(uint64(2)),
		Resource:          (&otelv1.Span_Resource_builder{Attributes: nil}).Build(),
		ResourceSchemaUrl: &resourceSchemaURL,
		Scope: (&otelv1.Span_InstrumentationScope_builder{
			Name: new("test-scope"),
		}).Build(),
		ScopeSchemaUrl: &scopeSchemaURL,
		Provenance: (&otelv1.Span_Provenance_builder{
			Source:         new("test"),
			OrganizationId: &organizationID,
			ProjectId:      &projectID,
		}).Build(),
	}).Build()
}

func relayRequestSpanNames(request *collectortracev1.ExportTraceServiceRequest) []string {
	var names []string
	for _, resourceSpans := range request.GetResourceSpans() {
		for _, scopeSpans := range resourceSpans.GetScopeSpans() {
			for _, span := range scopeSpans.GetSpans() {
				names = append(names, span.GetName())
			}
		}
	}
	return names
}
