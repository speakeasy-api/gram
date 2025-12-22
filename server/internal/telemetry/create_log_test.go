package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestToolCallLogger_EmitCreatesHTTPAndTelemetryLogs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := testenv.NewLogger(t)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	tracerProvider := testenv.NewTracerProvider(t)

	chClient := repo.New(logger, tracerProvider, chConn, func(context.Context, string) (bool, error) {
		return true, nil
	})

	// Create test data
	projectID := uuid.New().String()
	deploymentID := uuid.New().String()
	toolID := uuid.New().String()
	organizationID := uuid.New().String()
	toolURN := "tools:http:test-source:test-tool"
	toolName := "test-tool"

	// Create a tool call logger
	toolCallLogger, err := telemetry.NewToolCallLogger(
		ctx,
		chClient,
		organizationID,
		telemetry.ToolInfo{
			ID:             toolID,
			Urn:            toolURN,
			Name:           toolName,
			ProjectID:      projectID,
			DeploymentID:   deploymentID,
			OrganizationID: organizationID,
		},
		toolName,
		repo.ToolTypeHTTP,
	)
	require.NoError(t, err)
	require.True(t, toolCallLogger.Enabled())

	// Record HTTP request details
	toolCallLogger.RecordHTTPMethod("POST")
	toolCallLogger.RecordHTTPRoute("/api/test")
	toolCallLogger.RecordHTTPServerURL("https://example.com")
	toolCallLogger.RecordStatusCode(200)
	toolCallLogger.RecordDurationMs(123.45)
	toolCallLogger.RecordUserAgent("test-client/1.0")
	toolCallLogger.RecordRequestHeaders(map[string]string{"Authorization": "Bearer token"}, true)
	toolCallLogger.RecordResponseHeaders(map[string]string{"Content-Type": "application/json"})
	toolCallLogger.RecordRequestBodyBytes(100)
	toolCallLogger.RecordResponseBodyBytes(150)

	now := time.Now().UTC()

	// Emit the logs (writes to both http_requests_raw and telemetry_logs)
	toolCallLogger.Emit(ctx, logger)

	// Wait for async writes to complete (ClickHouse eventual consistency)
	time.Sleep(100 * time.Millisecond)

	// Query the database to verify the telemetry log was inserted
	logs, err := chClient.ListTelemetryLogs(ctx, repo.ListTelemetryLogsParams{
		GramProjectID: projectID,
		TimeStart:     now.Add(-1 * time.Minute).UnixNano(),
		TimeEnd:       now.Add(1 * time.Minute).UnixNano(),
		GramURN:       toolURN,
		SortOrder:     "desc",
		Cursor:        "",
		Limit:         10,
	})
	require.NoError(t, err)
	require.Len(t, logs, 1, "Expected 1 log in telemetry_logs table")

	// Verify the inserted log
	log := logs[0]
	require.Equal(t, projectID, log.GramProjectID)
	require.Equal(t, deploymentID, *log.GramDeploymentID)
	require.Nil(t, log.GramFunctionID)
	require.Equal(t, toolURN, log.GramURN)
	require.Equal(t, "gram-server", log.ServiceName)
	require.Equal(t, "INFO", *log.SeverityText)
	require.Equal(t, "POST", *log.HTTPRequestMethod)
	require.Equal(t, int32(200), *log.HTTPResponseStatusCode)
	require.Equal(t, "/api/test", *log.HTTPRoute)
	require.Equal(t, "https://example.com", *log.HTTPServerURL)
	require.Contains(t, log.Body, "POST /api/test -> 200")
	require.Contains(t, log.Body, "123.45")

	// Verify headers are included in attributes
	require.Contains(t, log.Attributes, "headers")
	require.Contains(t, log.Attributes, "Authorization")
	require.Contains(t, log.Attributes, "Bearer") // Redacted token
	require.Contains(t, log.Attributes, "Content-Type")
	require.Contains(t, log.Attributes, "application\\/json") // JSON escapes forward slashes
}

func TestToolCallLogger_404ErrorLogsWithWarnSeverity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := testenv.NewLogger(t)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	tracerProvider := testenv.NewTracerProvider(t)

	chClient := repo.New(logger, tracerProvider, chConn, func(context.Context, string) (bool, error) {
		return true, nil
	})

	// Create test data
	toolID := uuid.New().String()
	projectID := uuid.New().String()
	deploymentID := uuid.New().String()
	organizationID := uuid.New().String()
	toolURN := "tools:http:test:warn-severity"
	toolName := "test-tool"

	// Create a tool call logger
	toolCallLogger, err := telemetry.NewToolCallLogger(
		ctx,
		chClient,
		organizationID,
		telemetry.ToolInfo{
			ID:             toolID,
			Urn:            toolURN,
			Name:           toolName,
			ProjectID:      projectID,
			DeploymentID:   deploymentID,
			OrganizationID: organizationID,
		},
		toolName,
		repo.ToolTypeHTTP,
	)
	require.NoError(t, err)

	// Record 404 error
	toolCallLogger.RecordHTTPMethod("GET")
	toolCallLogger.RecordHTTPRoute("/users")
	toolCallLogger.RecordHTTPServerURL("https://api.example.com")
	toolCallLogger.RecordStatusCode(404)
	toolCallLogger.RecordDurationMs(50.25)
	toolCallLogger.RecordUserAgent("test-client/1.0")
	toolCallLogger.RecordRequestHeaders(map[string]string{"Authorization": "Bearer token"}, true)
	toolCallLogger.RecordResponseHeaders(map[string]string{"Content-Type": "application/json"})
	toolCallLogger.RecordResponseBodyBytes(150)

	now := time.Now().UTC()

	// Emit the logs
	toolCallLogger.Emit(ctx, logger)

	// Wait for async write
	time.Sleep(100 * time.Millisecond)

	// Verify the log was inserted with correct conversion
	logs, err := chClient.ListTelemetryLogs(ctx, repo.ListTelemetryLogsParams{
		GramProjectID: projectID,
		TimeStart:     now.Add(-1 * time.Minute).UnixNano(),
		TimeEnd:       now.Add(1 * time.Minute).UnixNano(),
		GramURN:       toolURN,
		SortOrder:     "desc",
		Cursor:        "",
		Limit:         10,
	})
	require.NoError(t, err)
	require.Len(t, logs, 1)

	log := logs[0]
	// Verify 404 was converted to WARN severity
	require.NotNil(t, log.SeverityText)
	require.Equal(t, "WARN", *log.SeverityText)
	require.Equal(t, "GET", *log.HTTPRequestMethod)
	require.Equal(t, int32(404), *log.HTTPResponseStatusCode)
	require.Contains(t, log.Body, "404")
	require.Contains(t, log.Body, "50.25")

	// Verify headers are included in attributes
	require.Contains(t, log.Attributes, "headers")
	require.Contains(t, log.Attributes, "Authorization") // Request header key
	require.Contains(t, log.Attributes, "Bearer") // Redacted request header value
	require.Contains(t, log.Attributes, "Content-Type") // Response header key
	require.Contains(t, log.Attributes, "application\\/json") // Response header value (JSON escapes slashes)
}
