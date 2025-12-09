package telemetry_test

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var (
	infra *testenv.Environment
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background())
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

func TestListToolLogs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	queries := telemetry.New(logger, tracerProvider, conn, nil)

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

	insertTestLogs(t, ctx, conn, projectID, testLogs)

	tests := []struct {
		name   string
		params telemetry.ListToolLogsParams
		assert func(t *testing.T, result *telemetry.ToolLogsListResult, err error)
	}{
		{
			name: "lists all logs for a project",
			params: telemetry.ListToolLogsParams{
				ProjectID: projectID,
				TsStart:   now.Add(-10 * time.Minute),
				TsEnd:     now.Add(10 * time.Minute),
				SortOrder: "DESC",
				Cursor:    uuid.Nil.String(),
				Limit:     10,
			},
			assert: func(t *testing.T, result *telemetry.ToolLogsListResult, err error) {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, result.Logs, 4)
				require.Equal(t, 9, result.Pagination.PerPage)
				require.False(t, result.Pagination.HasNextPage)
			},
		},
		{
			name: "filters logs by deployment ID",
			params: telemetry.ListToolLogsParams{
				ProjectID:    projectID,
				DeploymentID: deploymentID,
				TsStart:      now.Add(-10 * time.Minute),
				TsEnd:        now.Add(10 * time.Minute),
				SortOrder:    "DESC",
				Cursor:       uuid.Nil.String(),
				Limit:        10,
			},
			assert: func(t *testing.T, result *telemetry.ToolLogsListResult, err error) {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, result.Logs, 3)
			},
		},
		{
			name: "filters logs by function ID",
			params: telemetry.ListToolLogsParams{
				ProjectID:  projectID,
				FunctionID: functionID,
				TsStart:    now.Add(-10 * time.Minute),
				TsEnd:      now.Add(10 * time.Minute),
				SortOrder:  "DESC",
				Cursor:     uuid.Nil.String(),
				Limit:      10,
			},
			assert: func(t *testing.T, result *telemetry.ToolLogsListResult, err error) {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, result.Logs, 3)
			},
		},
		{
			name: "filters logs by level",
			params: telemetry.ListToolLogsParams{
				ProjectID: projectID,
				Level:     "error",
				TsStart:   now.Add(-10 * time.Minute),
				TsEnd:     now.Add(10 * time.Minute),
				SortOrder: "DESC",
				Cursor:    uuid.Nil.String(),
				Limit:     10,
			},
			assert: func(t *testing.T, result *telemetry.ToolLogsListResult, err error) {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, result.Logs, 1)
				require.Equal(t, "error", result.Logs[0].Level)
			},
		},
		{
			name: "filters logs by source",
			params: telemetry.ListToolLogsParams{
				ProjectID: projectID,
				Source:    "user",
				TsStart:   now.Add(-10 * time.Minute),
				TsEnd:     now.Add(10 * time.Minute),
				SortOrder: "DESC",
				Cursor:    uuid.Nil.String(),
				Limit:     10,
			},
			assert: func(t *testing.T, result *telemetry.ToolLogsListResult, err error) {
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
			params: telemetry.ListToolLogsParams{
				ProjectID: projectID,
				Instance:  "test-instance-1",
				TsStart:   now.Add(-10 * time.Minute),
				TsEnd:     now.Add(10 * time.Minute),
				SortOrder: "DESC",
				Cursor:    uuid.Nil.String(),
				Limit:     10,
			},
			assert: func(t *testing.T, result *telemetry.ToolLogsListResult, err error) {
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
			params: telemetry.ListToolLogsParams{
				ProjectID: projectID,
				TsStart:   now.Add(-10 * time.Minute),
				TsEnd:     now.Add(10 * time.Minute),
				SortOrder: "DESC",
				Cursor:    uuid.Nil.String(),
				Limit:     3,
			},
			assert: func(t *testing.T, result *telemetry.ToolLogsListResult, err error) {
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
			params: telemetry.ListToolLogsParams{
				ProjectID: projectID,
				TsStart:   now.Add(-10 * time.Minute),
				TsEnd:     now.Add(10 * time.Minute),
				SortOrder: "ASC",
				Cursor:    uuid.Nil.String(),
				Limit:     10,
			},
			assert: func(t *testing.T, result *telemetry.ToolLogsListResult, err error) {
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
			params: telemetry.ListToolLogsParams{
				ProjectID: projectID,
				TsStart:   now.Add(-10 * time.Minute),
				TsEnd:     now.Add(10 * time.Minute),
				SortOrder: "DESC",
				Cursor:    uuid.Nil.String(),
				Limit:     10,
			},
			assert: func(t *testing.T, result *telemetry.ToolLogsListResult, err error) {
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
			params: telemetry.ListToolLogsParams{
				ProjectID: projectID,
				TsStart:   now.Add(-4*time.Minute - 30*time.Second),
				TsEnd:     now.Add(-2*time.Minute - 30*time.Second),
				SortOrder: "DESC",
				Cursor:    uuid.Nil.String(),
				Limit:     10,
			},
			assert: func(t *testing.T, result *telemetry.ToolLogsListResult, err error) {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, result.Logs, 2)
			},
		},
		{
			name: "combines multiple filters correctly",
			params: telemetry.ListToolLogsParams{
				ProjectID:    projectID,
				DeploymentID: deploymentID,
				FunctionID:   functionID,
				Source:       "server",
				TsStart:      now.Add(-10 * time.Minute),
				TsEnd:        now.Add(10 * time.Minute),
				SortOrder:    "DESC",
				Cursor:       uuid.Nil.String(),
				Limit:        10,
			},
			assert: func(t *testing.T, result *telemetry.ToolLogsListResult, err error) {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, result.Logs, 2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

func insertTestLogs(t *testing.T, ctx context.Context, conn telemetry.CHTX, projectID string, logs []testLog) {
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
