package chrepo

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func testLogRow(organizationID, recordID string) OTelLogRow {
	return OTelLogRow{
		RecordID:             recordID,
		OrganizationID:       organizationID,
		ProjectID:            "11111111-2222-3333-4444-555555555555",
		TimeUnixNano:         1_724_500_000_000_000_001,
		ObservedTimeUnixNano: 1_724_500_000_000_000_002,
		Source:               "claude-code",
		TraceID:              "abababababababababababababababab",
		SpanID:               "cdcdcdcdcdcdcdcd",
		EventName:            "api_request",
		SeverityText:         "INFO",
		SeverityNumber:       9,
		Body:                 "hello world",
		LogAttributes:        `{"session_key":"session-1","input_tokens":42}`,
		Flags:                1,
		ResourceAttributes:   `{"service.name":"claude-code"}`,
		ResourceSchemaURL:    "https://opentelemetry.io/schemas/1.27.0",
		ScopeName:            "com.speakeasy.ai.logging",
		ScopeVersion:         "1.2.3",
		ScopeAttributes:      "{}",
	}
}

func testTraceRow(organizationID, recordID string) OTelTraceRow {
	return OTelTraceRow{
		RecordID:           recordID,
		OrganizationID:     organizationID,
		ProjectID:          "11111111-2222-3333-4444-555555555555",
		TimeUnixNano:       1_724_500_000_000_000_001,
		DurationNano:       500,
		Source:             "claude-code",
		TraceID:            "abababababababababababababababab",
		SpanID:             "cdcdcdcdcdcdcdcd",
		ParentSpanID:       "efefefefefefefef",
		SpanName:           "claude_code.api_request",
		SpanKind:           "client",
		StatusCode:         "error",
		StatusMessage:      "boom",
		TraceState:         "vendor=state",
		SpanAttributes:     `{"session_key":"session-1"}`,
		ResourceAttributes: `{"service.name":"claude-code"}`,
		ResourceSchemaURL:  "https://opentelemetry.io/schemas/1.27.0",
		ScopeName:          "com.speakeasy.ai.logging",
		ScopeVersion:       "1.2.3",
		ScopeAttributes:    "{}",
	}
}

func TestInsertOTelLogsRoundTrip(t *testing.T) {
	t.Parallel()

	conn := newTestClickhouse(t)
	queries := New(conn)
	organizationID := "org-" + uuid.NewString()

	require.NoError(t, queries.InsertOTelLogs(t.Context(), []OTelLogRow{testLogRow(organizationID, "record-1")}))

	// The insert waits for the async flush, so rows are readable immediately.
	rows, err := conn.Query(t.Context(), `
		SELECT
			record_id, project_id, time_unix_nano, observed_time_unix_nano,
			toUnixTimestamp64Nano(timestamp), toString(source), trace_id, span_id,
			event_name, toString(severity_text), severity_number, body,
			toJSONString(log_attributes), flags, toJSONString(resource_attributes),
			resource_schema_url, toString(scope_name), scope_version,
			toJSONString(scope_attributes)
		FROM otel_logs
		WHERE organization_id = ?
	`, organizationID)
	require.NoError(t, err)
	defer rows.Close() //nolint:errcheck // best-effort close

	require.True(t, rows.Next(), "no row returned: %v", rows.Err())
	var (
		recordID, projectID, traceID, spanID, eventName, severityText string
		body, logAttributes, resourceAttributes, resourceSchemaURL    string
		scopeName, scopeVersion, scopeAttributes, source              string
		timeUnixNano, observedTimeUnixNano, derivedTimestampNano      int64
		severityNumber                                                int32
		flags                                                         uint32
	)
	require.NoError(t, rows.Scan(
		&recordID, &projectID, &timeUnixNano, &observedTimeUnixNano,
		&derivedTimestampNano, &source, &traceID, &spanID,
		&eventName, &severityText, &severityNumber, &body,
		&logAttributes, &flags, &resourceAttributes,
		&resourceSchemaURL, &scopeName, &scopeVersion,
		&scopeAttributes,
	))
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())

	require.Equal(t, "record-1", recordID)
	require.Equal(t, "11111111-2222-3333-4444-555555555555", projectID)
	require.Equal(t, int64(1_724_500_000_000_000_001), timeUnixNano)
	require.Equal(t, int64(1_724_500_000_000_000_002), observedTimeUnixNano)
	require.Equal(t, timeUnixNano, derivedTimestampNano)
	require.Equal(t, "claude-code", source)
	require.Equal(t, "abababababababababababababababab", traceID)
	require.Equal(t, "cdcdcdcdcdcdcdcd", spanID)
	require.Equal(t, "api_request", eventName)
	require.Equal(t, "INFO", severityText)
	require.Equal(t, int32(9), severityNumber)
	require.Equal(t, "hello world", body)
	require.Equal(t, uint32(1), flags)
	require.Equal(t, "https://opentelemetry.io/schemas/1.27.0", resourceSchemaURL)
	require.Equal(t, "com.speakeasy.ai.logging", scopeName)
	require.Equal(t, "1.2.3", scopeVersion)

	var parsedLogAttributes map[string]any
	require.NoError(t, json.Unmarshal([]byte(logAttributes), &parsedLogAttributes))
	require.Equal(t, "session-1", parsedLogAttributes["session_key"])

	var parsedResourceAttributes map[string]any
	require.NoError(t, json.Unmarshal([]byte(resourceAttributes), &parsedResourceAttributes))
	require.Contains(t, resourceAttributes, "claude-code")
}

func TestInsertOTelLogsDedupsRedeliveryOnRecordID(t *testing.T) {
	t.Parallel()

	conn := newTestClickhouse(t)
	queries := New(conn)
	organizationID := "org-" + uuid.NewString()

	// A redelivered Pub/Sub batch re-inserts identical rows: same record_id
	// and time, so they share a ReplacingMergeTree sort key.
	row := testLogRow(organizationID, "record-1")
	require.NoError(t, queries.InsertOTelLogs(t.Context(), []OTelLogRow{row}))
	require.NoError(t, queries.InsertOTelLogs(t.Context(), []OTelLogRow{row}))

	rows, err := conn.Query(t.Context(), "SELECT count() FROM otel_logs FINAL WHERE organization_id = ?", organizationID)
	require.NoError(t, err)
	defer rows.Close() //nolint:errcheck // best-effort close

	require.True(t, rows.Next())
	var count uint64
	require.NoError(t, rows.Scan(&count))
	require.Equal(t, uint64(1), count)
}

func TestInsertOTelTracesRoundTrip(t *testing.T) {
	t.Parallel()

	conn := newTestClickhouse(t)
	queries := New(conn)
	organizationID := "org-" + uuid.NewString()

	recordID := "abababababababababababababababab:cdcdcdcdcdcdcdcd"
	require.NoError(t, queries.InsertOTelTraces(t.Context(), []OTelTraceRow{testTraceRow(organizationID, recordID)}))

	rows, err := conn.Query(t.Context(), `
		SELECT
			record_id, project_id, time_unix_nano, duration_nano,
			toUnixTimestamp64Nano(timestamp), toString(source), trace_id, span_id,
			parent_span_id, span_name, toString(span_kind), toString(status_code),
			status_message, trace_state, toJSONString(span_attributes),
			toJSONString(resource_attributes), resource_schema_url,
			toString(scope_name), scope_version, toJSONString(scope_attributes)
		FROM otel_traces
		WHERE organization_id = ?
	`, organizationID)
	require.NoError(t, err)
	defer rows.Close() //nolint:errcheck // best-effort close

	require.True(t, rows.Next(), "no row returned: %v", rows.Err())
	var (
		gotRecordID, projectID, source, traceID, spanID, parentSpanID string
		spanName, spanKind, statusCode, statusMessage, traceState     string
		spanAttributes, resourceAttributes, resourceSchemaURL         string
		scopeName, scopeVersion, scopeAttributes                      string
		timeUnixNano, durationNano, derivedTimestampNano              int64
	)
	require.NoError(t, rows.Scan(
		&gotRecordID, &projectID, &timeUnixNano, &durationNano,
		&derivedTimestampNano, &source, &traceID, &spanID,
		&parentSpanID, &spanName, &spanKind, &statusCode,
		&statusMessage, &traceState, &spanAttributes,
		&resourceAttributes, &resourceSchemaURL,
		&scopeName, &scopeVersion, &scopeAttributes,
	))
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())

	require.Equal(t, recordID, gotRecordID)
	require.Equal(t, "11111111-2222-3333-4444-555555555555", projectID)
	require.Equal(t, int64(1_724_500_000_000_000_001), timeUnixNano)
	require.Equal(t, int64(500), durationNano)
	require.Equal(t, timeUnixNano, derivedTimestampNano)
	require.Equal(t, "claude-code", source)
	require.Equal(t, "abababababababababababababababab", traceID)
	require.Equal(t, "cdcdcdcdcdcdcdcd", spanID)
	require.Equal(t, "efefefefefefefef", parentSpanID)
	require.Equal(t, "claude_code.api_request", spanName)
	require.Equal(t, "client", spanKind)
	require.Equal(t, "error", statusCode)
	require.Equal(t, "boom", statusMessage)
	require.Equal(t, "vendor=state", traceState)
	require.Equal(t, "https://opentelemetry.io/schemas/1.27.0", resourceSchemaURL)
	require.Equal(t, "com.speakeasy.ai.logging", scopeName)
	require.Equal(t, "1.2.3", scopeVersion)

	var parsedSpanAttributes map[string]any
	require.NoError(t, json.Unmarshal([]byte(spanAttributes), &parsedSpanAttributes))
	require.Equal(t, "session-1", parsedSpanAttributes["session_key"])
}

func TestInsertOTelTracesDedupsRedeliveryOnRecordID(t *testing.T) {
	t.Parallel()

	conn := newTestClickhouse(t)
	queries := New(conn)
	organizationID := "org-" + uuid.NewString()

	row := testTraceRow(organizationID, "abababababababababababababababab:cdcdcdcdcdcdcdcd")
	require.NoError(t, queries.InsertOTelTraces(t.Context(), []OTelTraceRow{row}))
	require.NoError(t, queries.InsertOTelTraces(t.Context(), []OTelTraceRow{row}))

	rows, err := conn.Query(t.Context(), "SELECT count() FROM otel_traces FINAL WHERE organization_id = ?", organizationID)
	require.NoError(t, err)
	defer rows.Close() //nolint:errcheck // best-effort close

	require.True(t, rows.Next())
	var count uint64
	require.NoError(t, rows.Scan(&count))
	require.Equal(t, uint64(1), count)
}

func TestInsertOTelLogsEmptyBatchIsNoop(t *testing.T) {
	t.Parallel()

	queries := New(newTestClickhouse(t))
	require.NoError(t, queries.InsertOTelLogs(t.Context(), nil))
}

func TestInsertOTelTracesEmptyBatchIsNoop(t *testing.T) {
	t.Parallel()

	queries := New(newTestClickhouse(t))
	require.NoError(t, queries.InsertOTelTraces(t.Context(), nil))
}
