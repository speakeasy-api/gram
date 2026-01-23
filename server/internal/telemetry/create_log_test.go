package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/stretchr/testify/require"
)

func TestCreateLog_LogsCorrectly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)

	attrs := telemetry.HTTPLogAttributes{}
	attrs.RecordMethod("POST")
	attrs.RecordRoute("/api/test")
	attrs.RecordStatusCode(200)
	attrs.RecordServerURL("https://example.com", repo.ToolTypeHTTP)
	attrs.RecordDuration(123.45)
	attrs.RecordUserAgent("test-client/1.0")

	toolInfo := newTestToolInfo()
	timestamp := time.Now().UTC()

	ti.service.CreateLog(ctx, telemetry.LogParams{
		Timestamp:  timestamp,
		ToolInfo:   toolInfo,
		Attributes: attrs,
	})

	log := waitForLog(
		t, ctx, ti.chClient, toolInfo.ProjectID, toolInfo.URN, timestamp)

	// logs HTTP data
	require.Equal(t, "POST", *log.HTTPRequestMethod)
	require.Equal(t, "/api/test", *log.HTTPRoute)
	require.Equal(t, int32(200), *log.HTTPResponseStatusCode)
	require.Equal(t, "https://example.com", *log.HTTPServerURL)

	/// logs tool info
	require.Equal(t, toolInfo.ProjectID, log.GramProjectID)
	require.NotNil(t, log.GramDeploymentID)
	require.Equal(t, toolInfo.DeploymentID, *log.GramDeploymentID)
	require.Equal(t, toolInfo.URN, log.GramURN)
	require.Equal(t, "gram-server", log.ServiceName)

	// logs to attributes col
	require.Contains(t, log.Attributes, toolInfo.URN)
	require.Contains(t, log.Attributes, toolInfo.Name)
	require.Contains(t, log.Attributes, toolInfo.OrganizationID)
}

func TestCreateLog_NilFunctionID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)

	attrs := telemetry.HTTPLogAttributes{}
	attrs.RecordMethod("GET")
	attrs.RecordStatusCode(200)

	toolInfo := newTestToolInfo()
	toolInfo.FunctionID = nil
	timestamp := time.Now().UTC()

	ti.service.CreateLog(ctx, telemetry.LogParams{
		Timestamp:  timestamp,
		ToolInfo:   toolInfo,
		Attributes: attrs,
	})

	log := waitForLog(t, ctx, ti.chClient, toolInfo.ProjectID, toolInfo.URN, timestamp)

	require.Nil(t, log.GramFunctionID)
}

func TestCreateLog_NonNilFunctionID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)

	attrs := telemetry.HTTPLogAttributes{}
	attrs.RecordMethod("GET")
	attrs.RecordStatusCode(200)

	funcID := uuid.New().String()
	toolInfo := newTestToolInfo()
	toolInfo.FunctionID = &funcID
	timestamp := time.Now().UTC()

	ti.service.CreateLog(ctx, telemetry.LogParams{
		Timestamp:  timestamp,
		ToolInfo:   toolInfo,
		Attributes: attrs,
	})

	log := waitForLog(t, ctx, ti.chClient, toolInfo.ProjectID, toolInfo.URN, timestamp)

	require.NotNil(t, log.GramFunctionID)
	require.Equal(t, funcID, *log.GramFunctionID)
}

func TestCreateLog_SeverityFromStatusCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		statusCode     int
		expectSeverity string
	}{
		{"2xx returns INFO", 200, "INFO"},
		{"4xx returns WARN", 404, "WARN"},
		{"5xx returns ERROR", 500, "ERROR"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestLogsService(t)

			attrs := telemetry.HTTPLogAttributes{}
			attrs.RecordMethod("GET")
			attrs.RecordStatusCode(tc.statusCode)

			toolInfo := newTestToolInfo()
			timestamp := time.Now().UTC()

			ti.service.CreateLog(ctx, telemetry.LogParams{
				Timestamp:  timestamp,
				ToolInfo:   toolInfo,
				Attributes: attrs,
			})

			log := waitForLog(t, ctx, ti.chClient, toolInfo.ProjectID, toolInfo.URN, timestamp)

			require.NotNil(t, log.SeverityText)
			require.Equal(t, tc.expectSeverity, *log.SeverityText)
		})
	}
}

func TestCreateLog_DefaultSeverityWithoutStatusCode(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)

	attrs := telemetry.HTTPLogAttributes{}
	attrs.RecordMethod("GET")
	// No status code recorded

	toolInfo := newTestToolInfo()
	timestamp := time.Now().UTC()

	ti.service.CreateLog(ctx, telemetry.LogParams{
		Timestamp:  timestamp,
		ToolInfo:   toolInfo,
		Attributes: attrs,
	})

	log := waitForLog(t, ctx, ti.chClient, toolInfo.ProjectID, toolInfo.URN, timestamp)

	require.NotNil(t, log.SeverityText)
	require.Equal(t, "INFO", *log.SeverityText)
}

func TestCreateLog_RequestHeaders(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)

	attrs := telemetry.HTTPLogAttributes{}
	attrs.RecordMethod("POST")
	attrs.RecordStatusCode(200)
	attrs.RecordRequestHeaders(map[string]string{
		"Content-Type": "application/json",
		"X-Request-ID": "req-123",
	}, false)

	toolInfo := newTestToolInfo()
	timestamp := time.Now().UTC()

	ti.service.CreateLog(ctx, telemetry.LogParams{
		Timestamp:  timestamp,
		ToolInfo:   toolInfo,
		Attributes: attrs,
	})

	log := waitForLog(t, ctx, ti.chClient, toolInfo.ProjectID, toolInfo.URN, timestamp)

	require.Contains(t, log.Attributes, "Content-Type")
	require.Contains(t, log.Attributes, "application\\/json") // JSON escapes forward slashes
	require.Contains(t, log.Attributes, "X-Request-ID")
	require.Contains(t, log.Attributes, "req-123")
}

func TestCreateLog_ResponseHeaders(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)

	attrs := telemetry.HTTPLogAttributes{}
	attrs.RecordMethod("GET")
	attrs.RecordStatusCode(200)
	attrs.RecordResponseHeaders(map[string]string{
		"Content-Type":   "application/json",
		"Content-Length": "1234",
	})

	toolInfo := newTestToolInfo()
	timestamp := time.Now().UTC()

	ti.service.CreateLog(ctx, telemetry.LogParams{
		Timestamp:  timestamp,
		ToolInfo:   toolInfo,
		Attributes: attrs,
	})

	log := waitForLog(t, ctx, ti.chClient, toolInfo.ProjectID, toolInfo.URN, timestamp)

	require.Contains(t, log.Attributes, "Content-Type")
	require.Contains(t, log.Attributes, "Content-Length")
	require.Contains(t, log.Attributes, "1234")
}

func TestCreateLog_LogMessageBody(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)

	attrs := telemetry.HTTPLogAttributes{}
	attrs.RecordMethod("POST")
	attrs.RecordStatusCode(200)
	attrs.RecordMessageBody("POST /api/test -> 200 (0.12s)")

	toolInfo := newTestToolInfo()
	timestamp := time.Now().UTC()

	ti.service.CreateLog(ctx, telemetry.LogParams{
		Timestamp:  timestamp,
		ToolInfo:   toolInfo,
		Attributes: attrs,
	})

	log := waitForLog(t, ctx, ti.chClient, toolInfo.ProjectID, toolInfo.URN, timestamp)

	require.Equal(t, "POST /api/test -> 200 (0.12s)", log.Body)
}

func TestCreateLog_Timestamp(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)

	attrs := telemetry.HTTPLogAttributes{}
	attrs.RecordMethod("GET")
	attrs.RecordStatusCode(200)

	toolInfo := newTestToolInfo()
	// Use a timestamp within the ClickHouse TTL window (30 days)
	// but still specific enough to verify timestamp storage
	timestamp := time.Now().UTC().Truncate(time.Second).Add(-24 * time.Hour)

	ti.service.CreateLog(ctx, telemetry.LogParams{
		Timestamp:  timestamp,
		ToolInfo:   toolInfo,
		Attributes: attrs,
	})

	log := waitForLog(t, ctx, ti.chClient, toolInfo.ProjectID, toolInfo.URN, timestamp)

	require.Equal(t, timestamp.UnixNano(), log.TimeUnixNano)
}

func newTestToolInfo() telemetry.ToolInfo {
	return telemetry.ToolInfo{
		ID:             uuid.New().String(),
		URN:            "tools:http:test-source:test-tool-" + uuid.New().String(),
		Name:           "test-tool",
		ProjectID:      uuid.New().String(),
		DeploymentID:   uuid.New().String(),
		OrganizationID: uuid.New().String(),
	}
}

func waitForLog(t *testing.T, ctx context.Context, client *repo.Queries, projectID, urn string, timestamp time.Time) repo.TelemetryLog {
	t.Helper()

	var logs []repo.TelemetryLog
	require.Eventually(t, func() bool {
		var err error
		logs, err = client.ListTelemetryLogs(ctx, repo.ListTelemetryLogsParams{
			GramProjectID: projectID,
			TimeStart:     timestamp.Add(-1 * time.Minute).UnixNano(),
			TimeEnd:       timestamp.Add(1 * time.Minute).UnixNano(),
			GramURNs:      []string{urn},
			SortOrder:     "desc",
			Cursor:        "",
			Limit:         10,
		})
		return err == nil && len(logs) == 1
	}, 2*time.Second, 50*time.Millisecond, "expected 1 log in ClickHouse")

	return logs[0]
}
