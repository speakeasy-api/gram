package otel

import (
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
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

type capturedLogRelayRequest struct {
	customer    string
	path        string
	contentType string
	bodySize    int
	request     *collectorlogsv1.ExportLogsServiceRequest
	err         error
}

type logRelayRequestCapture struct {
	mu       sync.Mutex
	requests []capturedLogRelayRequest
}

func (c *logRelayRequestCapture) handler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	request := &collectorlogsv1.ExportLogsServiceRequest{}
	if err == nil {
		err = proto.Unmarshal(body, request)
	}

	c.mu.Lock()
	c.requests = append(c.requests, capturedLogRelayRequest{
		customer:    r.Header.Get("X-Customer"),
		path:        r.URL.Path,
		contentType: r.Header.Get("Content-Type"),
		bodySize:    len(body),
		request:     request,
		err:         err,
	})
	c.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (c *logRelayRequestCapture) snapshot() []capturedLogRelayRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.requests)
}

func TestLogRelayHandlerGroupsByProvenanceAndCachesDestinations(t *testing.T) {
	t.Parallel()

	capture := &logRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)
	handler := newLogRelayTestHandler(t, testenv.NewMeterProvider(t))
	destinationA := cacheLogRelayTestDestination(t, handler, "org-a", server.URL, map[string]string{"X-Customer": "a"})
	cacheLogRelayTestDestination(t, handler, "org-b", server.URL, map[string]string{"X-Customer": "b"})
	require.Equal(t, 10*time.Second, destinationA.httpClient.Timeout)

	messages, failures := logRelayTestMessages(
		relayTestLogRecord("a-1", "org-a", "project-1", 0),
		relayTestLogRecord("a-2", "org-a", "project-1", 0),
		relayTestLogRecord("a-3", "org-a", "project-2", 0),
		relayTestLogRecord("b-1", "org-b", "project-1", 0),
	)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}

	messages, failures = logRelayTestMessages(relayTestLogRecord("a-4", "org-a", "project-1", 0))
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	require.NoError(t, failures[0])

	requests := capture.snapshot()
	require.Len(t, requests, 4)
	groupedBodies := make(map[string][]string)
	for _, captured := range requests {
		require.NoError(t, captured.err)
		require.Equal(t, "/v1/logs", captured.path)
		require.Equal(t, "application/x-protobuf", captured.contentType)

		bodies := relayRequestLogBodies(captured.request)
		slices.Sort(bodies)
		groupedBodies[captured.customer] = append(groupedBodies[captured.customer], bodies...)
		if slices.Equal(bodies, []string{"a-1", "a-2"}) {
			require.Len(t, captured.request.GetResourceLogs(), 1)
			require.Len(t, captured.request.GetResourceLogs()[0].GetScopeLogs(), 1)
			require.Len(t, captured.request.GetResourceLogs()[0].GetScopeLogs()[0].GetLogRecords(), 2)
		}
	}
	slices.Sort(groupedBodies["a"])
	slices.Sort(groupedBodies["b"])
	require.Equal(t, []string{"a-1", "a-2", "a-3", "a-4"}, groupedBodies["a"])
	require.Equal(t, []string{"b-1"}, groupedBodies["b"])
}

func TestLogRelayHandlerLimitsDestinationRequestsToFourMiB(t *testing.T) {
	t.Parallel()

	capture := &logRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)
	handler := newLogRelayTestHandler(t, testenv.NewMeterProvider(t))
	cacheLogRelayTestDestination(t, handler, "org-a", server.URL, nil)

	largeBodyBytes := maxLogRelayExportBytes/2 + 128*1024
	messages, failures := logRelayTestMessages(
		relayTestLogRecord("large-1", "org-a", "project-1", largeBodyBytes),
		relayTestLogRecord("large-2", "org-a", "project-1", largeBodyBytes),
	)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}

	requests := capture.snapshot()
	require.Len(t, requests, 2)
	deliveredRecords := 0
	for _, captured := range requests {
		require.NoError(t, captured.err)
		require.LessOrEqual(t, captured.bodySize, maxLogRelayExportBytes)
		for _, resourceLogs := range captured.request.GetResourceLogs() {
			for _, scopeLogs := range resourceLogs.GetScopeLogs() {
				deliveredRecords += len(scopeLogs.GetLogRecords())
			}
		}
	}
	require.Equal(t, 2, deliveredRecords)
}

func TestLogRelayHandlerDropsSingleRecordOverFourMiB(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	handler := newLogRelayTestHandler(t, testenv.NewMeterProvider(t))
	cacheLogRelayTestDestination(t, handler, "org-a", server.URL, nil)

	messages, failures := logRelayTestMessages(
		relayTestLogRecord("oversized", "org-a", "project-1", maxLogRelayExportBytes),
	)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	require.NoError(t, failures[0])
	require.Zero(t, requests.Load())
}

func TestLogRelayDestinationRejectsRequestOverFourMiB(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	handler := newLogRelayTestHandler(t, testenv.NewMeterProvider(t))
	destination := cacheLogRelayTestDestination(t, handler, "org-a", server.URL, nil)
	request, err := newLogRelayExportRequest([]*otelv1.LogRecord{
		relayTestLogRecord("oversized", "org-a", "project-1", maxLogRelayExportBytes),
	})
	require.NoError(t, err)

	err = destination.exportWithLimit(t.Context(), request, maxLogRelayExportBytes)

	require.ErrorContains(t, err, "limit is 4194304")
	require.Zero(t, requests.Load())
}

func TestLogRelayHandlerFailsMessagesForRetryableDestination(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	handler := newLogRelayTestHandler(t, testenv.NewMeterProvider(t))
	cacheLogRelayTestDestination(t, handler, "org-a", server.URL, nil)

	messages, failures := logRelayTestMessages(
		relayTestLogRecord("failed", "org-a", "project-1", 0),
	)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	require.ErrorContains(t, failures[0], "503 Service Unavailable")
	require.Equal(t, int64(2), requests.Load(), "Guardian should make one retry before Pub/Sub stages redelivery")
}

func TestNewLogRelayExportRequestDiscardsGramOnlyFields(t *testing.T) {
	t.Parallel()

	record := relayTestLogRecord("visible", "internal-organization-id", "internal-project-id", 0)
	futureOTLPField := protowire.AppendTag(nil, 13, protowire.BytesType)
	futureOTLPField = protowire.AppendString(futureOTLPField, "future-otlp-value")
	futureGramField := protowire.AppendTag(nil, 1006, protowire.BytesType)
	futureGramField = protowire.AppendString(futureGramField, "internal-future-value")
	record.ProtoReflect().SetUnknown(append(futureOTLPField, futureGramField...))

	request, err := newLogRelayExportRequest([]*otelv1.LogRecord{record})
	require.NoError(t, err)
	require.Len(t, request.GetResourceLogs(), 1)
	require.Len(t, request.GetResourceLogs()[0].GetScopeLogs(), 1)
	require.Len(t, request.GetResourceLogs()[0].GetScopeLogs()[0].GetLogRecords(), 1)

	converted := request.GetResourceLogs()[0].GetScopeLogs()[0].GetLogRecords()[0]
	require.Equal(t, futureOTLPField, []byte(converted.ProtoReflect().GetUnknown()))

	encoded, err := proto.Marshal(request)
	require.NoError(t, err)
	for _, internalValue := range []string{
		"record-visible",
		"internal-organization-id",
		"internal-project-id",
		"internal-future-value",
	} {
		require.NotContains(t, string(encoded), internalValue)
	}
}

func logRelayTestMessages(records ...*otelv1.LogRecord) ([]logRelayMessage, []error) {
	failures := make([]error, len(records))
	messages := make([]logRelayMessage, len(records))
	for i, record := range records {
		index := i
		messages[i] = logRelayMessage{
			record: record,
			fail: func(err error) {
				failures[index] = err
			},
		}
	}
	return messages, failures
}

func newLogRelayTestHandler(t *testing.T, meterProvider metric.MeterProvider) *LogRelayHandler {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return NewLogRelayHandler(
		testenv.NewLogger(t),
		meterProvider,
		nil,
		testenv.NewEncryptionClient(t),
		policy,
	)
}

func cacheLogRelayTestDestination(
	t *testing.T,
	handler *LogRelayHandler,
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

func relayTestLogRecord(body, organizationID, projectID string, bodyBytes int) *otelv1.LogRecord {
	resourceSchemaURL := "https://opentelemetry.io/schemas/1.27.0"
	scopeSchemaURL := "https://opentelemetry.io/schemas/1.28.0"
	var recordBody *otelv1.LogRecord_AnyValue
	if bodyBytes > 0 {
		recordBody = (&otelv1.LogRecord_AnyValue_builder{BytesValue: make([]byte, bodyBytes)}).Build()
	} else {
		recordBody = (&otelv1.LogRecord_AnyValue_builder{StringValue: &body}).Build()
	}
	recordID := "record-" + body
	return (&otelv1.LogRecord_builder{
		RecordId:          &recordID,
		Body:              recordBody,
		Resource:          (&otelv1.LogRecord_Resource_builder{Attributes: nil}).Build(),
		ResourceSchemaUrl: &resourceSchemaURL,
		Scope: (&otelv1.LogRecord_InstrumentationScope_builder{
			Name: new("com.speakeasy.ai.logging"),
		}).Build(),
		ScopeSchemaUrl: &scopeSchemaURL,
		Provenance: (&otelv1.LogRecord_Provenance_builder{
			Source:         new("test"),
			OrganizationId: &organizationID,
			ProjectId:      &projectID,
		}).Build(),
	}).Build()
}

func relayRequestLogBodies(request *collectorlogsv1.ExportLogsServiceRequest) []string {
	var bodies []string
	for _, resourceLogs := range request.GetResourceLogs() {
		for _, scopeLogs := range resourceLogs.GetScopeLogs() {
			for _, record := range scopeLogs.GetLogRecords() {
				bodies = append(bodies, record.GetBody().GetStringValue())
			}
		}
	}
	return bodies
}
