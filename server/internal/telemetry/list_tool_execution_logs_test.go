package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/logs"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/stretchr/testify/require"
)

func TestListToolExecutionLogs_EmptyResult(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	result, err := ti.service.ListToolExecutionLogs(ctx, &gen.ListToolExecutionLogsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		TsStart:          nil,
		TsEnd:            nil,
		DeploymentID:     nil,
		FunctionID:       nil,
		Instance:         nil,
		Level:            nil,
		Source:           nil,
		Cursor:           nil,
		PerPage:          20,
		Direction:        "next",
		Sort:             "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Logs)
	require.NotNil(t, result.Pagination)
	require.Equal(t, 20, *result.Pagination.PerPage)
	require.False(t, *result.Pagination.HasNextPage)
	require.Nil(t, result.Pagination.NextPageCursor)
}

func TestListToolExecutionLogs_SinglePage(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	ctx = setProjectID(t, ctx, projectID)

	deploymentID := uuid.New().String()
	functionID := uuid.New().String()

	// Insert 5 test logs
	insertTestToolExecutionLogs(t, ctx, projectID, deploymentID, functionID, 5)

	// Query logs
	result, err := ti.service.ListToolExecutionLogs(ctx, &gen.ListToolExecutionLogsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		TsStart:          nil,
		TsEnd:            nil,
		DeploymentID:     nil,
		FunctionID:       nil,
		Instance:         nil,
		Level:            nil,
		Source:           nil,
		Cursor:           nil,
		PerPage:          10,
		Direction:        "next",
		Sort:             "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Logs, 5)
	require.NotNil(t, result.Pagination)
	require.Equal(t, 10, *result.Pagination.PerPage)
	require.False(t, *result.Pagination.HasNextPage)
	require.Nil(t, result.Pagination.NextPageCursor)

	// Verify logs are sorted descending by timestamp
	for i := 0; i < len(result.Logs)-1; i++ {
		ts1, err1 := time.Parse(time.RFC3339, result.Logs[i].Timestamp)
		ts2, err2 := time.Parse(time.RFC3339, result.Logs[i+1].Timestamp)
		require.NoError(t, err1)
		require.NoError(t, err2)
		require.True(t, ts1.After(ts2) || ts1.Equal(ts2), "logs should be sorted descending")
	}
}

func TestListToolExecutionLogs_Pagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	ctx = setProjectID(t, ctx, projectID)

	deploymentID := uuid.New().String()
	functionID := uuid.New().String()

	// Insert 10 test logs
	insertTestToolExecutionLogs(t, ctx, projectID, deploymentID, functionID, 10)

	// Query first page
	result, err := ti.service.ListToolExecutionLogs(ctx, &gen.ListToolExecutionLogsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		TsStart:          nil,
		TsEnd:            nil,
		DeploymentID:     nil,
		FunctionID:       nil,
		Instance:         nil,
		Level:            nil,
		Source:           nil,
		Cursor:           nil,
		PerPage:          3,
		Direction:        "next",
		Sort:             "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Logs, 3)
	require.NotNil(t, result.Pagination)
	require.Equal(t, 3, *result.Pagination.PerPage)
	require.True(t, *result.Pagination.HasNextPage)
	require.NotNil(t, result.Pagination.NextPageCursor)

	// Query second page using cursor
	cursor := result.Pagination.NextPageCursor
	result2, err := ti.service.ListToolExecutionLogs(ctx, &gen.ListToolExecutionLogsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		TsStart:          nil,
		TsEnd:            nil,
		DeploymentID:     nil,
		FunctionID:       nil,
		Instance:         nil,
		Level:            nil,
		Source:           nil,
		Cursor:           cursor,
		PerPage:          3,
		Direction:        "next",
		Sort:             "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result2)
	require.Len(t, result2.Logs, 3)
	require.True(t, *result2.Pagination.HasNextPage)

	// Verify no duplicate logs across pages
	firstPageIDs := make(map[string]bool)
	for i, log := range result.Logs {
		t.Logf("Page 1 log %d: ID=%s, Timestamp=%s", i, log.ID, log.Timestamp)
		firstPageIDs[log.ID] = true
	}

	t.Logf("Cursor for page 2: %s", *cursor)

	for i, log := range result2.Logs {
		t.Logf("Page 2 log %d: ID=%s, Timestamp=%s", i, log.ID, log.Timestamp)
		require.False(t, firstPageIDs[log.ID], "found duplicate log in second page: %s", log.ID)
	}
}

func TestListToolExecutionLogs_FilterByDeployment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	ctx = setProjectID(t, ctx, projectID)

	deploymentID1 := uuid.New().String()
	deploymentID2 := uuid.New().String()
	functionID := uuid.New().String()

	// Insert logs for two different deployments
	insertTestToolExecutionLogs(t, ctx, projectID, deploymentID1, functionID, 3)
	insertTestToolExecutionLogs(t, ctx, projectID, deploymentID2, functionID, 2)

	// Query logs for deployment 1
	result, err := ti.service.ListToolExecutionLogs(ctx, &gen.ListToolExecutionLogsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		TsStart:          nil,
		TsEnd:            nil,
		DeploymentID:     &deploymentID1,
		FunctionID:       nil,
		Instance:         nil,
		Level:            nil,
		Source:           nil,
		Cursor:           nil,
		PerPage:          20,
		Direction:        "next",
		Sort:             "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Logs, 3)

	// Verify all logs are from deployment 1
	for _, log := range result.Logs {
		require.Equal(t, deploymentID1, log.DeploymentID)
	}
}

func TestListToolExecutionLogs_FilterByLevel(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	ctx = setProjectID(t, ctx, projectID)

	deploymentID := uuid.New().String()
	functionID := uuid.New().String()

	// Insert logs with different levels
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	now := time.Now().UTC()
	insertToolExecutionLog(t, ctx, conn, projectID, deploymentID, functionID, now.Add(-5*time.Minute), "info", "stdout", "test-instance")
	insertToolExecutionLog(t, ctx, conn, projectID, deploymentID, functionID, now.Add(-4*time.Minute), "error", "stderr", "test-instance")
	insertToolExecutionLog(t, ctx, conn, projectID, deploymentID, functionID, now.Add(-3*time.Minute), "warn", "stdout", "test-instance")
	insertToolExecutionLog(t, ctx, conn, projectID, deploymentID, functionID, now.Add(-2*time.Minute), "error", "stderr", "test-instance")

	// Query only error logs
	level := "error"
	result, err := ti.service.ListToolExecutionLogs(ctx, &gen.ListToolExecutionLogsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		TsStart:          nil,
		TsEnd:            nil,
		DeploymentID:     nil,
		FunctionID:       nil,
		Instance:         nil,
		Level:            &level,
		Source:           nil,
		Cursor:           nil,
		PerPage:          20,
		Direction:        "next",
		Sort:             "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Logs, 2)

	// Verify all logs are error level
	for _, log := range result.Logs {
		require.Equal(t, "error", log.Level)
	}
}

func TestListToolExecutionLogs_VerifyFields(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	ctx = setProjectID(t, ctx, projectID)

	deploymentID := uuid.New().String()
	functionID := uuid.New().String()
	now := time.Now().UTC()

	// Insert one specific log
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	insertToolExecutionLog(t, ctx, conn, projectID, deploymentID, functionID, now.Add(-1*time.Minute), "info", "stdout", "test-instance-specific")

	// Query logs
	result, err := ti.service.ListToolExecutionLogs(ctx, &gen.ListToolExecutionLogsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		TsStart:          nil,
		TsEnd:            nil,
		DeploymentID:     nil,
		FunctionID:       nil,
		Instance:         nil,
		Level:            nil,
		Source:           nil,
		Cursor:           nil,
		PerPage:          20,
		Direction:        "next",
		Sort:             "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Logs, 1)

	log := result.Logs[0]
	require.Equal(t, projectID, log.ProjectID)
	require.Equal(t, deploymentID, log.DeploymentID)
	require.Equal(t, functionID, log.FunctionID)
	require.Equal(t, "info", log.Level)
	require.Equal(t, "stdout", log.Source)
	require.Equal(t, "test-instance-specific", log.Instance)
	require.NotEmpty(t, log.RawLog)
}

// Helper functions

func insertTestToolExecutionLogs(t *testing.T, ctx context.Context, projectID, deploymentID, functionID string, count int) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	now := time.Now().UTC().Add(-1 * time.Hour)

	for i := 0; i < count; i++ {
		timestamp := now.Add(time.Duration(i) * time.Minute)
		id, err := fromTimeV7(timestamp)
		require.NoError(t, err)

		err = conn.Exec(ctx, `
			INSERT INTO tool_logs (id, timestamp, instance, level, source, raw_log, message, attributes, project_id, deployment_id, function_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, id.String(), timestamp, "test-instance", "info", "stdout", "test raw log", "test message", "{}", projectID, deploymentID, functionID)
		require.NoError(t, err)
	}

	// ClickHouse eventual consistency - sleep once at the end
	time.Sleep(100 * time.Millisecond)
}

func insertToolExecutionLog(t *testing.T, ctx context.Context, conn repo.CHTX, projectID, deploymentID, functionID string, timestamp time.Time, level, source, instance string) {
	t.Helper()

	id, err := fromTimeV7(timestamp)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO tool_logs (id, timestamp, instance, level, source, raw_log, message, attributes, project_id, deployment_id, function_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp, instance, level, source, "test raw log", "test message", "{}", projectID, deploymentID, functionID)
	require.NoError(t, err)

	// ClickHouse eventual consistency
	time.Sleep(100 * time.Millisecond)
}

// fromTimeV7 generates a UUIDv7 from a given timestamp.
// This is necessary for testing cursor-based pagination where the cursor
// embeds a timestamp that must match the actual record timestamp.
func fromTimeV7(t time.Time) (uuid.UUID, error) {
	var u uuid.UUID

	// 1) Encode unix milliseconds into first 48 bits
	ms := t.UnixMilli()
	u[0] = byte(ms >> 40)
	u[1] = byte(ms >> 32)
	u[2] = byte(ms >> 24)
	u[3] = byte(ms >> 16)
	u[4] = byte(ms >> 8)
	u[5] = byte(ms)

	// 2) 12-bit random field (or sub-millisecond precision)
	u[6] = 0
	u[7] = 0

	// 3) Set version (v7)
	u[6] = (u[6] & 0x0F) | 0x70

	// 4) Set variant (RFC 4122)
	u[8] = (u[8] & 0x3F) | 0x80

	return u, nil
}
