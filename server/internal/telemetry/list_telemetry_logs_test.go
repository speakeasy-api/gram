package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/logs"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/require"
)

// ListTelemetryLogs tests

func TestService_ListTelemetryLogs_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	timeStart := now.Add(-1 * time.Hour).UnixNano()
	timeEnd := now.Add(1 * time.Hour).UnixNano()

	result, err := ti.service.ListTelemetryLogs(ctx, &gen.ListTelemetryLogsPayload{
		TimeStart: &timeStart,
		TimeEnd:   &timeEnd,
		Limit:     50,
		Sort:      "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Logs)
	require.Nil(t, result.NextCursor)
}

func TestService_ListTelemetryLogs_SortDescending(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	// Insert 5 logs
	insertTestTelemetryLogs(t, ctx, projectID, deploymentID, 5)

	now := time.Now().UTC()
	timeStart := now.Add(-2 * time.Hour).UnixNano()
	timeEnd := now.Add(1 * time.Hour).UnixNano()

	result, err := ti.service.ListTelemetryLogs(ctx, &gen.ListTelemetryLogsPayload{
		TimeStart: &timeStart,
		TimeEnd:   &timeEnd,
		Limit:     10,
		Sort:      "", // Empty should default to desc
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Logs, 5)
	require.Nil(t, result.NextCursor)

	// Verify descending order
	for i := 0; i < len(result.Logs)-1; i++ {
		require.GreaterOrEqual(t, result.Logs[i].TimeUnixNano, result.Logs[i+1].TimeUnixNano,
			"logs should be sorted descending by time")
	}
}

func TestService_ListTelemetryLogs_Pagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	// Insert 10 logs
	insertTestTelemetryLogs(t, ctx, projectID, deploymentID, 10)

	now := time.Now().UTC()
	timeStart := now.Add(-2 * time.Hour).UnixNano()
	timeEnd := now.Add(1 * time.Hour).UnixNano()

	// Get first page (limit 4)
	page1, err := ti.service.ListTelemetryLogs(ctx, &gen.ListTelemetryLogsPayload{
		TimeStart: &timeStart,
		TimeEnd:   &timeEnd,
		Limit:     4,
		Sort:      "desc",
	})
	require.NoError(t, err)
	require.Len(t, page1.Logs, 4)
	require.NotNil(t, page1.NextCursor, "should have next cursor when more results exist")

	// Get second page using cursor
	page2, err := ti.service.ListTelemetryLogs(ctx, &gen.ListTelemetryLogsPayload{
		TimeStart: &timeStart,
		TimeEnd:   &timeEnd,
		Cursor:    page1.NextCursor,
		Limit:     4,
		Sort:      "desc",
	})
	require.NoError(t, err)
	require.Len(t, page2.Logs, 4)
	require.NotNil(t, page2.NextCursor, "should have next cursor for third page")

	// Get third page (remaining logs)
	page3, err := ti.service.ListTelemetryLogs(ctx, &gen.ListTelemetryLogsPayload{
		TimeStart: &timeStart,
		TimeEnd:   &timeEnd,
		Cursor:    page2.NextCursor,
		Limit:     4,
		Sort:      "desc",
	})
	require.NoError(t, err)
	require.Len(t, page3.Logs, 2)
	require.Nil(t, page3.NextCursor, "should not have next cursor on last page")

	// Verify all logs are in descending order across pages
	allLogs := append(append(page1.Logs, page2.Logs...), page3.Logs...)
	for i := 0; i < len(allLogs)-1; i++ {
		require.Greater(t, allLogs[i].TimeUnixNano, allLogs[i+1].TimeUnixNano,
			"logs should be sorted descending across pages")
	}
}

func TestService_ListTelemetryLogs_FilterByTraceID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	traceID1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	traceID2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-5*time.Minute), &traceID1, "urn:gram:test", "INFO")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-4*time.Minute), &traceID2, "urn:gram:test", "INFO")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-3*time.Minute), &traceID1, "urn:gram:test", "ERROR")

	timeStart := now.Add(-1 * time.Hour).UnixNano()
	timeEnd := now.Add(1 * time.Hour).UnixNano()

	result, err := ti.service.ListTelemetryLogs(ctx, &gen.ListTelemetryLogsPayload{
		TimeStart: &timeStart,
		TimeEnd:   &timeEnd,
		TraceID:   &traceID1,
		Limit:     10,
		Sort:      "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Logs, 2)

	// Verify all logs have the correct trace ID
	for _, log := range result.Logs {
		require.NotNil(t, log.TraceID)
		require.Equal(t, traceID1, *log.TraceID)
	}
}

func TestService_ListTelemetryLogs_AttributesAreJSON(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	insertTelemetryLog(t, ctx, projectID, deploymentID, now, nil, "urn:gram:test", "INFO")

	timeStart := now.Add(-1 * time.Hour).UnixNano()
	timeEnd := now.Add(1 * time.Hour).UnixNano()

	result, err := ti.service.ListTelemetryLogs(ctx, &gen.ListTelemetryLogsPayload{
		TimeStart: &timeStart,
		TimeEnd:   &timeEnd,
		Limit:     10,
		Sort:      "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Logs, 1)

	log := result.Logs[0]
	require.NotNil(t, log.Attributes)
	require.NotNil(t, log.ResourceAttributes)

	// Attributes should be parsed as map, not string
	_, ok := log.Attributes.(map[string]any)
	require.True(t, ok, "attributes should be a map[string]any, not a string")

	_, ok = log.ResourceAttributes.(map[string]any)
	require.True(t, ok, "resource_attributes should be a map[string]any, not a string")
}

// ListTraces tests

func TestService_ListTraces_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	timeStart := now.Add(-1 * time.Hour).UnixNano()
	timeEnd := now.Add(1 * time.Hour).UnixNano()

	result, err := ti.service.ListTraces(ctx, &gen.ListTracesPayload{
		TimeStart: &timeStart,
		TimeEnd:   &timeEnd,
		Limit:     50,
		Sort:      "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Traces)
	require.Nil(t, result.NextCursor)
}

func TestService_ListTraces_AggregatesByTraceID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	traceID1 := "cccccccccccccccccccccccccccccccc"
	traceID2 := "dddddddddddddddddddddddddddddddd"

	// Insert 3 logs for trace 1
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), &traceID1, "urn:gram:test1", "INFO")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), &traceID1, "urn:gram:test1", "WARN")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), &traceID1, "urn:gram:test1", "ERROR")

	// Insert 2 logs for trace 2
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-7*time.Minute), &traceID2, "urn:gram:test2", "INFO")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-6*time.Minute), &traceID2, "urn:gram:test2", "INFO")

	// Insert log with no trace ID (should be excluded)
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-5*time.Minute), nil, "urn:gram:test3", "INFO")

	timeStart := now.Add(-1 * time.Hour).UnixNano()
	timeEnd := now.Add(1 * time.Hour).UnixNano()

	result, err := ti.service.ListTraces(ctx, &gen.ListTracesPayload{
		TimeStart: &timeStart,
		TimeEnd:   &timeEnd,
		Limit:     100,
		Sort:      "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Traces, 2)

	// Find both traces
	var trace1, trace2 *gen.TraceSummaryRecord
	for i := range result.Traces {
		switch result.Traces[i].TraceID {
		case traceID1:
			trace1 = result.Traces[i]
		case traceID2:
			trace2 = result.Traces[i]
		}
	}

	require.NotNil(t, trace1)
	require.Equal(t, uint64(3), trace1.LogCount)
	require.Positive(t, trace1.StartTimeUnixNano)
	require.Equal(t, "urn:gram:test1", trace1.GramUrn)

	require.NotNil(t, trace2)
	require.Equal(t, uint64(2), trace2.LogCount)
	require.Positive(t, trace2.StartTimeUnixNano)
	require.Equal(t, "urn:gram:test2", trace2.GramUrn)
}

// ListLogsForTrace tests

func TestService_ListLogsForTrace_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	traceID := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

	result, err := ti.service.ListLogsForTrace(ctx, &gen.ListLogsForTracePayload{
		TraceID: traceID,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Logs)
}

func TestService_ListLogsForTrace_SortedAscending(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	traceID := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

	now := time.Now().UTC()

	// Insert 5 logs for this trace
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), &traceID, "urn:gram:test", "INFO")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), &traceID, "urn:gram:test", "DEBUG")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), &traceID, "urn:gram:test", "WARN")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-7*time.Minute), &traceID, "urn:gram:test", "ERROR")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-6*time.Minute), &traceID, "urn:gram:test", "INFO")

	// Insert logs for a different trace (should be excluded)
	otherTraceID := "ffffffffffffffffffffffffffffffff"
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-5*time.Minute), &otherTraceID, "urn:gram:test", "INFO")

	result, err := ti.service.ListLogsForTrace(ctx, &gen.ListLogsForTracePayload{
		TraceID: traceID,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Logs, 5)

	// Verify all logs have the correct trace ID
	for _, log := range result.Logs {
		require.NotNil(t, log.TraceID)
		require.Equal(t, traceID, *log.TraceID)
	}

	// Verify logs are sorted ascending by time
	for i := 0; i < len(result.Logs)-1; i++ {
		require.LessOrEqual(t, result.Logs[i].TimeUnixNano, result.Logs[i+1].TimeUnixNano,
			"logs should be sorted ascending by time")
	}

	// Verify severity progression matches insertion order
	severities := []string{"INFO", "DEBUG", "WARN", "ERROR", "INFO"}
	for i, log := range result.Logs {
		require.NotNil(t, log.SeverityText)
		require.Equal(t, severities[i], *log.SeverityText)
	}
}

func TestService_ListLogsForTrace_ReturnsAllLogs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	traceID := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

	now := time.Now().UTC()

	// Insert 10 logs for this trace
	for i := 0; i < 10; i++ {
		timestamp := now.Add(time.Duration(-10+i) * time.Minute)
		insertTelemetryLog(t, ctx, projectID, deploymentID, timestamp, &traceID, "urn:gram:test", "INFO")
	}

	result, err := ti.service.ListLogsForTrace(ctx, &gen.ListLogsForTracePayload{
		TraceID: traceID,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Logs, 10, "should return all logs for the trace")
}

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
