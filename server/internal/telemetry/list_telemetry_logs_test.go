package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/stretchr/testify/require"
)

func TestInsertTelemetryLog_HTTPLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	deploymentID := uuid.New().String()
	// Trace ID and Span ID must be hex strings without dashes (32 and 16 chars respectively)
	traceID := "0123456789abcdef0123456789abcdef" // 32 hex chars
	spanID := "0123456789abcdef"                  // 16 hex chars
	now := time.Now().UTC()

	severityText := "INFO"
	httpMethod := "GET"
	httpRoute := "/api/users"
	httpServerURL := "https://api.example.com"
	var httpStatusCode int32 = 200
	serviceVersion := "1.0.0"

	err := ti.chClient.InsertTelemetryLog(ctx, repo.InsertTelemetryLogParams{
		ID:                     uuid.New().String(),
		TimeUnixNano:           now.UnixNano(),
		ObservedTimeUnixNano:   now.UnixNano(),
		SeverityText:           &severityText,
		Body:                   "HTTP request completed",
		TraceID:                &traceID,
		SpanID:                 &spanID,
		Attributes:             `{"request_id":"abc123"}`,
		ResourceAttributes:     `{"service.name":"api"}`,
		GramProjectID:          projectID,
		GramDeploymentID:       &deploymentID,
		GramFunctionID:         nil,
		GramURN:                "tools:http:some_toolset:api_server",
		ServiceName:            "api-server",
		ServiceVersion:         &serviceVersion,
		HTTPRequestMethod:      &httpMethod,
		HTTPResponseStatusCode: &httpStatusCode,
		HTTPRoute:              &httpRoute,
		HTTPServerURL:          &httpServerURL,
	})
	require.NoError(t, err)

	// ClickHouse eventual consistency
	time.Sleep(100 * time.Millisecond)

	// Query the log to verify it was inserted
	logs, err := ti.chClient.ListTelemetryLogs(ctx, repo.ListTelemetryLogsParams{
		GramProjectID:          projectID,
		TimeStart:              now.Add(-1 * time.Hour).UnixNano(),
		TimeEnd:                now.Add(1 * time.Hour).UnixNano(),
		SortOrder:              "desc",
		Limit:                  10,
	})
	require.NoError(t, err)
	require.Len(t, logs, 1)

	log := logs[0]
	require.Equal(t, projectID, log.GramProjectID)
	require.Equal(t, deploymentID, *log.GramDeploymentID)
	require.Nil(t, log.GramFunctionID)
	require.Equal(t, "HTTP request completed", log.Body)
	require.Equal(t, traceID, *log.TraceID)
	require.Equal(t, spanID, *log.SpanID)
	require.Equal(t, "INFO", *log.SeverityText)
	require.Equal(t, "GET", *log.HTTPRequestMethod)
	require.Equal(t, int32(200), *log.HTTPResponseStatusCode)
	require.Equal(t, "/api/users", *log.HTTPRoute)
	require.Equal(t, "https://api.example.com", *log.HTTPServerURL)
}

func TestInsertTelemetryLog_FunctionLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	deploymentID := uuid.New().String()
	functionID := uuid.New().String()
	traceID := "abcdef0123456789abcdef0123456789" // 32 hex chars
	now := time.Now().UTC()

	severityText := "ERROR"
	serviceVersion := "2.0.0"

	err := ti.chClient.InsertTelemetryLog(ctx, repo.InsertTelemetryLogParams{
		ID:                     uuid.New().String(),
		TimeUnixNano:           now.UnixNano(),
		ObservedTimeUnixNano:   now.UnixNano(),
		SeverityText:           &severityText,
		Body:                   "Function execution failed",
		TraceID:                &traceID,
		SpanID:                 nil,
		Attributes:             `{"error":"timeout"}`,
		ResourceAttributes:     `{"service.name":"function-runner"}`,
		GramProjectID:          projectID,
		GramDeploymentID:       &deploymentID,
		GramFunctionID:         &functionID,
		GramURN:                "tools:function:some_toolset:some_tool",
		ServiceName:            "function-runner",
		ServiceVersion:         &serviceVersion,
		HTTPRequestMethod:      nil,
		HTTPResponseStatusCode: nil,
		HTTPRoute:              nil,
		HTTPServerURL:          nil,
	})
	require.NoError(t, err)

	// ClickHouse eventual consistency
	time.Sleep(100 * time.Millisecond)

	// Query the log to verify it was inserted
	logs, err := ti.chClient.ListTelemetryLogs(ctx, repo.ListTelemetryLogsParams{
		GramProjectID:          projectID,
		TimeStart:              now.Add(-1 * time.Hour).UnixNano(),
		TimeEnd:                now.Add(1 * time.Hour).UnixNano(),
		SortOrder:              "desc",
		Limit:                  10,
	})
	require.NoError(t, err)
	require.Len(t, logs, 1)

	log := logs[0]
	require.Equal(t, projectID, log.GramProjectID)
	require.Equal(t, deploymentID, *log.GramDeploymentID)
	require.Equal(t, functionID, *log.GramFunctionID)
	require.Equal(t, "Function execution failed", log.Body)
	require.Equal(t, traceID, *log.TraceID)
	require.Nil(t, log.SpanID)
	require.Equal(t, "ERROR", *log.SeverityText)
	require.Nil(t, log.HTTPRequestMethod)
	require.Nil(t, log.HTTPResponseStatusCode)
	require.Nil(t, log.HTTPRoute)
	require.Nil(t, log.HTTPServerURL)
}

func TestListTelemetryLogs_EmptyResult(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	now := time.Now().UTC()

	logs, err := ti.chClient.ListTelemetryLogs(ctx, repo.ListTelemetryLogsParams{
		GramProjectID:          projectID,
		TimeStart:              now.Add(-1 * time.Hour).UnixNano(),
		TimeEnd:                now.Add(1 * time.Hour).UnixNano(),
		SortOrder:              "desc",
		Limit:                  10,
	})
	require.NoError(t, err)
	require.Empty(t, logs)
}

func TestListTelemetryLogs_SinglePage(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	deploymentID := uuid.New().String()

	// Insert 5 logs
	insertTestTelemetryLogs(t, ctx, projectID, deploymentID, 5)

	now := time.Now().UTC()
	logs, err := ti.chClient.ListTelemetryLogs(ctx, repo.ListTelemetryLogsParams{
		GramProjectID:          projectID,
		TimeStart:              now.Add(-2 * time.Hour).UnixNano(),
		TimeEnd:                now.Add(1 * time.Hour).UnixNano(),
		SortOrder:              "desc",
		Limit:                  10,
	})
	require.NoError(t, err)
	require.Len(t, logs, 5)

	// Verify logs are sorted descending by time
	for i := 0; i < len(logs)-1; i++ {
		require.GreaterOrEqual(t, logs[i].TimeUnixNano, logs[i+1].TimeUnixNano, "logs should be sorted descending")
	}
}

func TestListTelemetryLogs_Pagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	deploymentID := uuid.New().String()

	// Insert 10 logs
	insertTestTelemetryLogs(t, ctx, projectID, deploymentID, 10)

	now := time.Now().UTC()

	// Query first page (limit 4, which returns 4 items)
	page1, err := ti.chClient.ListTelemetryLogs(ctx, repo.ListTelemetryLogsParams{
		GramProjectID:          projectID,
		TimeStart:              now.Add(-2 * time.Hour).UnixNano(),
		TimeEnd:                now.Add(1 * time.Hour).UnixNano(),
		SortOrder:              "desc",
		Limit:                  4,
	})
	require.NoError(t, err)
	require.Len(t, page1, 4)

	// Use last item's ID as cursor for page 2
	cursor := page1[3].ID

	// Query second page
	page2, err := ti.chClient.ListTelemetryLogs(ctx, repo.ListTelemetryLogsParams{
		GramProjectID:          projectID,
		TimeStart:              now.Add(-2 * time.Hour).UnixNano(),
		TimeEnd:                now.Add(1 * time.Hour).UnixNano(),
		SortOrder:              "desc",
		Cursor:                 cursor,
		Limit:                  4,
	})
	require.NoError(t, err)
	require.Len(t, page2, 4)

	// Verify no duplicates between pages
	page1IDs := make(map[string]bool)
	for _, log := range page1 {
		page1IDs[log.ID] = true
	}

	for _, log := range page2 {
		require.False(t, page1IDs[log.ID], "found duplicate log in second page: %s", log.ID)
	}
}

func TestListTelemetryLogs_FilterByTraceID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	deploymentID := uuid.New().String()
	traceID1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 32 hex chars
	traceID2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" // 32 hex chars
	now := time.Now().UTC()

	// Insert logs with different trace IDs
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-5*time.Minute), &traceID1, "urn:gram:test", "INFO")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-4*time.Minute), &traceID2, "urn:gram:test", "INFO")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-3*time.Minute), &traceID1, "urn:gram:test", "ERROR")

	logs, err := ti.chClient.ListTelemetryLogs(ctx, repo.ListTelemetryLogsParams{
		GramProjectID:          projectID,
		TimeStart:              now.Add(-1 * time.Hour).UnixNano(),
		TimeEnd:                now.Add(1 * time.Hour).UnixNano(),
		TraceID:                traceID1,
		SortOrder:              "desc",
		Limit:                  10,
	})
	require.NoError(t, err)
	require.Len(t, logs, 2)

	// Verify all logs have the correct trace ID
	for _, log := range logs {
		require.NotNil(t, log.TraceID)
		require.Equal(t, traceID1, *log.TraceID)
	}
}

func TestListTraces_MultipleTraces(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	deploymentID := uuid.New().String()
	traceID1 := "cccccccccccccccccccccccccccccccc" 
	traceID2 := "dddddddddddddddddddddddddddddddd" 
	now := time.Now().UTC()

	// Insert logs for trace 1 (3 logs: 1 INFO, 1 WARN, 1 ERROR)
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), &traceID1, "urn:gram:test1", "INFO")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), &traceID1, "urn:gram:test1", "WARN")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), &traceID1, "urn:gram:test1", "ERROR")

	// Insert logs for trace 2 (2 logs: both INFO)
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-7*time.Minute), &traceID2, "urn:gram:test2", "INFO")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-6*time.Minute), &traceID2, "urn:gram:test2", "INFO")

	// Insert log with no trace ID (should be excluded)
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-5*time.Minute), nil, "urn:gram:test3", "INFO")

	traces, err := ti.chClient.ListTraces(ctx, repo.ListTracesParams{
		GramProjectID:    projectID,
		TimeStart:        now.Add(-1 * time.Hour).UnixNano(),
		TimeEnd:          now.Add(1 * time.Hour).UnixNano(),
		SortOrder:        "desc",
		Limit:            100,
	})
	require.NoError(t, err)
	require.Len(t, traces, 2)

	// Find trace 1 and verify metrics
	var trace1, trace2 *repo.TraceSummary
	for i := range traces {
		switch traces[i].TraceID {
		case traceID1:
			trace1 = &traces[i]
		case traceID2:
			trace2 = &traces[i]
		}
	}

	require.NotNil(t, trace1)
	require.Equal(t, uint64(3), trace1.LogCount)
	require.Positive(t, trace1.StartTimeUnixNano)
	require.Equal(t, "urn:gram:test1", trace1.GramURN)

	require.NotNil(t, trace2)
	require.Equal(t, uint64(2), trace2.LogCount)
	require.Positive(t, trace2.StartTimeUnixNano)
	require.Equal(t, "urn:gram:test2", trace2.GramURN)
}

func TestListLogsForTrace_ReturnsAllLogsInOrder(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	deploymentID := uuid.New().String()
	traceID := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" // 32 hex chars
	now := time.Now().UTC()

	// Insert 5 logs for this trace with different timestamps
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), &traceID, "urn:gram:test", "INFO")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), &traceID, "urn:gram:test", "DEBUG")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), &traceID, "urn:gram:test", "WARN")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-7*time.Minute), &traceID, "urn:gram:test", "ERROR")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-6*time.Minute), &traceID, "urn:gram:test", "INFO")

	// Insert logs for a different trace (should be excluded)
	otherTraceID := "ffffffffffffffffffffffffffffffff"
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-5*time.Minute), &otherTraceID, "urn:gram:test", "INFO")

	logs, err := ti.chClient.ListLogsForTrace(ctx, repo.ListLogsForTraceParams{
		GramProjectID: projectID,
		TraceID:       traceID,
		Limit:         100,
	})
	require.NoError(t, err)
	require.Len(t, logs, 5)

	// Verify all logs have the correct trace ID
	for _, log := range logs {
		require.NotNil(t, log.TraceID)
		require.Equal(t, traceID, *log.TraceID)
	}

	// Verify logs are sorted ascending by time
	for i := 0; i < len(logs)-1; i++ {
		require.LessOrEqual(t, logs[i].TimeUnixNano, logs[i+1].TimeUnixNano, "logs should be sorted ascending by time")
	}

	// Verify severity progression matches insertion order
	require.NotNil(t, logs[0].SeverityText)
	require.Equal(t, "INFO", *logs[0].SeverityText)
	require.NotNil(t, logs[1].SeverityText)
	require.Equal(t, "DEBUG", *logs[1].SeverityText)
	require.NotNil(t, logs[2].SeverityText)
	require.Equal(t, "WARN", *logs[2].SeverityText)
	require.NotNil(t, logs[3].SeverityText)
	require.Equal(t, "ERROR", *logs[3].SeverityText)
	require.NotNil(t, logs[4].SeverityText)
	require.Equal(t, "INFO", *logs[4].SeverityText)
}

// Helper functions

func insertTestTelemetryLogs(t *testing.T, ctx context.Context, projectID, deploymentID string, count int) {
	t.Helper()

	now := time.Now().UTC().Add(-1 * time.Hour)

	for i := range count {
		timestamp := now.Add(time.Duration(i) * time.Minute)
		insertTelemetryLog(t, ctx, projectID, deploymentID, timestamp, nil, "urn:gram:test", "INFO")
	}

	// ClickHouse eventual consistency - sleep once at the end
	time.Sleep(100 * time.Millisecond)
}

func insertTelemetryLog(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, traceID *string, gramURN, severityText string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := fromTimeV7(timestamp)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), severityText, "test log body",
		traceID, nil, "{}", "{}",
		projectID, deploymentID, gramURN, "test-service")
	require.NoError(t, err)

	// ClickHouse eventual consistency
	time.Sleep(100 * time.Millisecond)
}
