package otel

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"

	telemetryv1 "github.com/speakeasy-api/gram/infra/gen/gram/telemetry/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestToolCallLogRelayHandlerDeliversToolCallsAndSkipsHookRows(t *testing.T) {
	t.Parallel()

	capture := &logRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)

	handler := newToolCallLogRelayTestHandler(t)
	cacheToolCallLogTestDestination(t, handler, testLogOrganizationID, testLogProjectID, server.URL, map[string]string{"X-Customer": "a"}, true)

	messages, failures := toolCallLogRelayTestMessages(
		toolCallLogTestRecord("tool-1", testLogOrganizationID, testLogProjectID, "tool_call", nil),
		// The topic mirrors every telemetry_logs row. Agent-reported tool use
		// already leaves through product telemetry, so it must not be relayed
		// a second time here.
		toolCallLogTestRecord("hook-1", testLogOrganizationID, testLogProjectID, "hook", nil),
		toolCallLogTestRecord("tool-2", testLogOrganizationID, testLogProjectID, "tool_call", nil),
	)

	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}

	requests := capture.snapshot()
	require.Len(t, requests, 1)
	require.Equal(t, "/v1/logs", requests[0].path)
	require.Equal(t, "application/x-protobuf", requests[0].contentType)
	require.Equal(t, "a", requests[0].customer)

	bodies := relayRequestLogBodies(requests[0].request)
	slices.Sort(bodies)
	require.Equal(t, []string{"tool-1", "tool-2"}, bodies)
}

func TestToolCallLogRelayHandlerRoutesByOrganizationAttributeAndProjectColumn(t *testing.T) {
	t.Parallel()

	capture := &logRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)

	handler := newToolCallLogRelayTestHandler(t)
	cacheToolCallLogTestDestination(t, handler, testLogOrganizationID, testLogProjectID, server.URL, map[string]string{"X-Customer": "a"}, true)
	cacheToolCallLogTestDestination(t, handler, testLogOtherOrganizationID, testLogOtherProjectID, server.URL, map[string]string{"X-Customer": "b"}, true)

	messages, failures := toolCallLogRelayTestMessages(
		toolCallLogTestRecord("a-1", testLogOrganizationID, testLogProjectID, "tool_call", nil),
		toolCallLogTestRecord("b-1", testLogOtherOrganizationID, testLogOtherProjectID, "tool_call", nil),
		toolCallLogTestRecord("a-2", testLogOrganizationID, testLogProjectID, "tool_call", nil),
	)

	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}

	delivered := make(map[string][]string)
	for _, captured := range capture.snapshot() {
		require.NoError(t, captured.err)
		delivered[captured.customer] = append(delivered[captured.customer], relayRequestLogBodies(captured.request)...)
	}
	slices.Sort(delivered["a"])
	require.Equal(t, []string{"a-1", "a-2"}, delivered["a"])
	require.Equal(t, []string{"b-1"}, delivered["b"])
}

func TestToolCallLogRelayHandlerDropsRecordsWithoutTenancy(t *testing.T) {
	t.Parallel()

	capture := &logRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)

	handler := newToolCallLogRelayTestHandler(t)
	cacheToolCallLogTestDestination(t, handler, testLogOrganizationID, testLogProjectID, server.URL, nil, true)

	missingOrg := toolCallLogTestRecord("no-org", "", testLogProjectID, "tool_call", nil)
	malformedProject := toolCallLogTestRecord("bad-project", testLogOrganizationID, "not-a-uuid", "tool_call", nil)
	malformedAttributes := toolCallLogTestRecord("bad-attrs", testLogOrganizationID, testLogProjectID, "tool_call", nil)
	malformedAttributes.SetAttributesJson("{not json")

	messages, failures := toolCallLogRelayTestMessages(missingOrg, malformedProject, malformedAttributes)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}
	require.Empty(t, capture.snapshot())
}

func TestToolCallLogRelayHandlerRedactsToolIOForExcludingDestinations(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		it                   string
		includeSensitiveData bool
		wantArguments        string
		wantResult           string
	}{
		{
			it:                   "keeps tool IO when the destination includes sensitive data",
			includeSensitiveData: true,
			wantArguments:        `{"query":"secret"}`,
			wantResult:           `{"rows":["secret"]}`,
		},
		{
			it:                   "redacts tool IO when the destination excludes sensitive data",
			includeSensitiveData: false,
			wantArguments:        redactedSensitiveDataValue,
			wantResult:           redactedSensitiveDataValue,
		},
	} {
		t.Run(tt.it, func(t *testing.T) {
			t.Parallel()

			capture := &logRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
			server := httptest.NewServer(http.HandlerFunc(capture.handler))
			t.Cleanup(server.Close)

			handler := newToolCallLogRelayTestHandler(t)
			cacheToolCallLogTestDestination(t, handler, testLogOrganizationID, testLogProjectID, server.URL, nil, tt.includeSensitiveData)

			record := toolCallLogTestRecord("tool-1", testLogOrganizationID, testLogProjectID, "tool_call", map[string]any{
				string(attr.GenAIToolCallArgumentsKey): `{"query":"secret"}`,
				string(attr.GenAIToolCallResultKey):    `{"rows":["secret"]}`,
				string(attr.ToolNameKey):               "search_logs",
			})
			messages, failures := toolCallLogRelayTestMessages(record)
			require.NoError(t, handler.handleBatch(t.Context(), messages))
			require.NoError(t, failures[0])

			requests := capture.snapshot()
			require.Len(t, requests, 1)
			delivered := toolCallLogTestDeliveredRecords(t, requests[0].request)
			require.Len(t, delivered, 1)

			attributes := toolCallLogTestAttributeMap(delivered[0])
			require.Equal(t, tt.wantArguments, attributes[string(attr.GenAIToolCallArgumentsKey)].GetStringValue())
			require.Equal(t, tt.wantResult, attributes[string(attr.GenAIToolCallResultKey)].GetStringValue())
			// A non-sensitive attribute on the same record is untouched either way.
			require.Equal(t, "search_logs", attributes[string(attr.ToolNameKey)].GetStringValue())
		})
	}
}

func TestToolCallLogRelayHandlerConvertsRowShapeToOTLP(t *testing.T) {
	t.Parallel()

	capture := &logRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)

	handler := newToolCallLogRelayTestHandler(t)
	cacheToolCallLogTestDestination(t, handler, testLogOrganizationID, testLogProjectID, server.URL, nil, true)

	record := toolCallLogTestRecord("tool-1", testLogOrganizationID, testLogProjectID, "tool_call", map[string]any{
		string(attr.HTTPResponseStatusCodeKey): 500,
		// Nanosecond timestamps are past 2^53, where float64 stops holding
		// integers exactly. Every row carries one, so a float round trip in
		// the decoder would corrupt them all.
		string(attr.TimeUnixNanoKey): int64(1789691307487308787),
		"gram.test.ratio":            0.25,
		"gram.test.flag":             true,
		"gram.test.list":             []any{"a", "b"},
	})
	record.SetSeverityText("ERROR")
	record.SetTraceId("72208a3b8032e3c9f99cff91650d177e")
	record.SetSpanId("0673e4bae5a1e5c8")

	messages, failures := toolCallLogRelayTestMessages(record)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	require.NoError(t, failures[0])

	requests := capture.snapshot()
	require.Len(t, requests, 1)
	delivered := toolCallLogTestDeliveredRecords(t, requests[0].request)
	require.Len(t, delivered, 1)
	converted := delivered[0]

	require.Equal(t, "ERROR", converted.GetSeverityText())
	require.Equal(t, logsv1.SeverityNumber_SEVERITY_NUMBER_ERROR, converted.GetSeverityNumber())
	require.Equal(t, toolCallLogEventName, converted.GetEventName())
	require.Len(t, converted.GetTraceId(), 16)
	require.Len(t, converted.GetSpanId(), 8)

	attributes := toolCallLogTestAttributeMap(converted)
	// Whole numbers must not arrive as doubles: a status code rendered as
	// 5e+02 is unusable for a destination filtering on it.
	require.Equal(t, int64(500), attributes[string(attr.HTTPResponseStatusCodeKey)].GetIntValue())
	require.Equal(t, int64(1789691307487308787), attributes[string(attr.TimeUnixNanoKey)].GetIntValue())
	require.InDelta(t, 0.25, attributes["gram.test.ratio"].GetDoubleValue(), 0.0001)
	require.True(t, attributes["gram.test.flag"].GetBoolValue())
	require.Len(t, attributes["gram.test.list"].GetArrayValue().GetValues(), 2)
	// The ledger id travels with the record so an at-least-once redelivery is
	// dedupable at the destination.
	require.Equal(t, "log-tool-1", attributes[string(attr.TelemetryLogIDKey)].GetStringValue())

	resourceAttributes := requests[0].request.GetResourceLogs()[0].GetResource().GetAttributes()
	require.Len(t, resourceAttributes, 1)
	require.Equal(t, string(attr.ServiceNameKey), resourceAttributes[0].GetKey())
}

func TestToolCallLogRelayHandlerKeepsRecordsUnderTheirOwnResource(t *testing.T) {
	t.Parallel()

	capture := &logRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)

	handler := newToolCallLogRelayTestHandler(t)
	cacheToolCallLogTestDestination(t, handler, testLogOrganizationID, testLogProjectID, server.URL, nil, true)

	// Resource attributes carry the deployment, so one project's batch can
	// span several. Stamping the whole export with the first row's resource
	// would misattribute the rest.
	first := toolCallLogTestRecord("deploy-a", testLogOrganizationID, testLogProjectID, "tool_call", nil)
	first.SetResourceAttributesJson(`{"service.name":"gram-server","gram.deployment.id":"deployment-a"}`)
	second := toolCallLogTestRecord("deploy-b", testLogOrganizationID, testLogProjectID, "tool_call", nil)
	second.SetResourceAttributesJson(`{"service.name":"gram-server","gram.deployment.id":"deployment-b"}`)

	messages, failures := toolCallLogRelayTestMessages(first, second)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}

	requests := capture.snapshot()
	require.Len(t, requests, 1)
	resourceLogs := requests[0].request.GetResourceLogs()
	require.Len(t, resourceLogs, 2)

	deploymentByBody := make(map[string]string, 2)
	for _, resourceLog := range resourceLogs {
		var deployment string
		for _, keyValue := range resourceLog.GetResource().GetAttributes() {
			if keyValue.GetKey() == "gram.deployment.id" {
				deployment = keyValue.GetValue().GetStringValue()
			}
		}
		for _, scopeLogs := range resourceLog.GetScopeLogs() {
			for _, record := range scopeLogs.GetLogRecords() {
				deploymentByBody[record.GetBody().GetStringValue()] = deployment
			}
		}
	}
	require.Equal(t, map[string]string{
		"deploy-a": "deployment-a",
		"deploy-b": "deployment-b",
	}, deploymentByBody)
}

func TestToolCallLogRelayHandlerSkipsProjectsWithoutARoute(t *testing.T) {
	t.Parallel()

	capture := &logRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)

	db := newTestDatabase(t)
	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	handler := NewToolCallLogRelayHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), db, enc, policy)

	routedProjectID := createRelayTestProject(t, db, "org-test")
	unroutedProjectID := createRelayTestProject(t, db, "org-test")
	destination := createRelayTestDestination(
		t,
		db,
		"org-test",
		routedProjectID,
		server.URL,
		encryptRelayTestHeaders(t, enc, map[string]string{"X-Customer": "routed"}),
		"include",
	)
	createRelayTestRoute(t, db, "org-test", routedProjectID, dataexports.DataSourceToolCallLogs, true, uuid.NullUUID{UUID: destination.ID, Valid: true})
	// A product telemetry route on the other project must not pull tool call
	// logs along with it: the two data sources are configured independently.
	productTelemetryDestination := createRelayTestDestination(
		t,
		db,
		"org-test",
		unroutedProjectID,
		server.URL,
		encryptRelayTestHeaders(t, enc, map[string]string{"X-Customer": "unrouted"}),
		"include",
	)
	createRelayTestRoute(t, db, "org-test", unroutedProjectID, dataexports.DataSourceProductTelemetry, true, uuid.NullUUID{UUID: productTelemetryDestination.ID, Valid: true})

	messages, failures := toolCallLogRelayTestMessages(
		toolCallLogTestRecord("routed", "org-test", routedProjectID.String(), "tool_call", nil),
		toolCallLogTestRecord("unrouted", "org-test", unroutedProjectID.String(), "tool_call", nil),
	)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}

	requests := capture.snapshot()
	require.Len(t, requests, 1)
	require.Equal(t, "routed", requests[0].customer)
	require.Equal(t, []string{"routed"}, relayRequestLogBodies(requests[0].request))
}

func TestToolCallLogRelayHandlerFailsRetryableDeliveriesForRedelivery(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	handler := newToolCallLogRelayTestHandler(t)
	cacheToolCallLogTestDestination(t, handler, testLogOrganizationID, testLogProjectID, server.URL, nil, true)

	messages, failures := toolCallLogRelayTestMessages(
		toolCallLogTestRecord("tool-1", testLogOrganizationID, testLogProjectID, "tool_call", nil),
	)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	require.ErrorContains(t, failures[0], "500")
}

func newToolCallLogRelayTestHandler(t *testing.T) *ToolCallLogRelayHandler {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return NewToolCallLogRelayHandler(
		testenv.NewLogger(t),
		testenv.NewMeterProvider(t),
		nil,
		testenv.NewEncryptionClient(t),
		policy,
	)
}

func cacheToolCallLogTestDestination(
	t *testing.T,
	handler *ToolCallLogRelayHandler,
	organizationID string,
	projectID string,
	baseURL string,
	headers map[string]string,
	includeSensitiveData bool,
) {
	t.Helper()
	key := relayTestRouteKey(organizationID, projectID)
	destination, err := handler.relay.newDestination(key, baseURL, headers, includeSensitiveData)
	require.NoError(t, err)
	handler.relay.destinationCache[key] = cachedRelayDestination{
		destination: destination,
		expiresAt:   time.Now().Add(time.Hour),
	}
}

func toolCallLogRelayTestMessages(records ...*telemetryv1.LogRecord) ([]toolCallLogRelayMessage, []error) {
	failures := make([]error, len(records))
	messages := make([]toolCallLogRelayMessage, len(records))
	for i, record := range records {
		index := i
		messages[i] = toolCallLogRelayMessage{
			record: record,
			fail: func(err error) {
				failures[index] = err
			},
		}
	}
	return messages, failures
}

// toolCallLogTestRecord builds the shape telemetry.LogPublisher mirrors onto
// the topic: tenancy split between the project column and the gram.org.id
// attribute, with the row's attributes carried as a JSON payload.
func toolCallLogTestRecord(
	body string,
	organizationID string,
	projectID string,
	eventSource string,
	extraAttributes map[string]any,
) *telemetryv1.LogRecord {
	attributes := map[string]any{
		string(attr.EventSourceKey): eventSource,
	}
	if organizationID != "" {
		attributes[string(attr.OrganizationIDKey)] = organizationID
	}
	maps.Copy(attributes, extraAttributes)
	attributesJSON, err := json.Marshal(attributes)
	if err != nil {
		panic(err)
	}
	resourceJSON, err := json.Marshal(map[string]any{string(attr.ServiceNameKey): "gram-server"})
	if err != nil {
		panic(err)
	}

	id := "log-" + body
	timeUnixNano := time.Now().UnixNano()
	severityText := "INFO"
	serviceName := "gram-server"
	attributesPayload := string(attributesJSON)
	resourcePayload := string(resourceJSON)
	return (&telemetryv1.LogRecord_builder{
		Id:                     &id,
		TimeUnixNano:           &timeUnixNano,
		ObservedTimeUnixNano:   &timeUnixNano,
		SeverityText:           &severityText,
		Body:                   &body,
		AttributesJson:         &attributesPayload,
		ResourceAttributesJson: &resourcePayload,
		GramProjectId:          &projectID,
		GramUrn:                new(string),
		ServiceName:            &serviceName,
	}).Build()
}

func toolCallLogTestDeliveredRecords(t *testing.T, request *collectorlogsv1.ExportLogsServiceRequest) []*logsv1.LogRecord {
	t.Helper()
	require.Len(t, request.GetResourceLogs(), 1)
	require.Len(t, request.GetResourceLogs()[0].GetScopeLogs(), 1)
	require.Equal(t, toolCallLogScopeName, request.GetResourceLogs()[0].GetScopeLogs()[0].GetScope().GetName())
	return request.GetResourceLogs()[0].GetScopeLogs()[0].GetLogRecords()
}

func toolCallLogTestAttributeMap(record *logsv1.LogRecord) map[string]*commonv1.AnyValue {
	attributes := make(map[string]*commonv1.AnyValue, len(record.GetAttributes()))
	for _, keyValue := range record.GetAttributes() {
		attributes[keyValue.GetKey()] = keyValue.GetValue()
	}
	return attributes
}
