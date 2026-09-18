package otel

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"

	telemetryv1 "github.com/speakeasy-api/gram/infra/gen/gram/telemetry/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

var toolCallLogRelayObservedAt = time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)

const (
	toolCallLogRelayTraceID = "0af7651916cd43dd8448eb211c80319c"
	toolCallLogRelaySpanID  = "b7ad6b7169203331"
)

func TestToolCallLogRelayExportsToolCallRow(t *testing.T) {
	t.Parallel()

	requests := make(chan *collectorlogsv1.ExportLogsServiceRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/logs" {
			t.Errorf("request path = %q, want /v1/logs", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		request := new(collectorlogsv1.ExportLogsServiceRequest)
		if err := proto.Unmarshal(body, request); err != nil {
			t.Errorf("unmarshal request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- request
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	handler, projectID := newToolCallLogRelayTestHandler(t, server.URL, "include")
	record := toolCallLogRelayTestRecord(projectID, nil)

	require.NoError(t, handler.HandleBatchWithResult(t.Context(), []gcp.BatchMessage[*telemetryv1.LogRecord]{{Message: record}}))

	request := requireToolCallLogRelayRequest(t, requests)
	resourceLogs := requireOne(t, request.GetResourceLogs())
	require.Equal(t, "gram-server", otlpStringAttribute(resourceLogs.GetResource().GetAttributes(), string(attr.ServiceNameKey)))
	require.Equal(t, "v1.2.3", otlpStringAttribute(resourceLogs.GetResource().GetAttributes(), string(attr.ServiceVersionKey)))
	scopeLogs := requireOne(t, resourceLogs.GetScopeLogs())
	require.Equal(t, toolCallLogScopeName, scopeLogs.GetScope().GetName())
	logRecord := requireOne(t, scopeLogs.GetLogRecords())

	require.Equal(t, toolCallLogEventName, logRecord.GetEventName())
	require.Equal(t, uint64(record.GetTimeUnixNano()), logRecord.GetTimeUnixNano())
	require.Equal(t, uint64(record.GetObservedTimeUnixNano()), logRecord.GetObservedTimeUnixNano())
	require.Equal(t, "ERROR", logRecord.GetSeverityText())
	require.Equal(t, logsv1.SeverityNumber_SEVERITY_NUMBER_ERROR, logRecord.GetSeverityNumber())
	require.Equal(t, mustDecodeHex(t, toolCallLogRelayTraceID), logRecord.GetTraceId())
	require.Equal(t, mustDecodeHex(t, toolCallLogRelaySpanID), logRecord.GetSpanId())

	attributes := logRecord.GetAttributes()
	require.Equal(t, record.GetId(), otlpStringAttribute(attributes, string(attr.TelemetryLogIDKey)))
	require.Equal(t, "org-test", otlpStringAttribute(attributes, string(attr.OrganizationIDKey)))
	require.Equal(t, projectID.String(), otlpStringAttribute(attributes, string(attr.ProjectIDKey)))
	require.Equal(t, "tool_call", otlpStringAttribute(attributes, string(attr.EventSourceKey)))
	require.Equal(t, "check_health", otlpStringAttribute(attributes, string(attr.ToolNameKey)))
	require.Equal(t, int64(422), otlpIntAttribute(attributes, string(attr.HTTPResponseStatusCodeKey)))
	require.InDelta(t, 12.5, otlpDoubleAttribute(attributes, string(attr.HTTPServerRequestDurationKey)), 0.0001)
	require.JSONEq(t, `{"zip":"not-a-zip"}`, otlpStringAttribute(attributes, string(attr.GenAIToolCallArgumentsKey)))

	// The row's own columns already carry these; re-sending them as
	// attributes would only inflate the payload.
	require.False(t, hasOTLPAttribute(attributes, string(attr.TimeUnixNanoKey)))
	require.False(t, hasOTLPAttribute(attributes, string(attr.ObservedTimeUnixNanoKey)))
}

func TestToolCallLogRelayRedactsToolIOWithExcludePolicy(t *testing.T) {
	t.Parallel()

	requests := make(chan *collectorlogsv1.ExportLogsServiceRequest, 1)
	bodies := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		request := new(collectorlogsv1.ExportLogsServiceRequest)
		if err := proto.Unmarshal(body, request); err != nil {
			t.Errorf("unmarshal request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		bodies <- body
		requests <- request
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	handler, projectID := newToolCallLogRelayTestHandler(t, server.URL, "exclude")
	record := toolCallLogRelayTestRecord(projectID, nil)

	require.NoError(t, handler.HandleBatchWithResult(t.Context(), []gcp.BatchMessage[*telemetryv1.LogRecord]{{Message: record}}))

	request := requireToolCallLogRelayRequest(t, requests)
	logRecord := requireOne(t, requireOne(t, requireOne(t, request.GetResourceLogs()).GetScopeLogs()).GetLogRecords())
	attributes := logRecord.GetAttributes()
	require.Equal(t, redactedSensitiveDataValue, otlpStringAttribute(attributes, string(attr.GenAIToolCallArgumentsKey)))
	require.Equal(t, redactedSensitiveDataValue, otlpStringAttribute(attributes, string(attr.GenAIToolCallResultKey)))
	require.Equal(t, redactedSensitiveDataValue, otlpStringAttribute(attributes, string(attr.UserEmailKey)))
	// Operational attributes stay readable: without them a destination cannot
	// tell a failing tool from a healthy one.
	require.Equal(t, "check_health", otlpStringAttribute(attributes, string(attr.ToolNameKey)))
	require.Equal(t, int64(422), otlpIntAttribute(attributes, string(attr.HTTPResponseStatusCodeKey)))

	select {
	case body := <-bodies:
		require.NotContains(t, string(body), "not-a-zip")
		require.NotContains(t, string(body), "caller@example.invalid")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for tool call log relay request body")
	}
}

func TestToolCallLogRelaySelectsOnlyToolCallRows(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse(testLogProjectID)
	tests := []struct {
		name        string
		mutate      func(*telemetryv1.LogRecord)
		eligibility toolCallLogEligibility
	}{
		{name: "tool call", mutate: func(*telemetryv1.LogRecord) {}, eligibility: toolCallLogEligible},
		{
			name: "chat completion",
			mutate: func(record *telemetryv1.LogRecord) {
				setToolCallLogRelayAttributes(record, map[string]any{string(attr.EventSourceKey): "chat_completion"})
			},
			eligibility: toolCallLogOtherSource,
		},
		{
			name: "agent hook event",
			mutate: func(record *telemetryv1.LogRecord) {
				setToolCallLogRelayAttributes(record, map[string]any{string(attr.EventSourceKey): "hook"})
			},
			eligibility: toolCallLogOtherSource,
		},
		{
			name: "missing event source",
			mutate: func(record *telemetryv1.LogRecord) {
				setToolCallLogRelayAttributes(record, map[string]any{})
			},
			eligibility: toolCallLogOtherSource,
		},
		{
			name: "malformed attributes",
			mutate: func(record *telemetryv1.LogRecord) {
				record.SetAttributesJson("{")
			},
			eligibility: toolCallLogOtherSource,
		},
		{
			name: "missing organization",
			mutate: func(record *telemetryv1.LogRecord) {
				setToolCallLogRelayAttributes(record, map[string]any{
					string(attr.EventSourceKey): "tool_call",
				})
			},
			eligibility: toolCallLogUnroutable,
		},
		{
			name: "malformed project",
			mutate: func(record *telemetryv1.LogRecord) {
				record.SetGramProjectId("not-a-uuid")
			},
			eligibility: toolCallLogUnroutable,
		},
		{
			name: "missing timestamp",
			mutate: func(record *telemetryv1.LogRecord) {
				record.SetTimeUnixNano(0)
			},
			eligibility: toolCallLogUnroutable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			record := toolCallLogRelayTestRecord(projectID, nil)
			tt.mutate(record)
			message, eligibility := newToolCallLogRelayMessage(record, nil)
			require.Equal(t, tt.eligibility, eligibility)
			if eligibility == toolCallLogEligible {
				require.Equal(t, "org-test", message.key.organizationID)
				require.Equal(t, projectID, message.key.projectID)
			}
		})
	}
}

func TestToolCallLogRelayGroupsRowsByResource(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse(testLogProjectID)
	first := toolCallLogRelayTestRecord(projectID, nil)
	second := toolCallLogRelayTestRecord(projectID, nil)
	second.SetId("0199cb4f-4840-70e6-9e1d-5558dc2d7ce9")
	third := toolCallLogRelayTestRecord(projectID, nil)
	third.SetId("0199cb4f-4840-70e6-9e1d-5558dc2d7cea")
	third.SetResourceAttributesJson(`{"service.name":"gram-server","service.version":"v9.9.9"}`)

	messages := make([]toolCallLogRelayMessage, 0, 3)
	for _, record := range []*telemetryv1.LogRecord{first, second, third} {
		message, eligibility := newToolCallLogRelayMessage(record, nil)
		require.Equal(t, toolCallLogEligible, eligibility)
		messages = append(messages, message)
	}

	request, err := buildToolCallLogRelayExport(messages, toolCallLogRelayObservedAt, true)
	require.NoError(t, err)
	require.Len(t, request.GetResourceLogs(), 2)
	require.Len(t, requireOne(t, request.GetResourceLogs()[0].GetScopeLogs()).GetLogRecords(), 2)
	require.Len(t, requireOne(t, request.GetResourceLogs()[1].GetScopeLogs()).GetLogRecords(), 1)
	require.Equal(
		t,
		"v9.9.9",
		otlpStringAttribute(request.GetResourceLogs()[1].GetResource().GetAttributes(), string(attr.ServiceVersionKey)),
	)
}

// TestToolCallLogRelayBuildsDeterministically pins the property
// rightSizeProtoBatches depends on: the same rows must always encode to the
// same bytes, or its size search would pick batches it cannot reproduce.
func TestToolCallLogRelayBuildsDeterministically(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse(testLogProjectID)
	record := toolCallLogRelayTestRecord(projectID, map[string]any{
		"gram.tool_call.headers": map[string]any{"b": "2", "a": "1", "c": "3"},
	})
	message, eligibility := newToolCallLogRelayMessage(record, nil)
	require.Equal(t, toolCallLogEligible, eligibility)

	first, err := buildToolCallLogRelayExport([]toolCallLogRelayMessage{message}, toolCallLogRelayObservedAt, true)
	require.NoError(t, err)
	firstWire, err := proto.Marshal(first)
	require.NoError(t, err)
	for range 8 {
		next, err := buildToolCallLogRelayExport([]toolCallLogRelayMessage{message}, toolCallLogRelayObservedAt, true)
		require.NoError(t, err)
		nextWire, err := proto.Marshal(next)
		require.NoError(t, err)
		require.Equal(t, firstWire, nextWire)
	}
}

func TestToolCallLogRelayRightSizesExports(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse(testLogProjectID)
	messages := make([]toolCallLogRelayMessage, 3)
	for i := range messages {
		record := toolCallLogRelayTestRecord(projectID, map[string]any{
			string(attr.GenAIToolCallResultKey): strings.Repeat("x", maxLogRelayExportBytes/2),
		})
		record.SetId(uuid.NewString())
		message, eligibility := newToolCallLogRelayMessage(record, nil)
		require.Equal(t, toolCallLogEligible, eligibility)
		messages[i] = message
	}

	batches, err := rightSizeProtoBatches(messages, maxLogRelayExportBytes, func(batch []toolCallLogRelayMessage) (*collectorlogsv1.ExportLogsServiceRequest, error) {
		return buildToolCallLogRelayExport(batch, toolCallLogRelayObservedAt, true)
	})
	require.NoError(t, err)
	require.Greater(t, len(batches), 1)
	for _, batch := range batches {
		require.LessOrEqual(t, proto.Size(batch.message), maxLogRelayExportBytes)
	}
}

func TestToolCallLogRelaySendsNothingWithoutRoute(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	db, enc, productRelay := newRelayRouteTest(t, "/v1/logs")
	projectID := createRelayTestProject(t, db, "org-test")
	destination := createRelayTestDestination(t, db, "org-test", projectID, server.URL, pgtype.Text{}, "exclude")
	// A product telemetry route must not pick up tool call logs: each data
	// source is configured, and billed for, on its own.
	createRelayTestRoute(t, db, "org-test", projectID, dataexports.DataSourceProductTelemetry, true, uuid.NullUUID{UUID: destination.ID, Valid: true})
	handler := newToolCallLogRelayTestHandlerFor(t, db, enc, productRelay)

	require.NoError(t, handler.HandleBatchWithResult(t.Context(), []gcp.BatchMessage[*telemetryv1.LogRecord]{
		{Message: toolCallLogRelayTestRecord(projectID, nil)},
	}))

	require.Zero(t, requests.Load())
}

func TestToolCallLogRelayIsolatesCollectorFailuresByProject(t *testing.T) {
	t.Parallel()

	var failingRequests atomic.Int64
	failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		failingRequests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failingServer.Close)

	successRequests := make(chan *collectorlogsv1.ExportLogsServiceRequest, 1)
	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		export := new(collectorlogsv1.ExportLogsServiceRequest)
		if err := proto.Unmarshal(body, export); err != nil {
			t.Errorf("unmarshal request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		successRequests <- export
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(successServer.Close)

	db, enc, productRelay := newRelayRouteTest(t, "/v1/logs")
	failingProjectID := createRelayTestProject(t, db, "org-test")
	successProjectID := createRelayTestProject(t, db, "org-test")
	failingDestination := createRelayTestDestination(t, db, "org-test", failingProjectID, failingServer.URL, pgtype.Text{}, "exclude")
	successDestination := createRelayTestDestination(t, db, "org-test", successProjectID, successServer.URL, pgtype.Text{}, "exclude")
	createRelayTestRoute(t, db, "org-test", failingProjectID, dataexports.DataSourceToolCallLogs, true, uuid.NullUUID{UUID: failingDestination.ID, Valid: true})
	createRelayTestRoute(t, db, "org-test", successProjectID, dataexports.DataSourceToolCallLogs, true, uuid.NullUUID{UUID: successDestination.ID, Valid: true})
	handler := newToolCallLogRelayTestHandlerFor(t, db, enc, productRelay)

	failing, eligibility := newToolCallLogRelayMessage(toolCallLogRelayTestRecord(failingProjectID, nil), nil)
	require.Equal(t, toolCallLogEligible, eligibility)
	successRecord := toolCallLogRelayTestRecord(successProjectID, nil)
	successRecord.SetId("0199cb4f-4840-70e6-9e1d-5558dc2d7ceb")
	success, eligibility := newToolCallLogRelayMessage(successRecord, nil)
	require.Equal(t, toolCallLogEligible, eligibility)
	failingErrors := make(chan error, 1)
	successErrors := make(chan error, 1)
	failing.fail = func(err error) { failingErrors <- err }
	success.fail = func(err error) { successErrors <- err }

	require.NoError(t, handler.handleBatch(t.Context(), []toolCallLogRelayMessage{failing, success}))

	require.Error(t, <-failingErrors)
	select {
	case err := <-successErrors:
		require.NoError(t, err)
	default:
	}
	require.Equal(t, int64(2), failingRequests.Load(), "Guardian retries the affected 5xx route once")
	export := requireToolCallLogRelayRequest(t, successRequests)
	logRecord := requireOne(t, requireOne(t, requireOne(t, export.GetResourceLogs()).GetScopeLogs()).GetLogRecords())
	require.Equal(t, successRecord.GetId(), otlpStringAttribute(logRecord.GetAttributes(), string(attr.TelemetryLogIDKey)))
}

func TestToolCallLogAnyValuePreservesJSONKinds(t *testing.T) {
	t.Parallel()

	values, err := decodeTelemetryLogJSONObject(`{
		"count": 7,
		"ratio": 1.5,
		"flag": true,
		"text": "value",
		"missing": null,
		"list": [1, "two"],
		"nested": {"inner": 3}
	}`)
	require.NoError(t, err)

	require.Equal(t, int64(7), toolCallLogAnyValue(values["count"], 0).GetIntValue())
	require.InDelta(t, 1.5, toolCallLogAnyValue(values["ratio"], 0).GetDoubleValue(), 0.0001)
	require.True(t, toolCallLogAnyValue(values["flag"], 0).GetBoolValue())
	require.Equal(t, "value", toolCallLogAnyValue(values["text"], 0).GetStringValue())
	require.Nil(t, toolCallLogAnyValue(values["missing"], 0).GetValue())

	list := toolCallLogAnyValue(values["list"], 0).GetArrayValue().GetValues()
	require.Len(t, list, 2)
	require.Equal(t, int64(1), list[0].GetIntValue())
	require.Equal(t, "two", list[1].GetStringValue())

	nested := toolCallLogAnyValue(values["nested"], 0).GetKvlistValue().GetValues()
	require.Equal(t, "inner", requireOne(t, nested).GetKey())
	require.Equal(t, int64(3), nested[0].GetValue().GetIntValue())
}

func TestToolCallLogRelayDropsUnusableIDs(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse(testLogProjectID)
	record := toolCallLogRelayTestRecord(projectID, nil)
	record.SetTraceId("nothex")
	record.SetSpanId("0af7651916cd43dd8448eb211c80319c")
	message, eligibility := newToolCallLogRelayMessage(record, nil)
	require.Equal(t, toolCallLogEligible, eligibility)

	logRecord := toolCallLogRecord(message, toolCallLogRelayObservedAt)
	require.Nil(t, logRecord.GetTraceId())
	require.Nil(t, logRecord.GetSpanId())
}

func TestToolCallLogRelayStampsObservedTimeWhenRowHasNone(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse(testLogProjectID)
	record := toolCallLogRelayTestRecord(projectID, nil)
	record.SetObservedTimeUnixNano(0)
	message, eligibility := newToolCallLogRelayMessage(record, nil)
	require.Equal(t, toolCallLogEligible, eligibility)

	logRecord := toolCallLogRecord(message, toolCallLogRelayObservedAt)
	require.Equal(t, uint64(toolCallLogRelayObservedAt.UnixNano()), logRecord.GetObservedTimeUnixNano())
}

func newToolCallLogRelayTestHandler(t *testing.T, endpoint, sensitiveDataPolicy string) (*ToolCallLogRelayHandler, uuid.UUID) {
	t.Helper()

	db, enc, productRelay := newRelayRouteTest(t, "/v1/logs")
	projectID := createRelayTestProject(t, db, "org-test")
	destination := createRelayTestDestination(t, db, "org-test", projectID, endpoint, pgtype.Text{}, sensitiveDataPolicy)
	createRelayTestRoute(t, db, "org-test", projectID, dataexports.DataSourceToolCallLogs, true, uuid.NullUUID{UUID: destination.ID, Valid: true})
	return newToolCallLogRelayTestHandlerFor(t, db, enc, productRelay), projectID
}

func newToolCallLogRelayTestHandlerFor(t *testing.T, db *pgxpool.Pool, enc *encryption.Client, productRelay *signalRelay) *ToolCallLogRelayHandler {
	t.Helper()

	return &ToolCallLogRelayHandler{
		logger:  testenv.NewLogger(t),
		dropped: nil,
		failed:  nil,
		relay:   newSignalRelay(db, enc, productRelay.policy, dataexports.DataSourceToolCallLogs, "/v1/logs", "tool call log"),
		now:     func() time.Time { return toolCallLogRelayObservedAt },
	}
}

// toolCallLogRelayTestRecord mirrors the shape server/internal/mcp writes for
// one failed hosted MCP tool call, down to the tool IO and caller identity a
// destination must never see unless the project opted in.
func toolCallLogRelayTestRecord(projectID uuid.UUID, extraAttributes map[string]any) *telemetryv1.LogRecord {
	attributes := map[string]any{
		string(attr.EventSourceKey):               "tool_call",
		string(attr.EventURNKey):                  "urn:telemetry:gram-service:log:tool_call",
		string(attr.OrganizationIDKey):            "org-test",
		string(attr.ProjectIDKey):                 projectID.String(),
		string(attr.ToolURNKey):                   "tool:http:check_health",
		string(attr.ToolNameKey):                  "check_health",
		string(attr.ToolsetSlugKey):               "health",
		string(attr.McpURLKey):                    "https://app.getgram.ai/mcp/health",
		string(attr.HTTPResponseStatusCodeKey):    422,
		string(attr.HTTPServerRequestDurationKey): 12.5,
		string(attr.GenAIToolCallArgumentsKey):    `{"zip":"not-a-zip"}`,
		string(attr.GenAIToolCallResultKey):       `{"error":"invalid zip"}`,
		string(attr.UserEmailKey):                 "caller@example.invalid",
		string(attr.TraceIDKey):                   toolCallLogRelayTraceID,
		string(attr.SpanIDKey):                    toolCallLogRelaySpanID,
		string(attr.TimeUnixNanoKey):              toolCallLogRelayObservedAt.UnixNano(),
		string(attr.ObservedTimeUnixNanoKey):      toolCallLogRelayObservedAt.UnixNano(),
	}
	maps.Copy(attributes, extraAttributes)

	record := telemetryv1.LogRecord_builder{
		Id:                     new("0199cb4f-4840-70e6-9e1d-5558dc2d7ce1"),
		TimeUnixNano:           new(toolCallLogRelayObservedAt.UnixNano()),
		ObservedTimeUnixNano:   new(toolCallLogRelayObservedAt.Add(time.Millisecond).UnixNano()),
		SeverityText:           new("ERROR"),
		Body:                   new(""),
		TraceId:                new(toolCallLogRelayTraceID),
		SpanId:                 new(toolCallLogRelaySpanID),
		AttributesJson:         new("{}"),
		ResourceAttributesJson: new(`{"service.name":"gram-server","service.version":"v1.2.3"}`),
		GramProjectId:          new(projectID.String()),
		GramUrn:                new("tool:http:check_health"),
		ServiceName:            new("gram-server"),
		ServiceVersion:         new("v1.2.3"),
	}.Build()
	setToolCallLogRelayAttributes(record, attributes)
	return record
}

func setToolCallLogRelayAttributes(record *telemetryv1.LogRecord, attributes map[string]any) {
	encoded, err := json.Marshal(attributes)
	if err != nil {
		panic(err)
	}
	record.SetAttributesJson(string(encoded))
}

func requireToolCallLogRelayRequest(t *testing.T, requests <-chan *collectorlogsv1.ExportLogsServiceRequest) *collectorlogsv1.ExportLogsServiceRequest {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for tool call log relay request")
		return nil
	}
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	require.NoError(t, err)
	return decoded
}
