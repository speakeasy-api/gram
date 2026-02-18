package telemetry_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/require"
)

func TestGetObservabilityOverview_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
		From:              from,
		To:                to,
		IncludeTimeSeries: true,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Summary)
	require.NotNil(t, result.Comparison)

	// All metrics should be zero for empty data
	require.Equal(t, int64(0), result.Summary.TotalChats)
	require.Equal(t, int64(0), result.Summary.ResolvedChats)
	require.Equal(t, int64(0), result.Summary.FailedChats)
	require.Equal(t, int64(0), result.Summary.TotalToolCalls)
}

func TestGetObservabilityOverview_WithChatResolutions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	chatID1 := uuid.New().String()
	chatID2 := uuid.New().String()
	chatID3 := uuid.New().String()

	// Insert resolution events for chats
	insertResolutionLog(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID1, "success", 85)
	insertResolutionLog(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), chatID2, "failure", 25)
	insertResolutionLog(t, ctx, projectID, deploymentID, now.Add(-6*time.Minute), chatID3, "partial", 60)

	// Insert some tool calls
	insertToolCallLog(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), "tools:http:petstore:listPets", 200, 0.5)
	insertToolCallLog(t, ctx, projectID, deploymentID, now.Add(-7*time.Minute), "tools:http:petstore:getPet", 500, 1.0)

	// Wait for ClickHouse eventual consistency
	time.Sleep(200 * time.Millisecond)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
		From:              from,
		To:                to,
		IncludeTimeSeries: true,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Summary)

	// Verify chat resolution metrics
	require.Equal(t, int64(3), result.Summary.TotalChats)
	require.Equal(t, int64(1), result.Summary.ResolvedChats)
	require.Equal(t, int64(1), result.Summary.FailedChats)

	// Verify tool metrics
	require.Equal(t, int64(2), result.Summary.TotalToolCalls)
	require.Equal(t, int64(1), result.Summary.FailedToolCalls)

	// Verify time series is returned
	require.NotEmpty(t, result.TimeSeries)
}

func TestGetObservabilityOverview_TimeSeriesMetrics(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()

	// Insert data at different times to verify time bucketing
	chatID1 := uuid.New().String()
	chatID2 := uuid.New().String()

	insertResolutionLog(t, ctx, projectID, deploymentID, now.Add(-50*time.Minute), chatID1, "success", 90)
	insertResolutionLog(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID2, "success", 85)

	time.Sleep(200 * time.Millisecond)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
		From:              from,
		To:                to,
		IncludeTimeSeries: true,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.TimeSeries)

	// With 2-hour range and auto-calculated 15-min buckets, we should have 8+ buckets
	require.GreaterOrEqual(t, len(result.TimeSeries), 8)

	// Verify the first bucket has a valid timestamp
	require.Positive(t, result.TimeSeries[0].BucketTimeUnixNano)
}

func TestGetObservabilityOverview_FromAfterTo(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(1 * time.Hour).Format(time.RFC3339) // after
	to := now.Add(-1 * time.Hour).Format(time.RFC3339)  // before

	_, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
		From:              from,
		To:                to,
		IncludeTimeSeries: true,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "'from' time must be before 'to' time")
}

func TestGetObservabilityOverview_LogsDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = switchOrganizationInCtx(t, ctx, ti.disabledLogsOrgID)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	_, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
		From:              from,
		To:                to,
		IncludeTimeSeries: true,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "logs are not enabled")
}

// insertResolutionLog inserts a chat resolution event log
func insertResolutionLog(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, chatID string, resolution string, score int) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gen_ai.conversation.id":        chatID,
		"gen_ai.conversation.duration":  30.0, // 30 seconds
		"gram.resource.urn":             "agents:chat:resolution",
		"evaluation.score":              score,
		"gen_ai.evaluation.score.label": resolution, // This feeds the MATERIALIZED column
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, gram_chat_id,
			service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "chat resolution",
		nil, nil, string(attrsJSON), "{}",
		projectID, deploymentID, "agents:chat:resolution", chatID,
		"gram-agents")
	require.NoError(t, err)
}
