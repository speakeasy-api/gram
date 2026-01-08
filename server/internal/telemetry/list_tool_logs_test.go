package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestListToolLogs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	queries := repo.New(logger, tracerProvider, conn)

	projectID := uuid.New().String()
	deploymentID := uuid.New().String()
	functionID := uuid.New().String()
	now := time.Now().UTC()

	// Create test data
	testLogs := []testLog{
		{
			id:           uuid.New().String(),
			timestamp:    now.Add(-5 * time.Minute),
			instance:     "test-instance-1",
			level:        "info",
			source:       "server",
			rawLog:       "test log 1",
			message:      "test message 1",
			deploymentID: deploymentID,
			functionID:   functionID,
		},
		{
			id:           uuid.New().String(),
			timestamp:    now.Add(-4 * time.Minute),
			instance:     "test-instance-1",
			level:        "error",
			source:       "user",
			rawLog:       "test log 2",
			message:      "test message 2",
			deploymentID: deploymentID,
			functionID:   functionID,
		},
		{
			id:           uuid.New().String(),
			timestamp:    now.Add(-3 * time.Minute),
			instance:     "test-instance-2",
			level:        "warn",
			source:       "server",
			rawLog:       "test log 3",
			message:      "test message 3",
			deploymentID: deploymentID,
			functionID:   functionID,
		},
		{
			id:           uuid.New().String(),
			timestamp:    now.Add(-2 * time.Minute),
			instance:     "test-instance-2",
			level:        "info",
			source:       "user",
			rawLog:       "test log 4",
			message:      "test message 4",
			deploymentID: uuid.New().String(), // Different deployment
			functionID:   uuid.New().String(), // Different function
		},
	}

	insertTestToolLogs(t, ctx, conn, projectID, testLogs)

	tests := []struct {
		name   string
		params repo.ListToolLogsParams
		assert func(t *testing.T, result *repo.ToolLogsListResult, err error)
	}{
		{
			name: "lists all logs for a project",
			params: repo.ListToolLogsParams{
				ProjectID: projectID,
				TsStart:   now.Add(-10 * time.Minute),
				TsEnd:     now.Add(10 * time.Minute),
				SortOrder: "desc",
				Cursor:    uuid.Nil.String(),
				Limit:     10,
			},
			assert: func(t *testing.T, result *repo.ToolLogsListResult, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, result.Logs, 4)
				require.Equal(t, 9, result.Pagination.PerPage)
				require.False(t, result.Pagination.HasNextPage)
			},
		},
		{
			name: "filters logs by deployment ID",
			params: repo.ListToolLogsParams{
				ProjectID:    projectID,
				DeploymentID: deploymentID,
				TsStart:      now.Add(-10 * time.Minute),
				TsEnd:        now.Add(10 * time.Minute),
				SortOrder:    "desc",
				Cursor:       uuid.Nil.String(),
				Limit:        10,
			},
			assert: func(t *testing.T, result *repo.ToolLogsListResult, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, result.Logs, 3)
			},
		},
		{
			name: "filters logs by function ID",
			params: repo.ListToolLogsParams{
				ProjectID:  projectID,
				FunctionID: functionID,
				TsStart:    now.Add(-10 * time.Minute),
				TsEnd:      now.Add(10 * time.Minute),
				SortOrder:  "desc",
				Cursor:     uuid.Nil.String(),
				Limit:      10,
			},
			assert: func(t *testing.T, result *repo.ToolLogsListResult, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, result.Logs, 3)
			},
		},
		{
			name: "filters logs by level",
			params: repo.ListToolLogsParams{
				ProjectID: projectID,
				Level:     "error",
				TsStart:   now.Add(-10 * time.Minute),
				TsEnd:     now.Add(10 * time.Minute),
				SortOrder: "desc",
				Cursor:    uuid.Nil.String(),
				Limit:     10,
			},
			assert: func(t *testing.T, result *repo.ToolLogsListResult, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, result.Logs, 1)
				require.Equal(t, "error", result.Logs[0].Level)
			},
		},
		{
			name: "filters logs by source",
			params: repo.ListToolLogsParams{
				ProjectID: projectID,
				Source:    "user",
				TsStart:   now.Add(-10 * time.Minute),
				TsEnd:     now.Add(10 * time.Minute),
				SortOrder: "desc",
				Cursor:    uuid.Nil.String(),
				Limit:     10,
			},
			assert: func(t *testing.T, result *repo.ToolLogsListResult, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, result.Logs, 2)
				for _, log := range result.Logs {
					require.Equal(t, "user", log.Source)
				}
			},
		},
		{
			name: "filters logs by instance",
			params: repo.ListToolLogsParams{
				ProjectID: projectID,
				Instance:  "test-instance-1",
				TsStart:   now.Add(-10 * time.Minute),
				TsEnd:     now.Add(10 * time.Minute),
				SortOrder: "desc",
				Cursor:    uuid.Nil.String(),
				Limit:     10,
			},
			assert: func(t *testing.T, result *repo.ToolLogsListResult, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, result.Logs, 2)
				for _, log := range result.Logs {
					require.Equal(t, "test-instance-1", log.Instance)
				}
			},
		},
		{
			name: "paginates logs with configurable limit",
			params: repo.ListToolLogsParams{
				ProjectID: projectID,
				TsStart:   now.Add(-10 * time.Minute),
				TsEnd:     now.Add(10 * time.Minute),
				SortOrder: "desc",
				Cursor:    uuid.Nil.String(),
				Limit:     3,
			},
			assert: func(t *testing.T, result *repo.ToolLogsListResult, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, result.Logs, 2)
				require.Equal(t, 2, result.Pagination.PerPage)
				require.True(t, result.Pagination.HasNextPage)
				require.NotNil(t, result.Pagination.NextPageCursor)
			},
		},
		{
			name: "sorts logs in ascending order by timestamp",
			params: repo.ListToolLogsParams{
				ProjectID: projectID,
				TsStart:   now.Add(-10 * time.Minute),
				TsEnd:     now.Add(10 * time.Minute),
				SortOrder: "asc",
				Cursor:    uuid.Nil.String(),
				Limit:     10,
			},
			assert: func(t *testing.T, result *repo.ToolLogsListResult, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, result.Logs, 4)
				for i := 1; i < len(result.Logs); i++ {
					require.True(t, result.Logs[i].Timestamp.After(result.Logs[i-1].Timestamp) ||
						result.Logs[i].Timestamp.Equal(result.Logs[i-1].Timestamp))
				}
			},
		},
		{
			name: "sorts logs in descending order by timestamp",
			params: repo.ListToolLogsParams{
				ProjectID: projectID,
				TsStart:   now.Add(-10 * time.Minute),
				TsEnd:     now.Add(10 * time.Minute),
				SortOrder: "desc",
				Cursor:    uuid.Nil.String(),
				Limit:     10,
			},
			assert: func(t *testing.T, result *repo.ToolLogsListResult, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, result.Logs, 4)
				for i := 1; i < len(result.Logs); i++ {
					require.True(t, result.Logs[i].Timestamp.Before(result.Logs[i-1].Timestamp) ||
						result.Logs[i].Timestamp.Equal(result.Logs[i-1].Timestamp))
				}
			},
		},
		{
			name: "filters logs by time range",
			params: repo.ListToolLogsParams{
				ProjectID: projectID,
				TsStart:   now.Add(-4*time.Minute - 30*time.Second),
				TsEnd:     now.Add(-2*time.Minute - 30*time.Second),
				SortOrder: "desc",
				Cursor:    uuid.Nil.String(),
				Limit:     10,
			},
			assert: func(t *testing.T, result *repo.ToolLogsListResult, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, result.Logs, 2)
			},
		},
		{
			name: "combines multiple filters correctly",
			params: repo.ListToolLogsParams{
				ProjectID:    projectID,
				DeploymentID: deploymentID,
				FunctionID:   functionID,
				Source:       "server",
				TsStart:      now.Add(-10 * time.Minute),
				TsEnd:        now.Add(10 * time.Minute),
				SortOrder:    "desc",
				Cursor:       uuid.Nil.String(),
				Limit:        10,
			},
			assert: func(t *testing.T, result *repo.ToolLogsListResult, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, result.Logs, 2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := queries.ListToolLogs(ctx, tt.params)
			tt.assert(t, result, err)
		})
	}
}

type testLog struct {
	id           string
	timestamp    time.Time
	instance     string
	level        string
	source       string
	rawLog       string
	message      string
	deploymentID string
	functionID   string
}

func insertTestToolLogs(t *testing.T, ctx context.Context, conn repo.CHTX, projectID string, logs []testLog) {
	t.Helper()

	for _, log := range logs {
		err := conn.Exec(ctx, `
			INSERT INTO tool_logs (
				id, timestamp, instance, level, source, raw_log, message, attributes,
				project_id, deployment_id, function_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			log.id,
			log.timestamp,
			log.instance,
			log.level,
			log.source,
			log.rawLog,
			log.message,
			"{}",
			projectID,
			log.deploymentID,
			log.functionID,
		)
		require.NoError(t, err)
	}

	// Give ClickHouse a moment to make data available for queries
	time.Sleep(100 * time.Millisecond)
}
