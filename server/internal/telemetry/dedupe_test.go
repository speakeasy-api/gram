package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/stretchr/testify/require"
)

const codexUsageMetricsURN = "codex:usage:metrics"

func TestLogBulkDeduped_DropsRepeatsWithinBatch(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	timestamp := time.Now().UTC()
	repeated := uuid.NewString()

	// The compliance feed shape this reproduces: one event delivered in two
	// different log files inside the same page.
	dropped, err := ti.telemLogger.LogBulkDeduped(ctx, attr.CodexComplianceEventHashKey, []telemetry.LogParams{
		newCostEventParams(ti.orgID, projectID, repeated, timestamp),
		newCostEventParams(ti.orgID, projectID, uuid.NewString(), timestamp),
		newCostEventParams(ti.orgID, projectID, repeated, timestamp),
	})
	require.NoError(t, err)
	require.Equal(t, 1, dropped)
	require.Len(t, listCostEventLogs(t, ctx, ti, projectID, timestamp), 2)
}

func TestLogBulkDeduped_SkipsEventsAlreadyIngested(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	timestamp := time.Now().UTC()
	batch := []telemetry.LogParams{
		newCostEventParams(ti.orgID, projectID, uuid.NewString(), timestamp),
		newCostEventParams(ti.orgID, projectID, uuid.NewString(), timestamp),
	}

	dropped, err := ti.telemLogger.LogBulkDeduped(ctx, attr.CodexComplianceEventHashKey, batch)
	require.NoError(t, err)
	require.Equal(t, 0, dropped)
	require.Len(t, listCostEventLogs(t, ctx, ti, projectID, timestamp), 2)

	// Re-importing the same window is the failure that doubles every
	// downstream token and cost sum.
	dropped, err = ti.telemLogger.LogBulkDeduped(ctx, attr.CodexComplianceEventHashKey, batch)
	require.NoError(t, err)
	require.Equal(t, 2, dropped)
	require.Len(t, listCostEventLogs(t, ctx, ti, projectID, timestamp), 2)
}

// The lookup chunks its IN list to stay under ClickHouse's max_query_size, so a
// batch larger than one chunk has to reassemble results across queries.
func TestLogBulkDeduped_SkipsIngestedEventsAcrossChunkBoundary(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	timestamp := time.Now().UTC()
	batch := make([]telemetry.LogParams, 0, 2500)
	for range 2500 {
		batch = append(batch, newCostEventParams(ti.orgID, projectID, uuid.NewString(), timestamp))
	}

	dropped, err := ti.telemLogger.LogBulkDeduped(ctx, attr.CodexComplianceEventHashKey, batch)
	require.NoError(t, err)
	require.Equal(t, 0, dropped)

	dropped, err = ti.telemLogger.LogBulkDeduped(ctx, attr.CodexComplianceEventHashKey, batch)
	require.NoError(t, err)
	require.Equal(t, 2500, dropped, "every event should be recognized, including those past the first chunk")
}

func TestLogBulkDeduped_KeepsSameEventInDifferentProjects(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)

	firstProjectID := uuid.New().String()
	secondProjectID := uuid.New().String()
	timestamp := time.Now().UTC()
	shared := uuid.NewString()

	dropped, err := ti.telemLogger.LogBulkDeduped(ctx, attr.CodexComplianceEventHashKey, []telemetry.LogParams{
		newCostEventParams(ti.orgID, firstProjectID, shared, timestamp),
		newCostEventParams(ti.orgID, secondProjectID, shared, timestamp),
	})
	require.NoError(t, err)
	require.Equal(t, 0, dropped)
	require.Len(t, listCostEventLogs(t, ctx, ti, firstProjectID, timestamp), 1)
	require.Len(t, listCostEventLogs(t, ctx, ti, secondProjectID, timestamp), 1)
}

func TestLogBulkDeduped_KeepsRowsCarryingNoFingerprint(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()
	timestamp := time.Now().UTC()

	unfingerprinted := newCostEventParams(ti.orgID, projectID, "", timestamp)
	delete(unfingerprinted.Attributes, attr.CodexComplianceEventHashKey)

	dropped, err := ti.telemLogger.LogBulkDeduped(ctx, attr.CodexComplianceEventHashKey, []telemetry.LogParams{
		unfingerprinted,
		unfingerprinted,
	})
	require.NoError(t, err)
	require.Equal(t, 0, dropped)
	require.Len(t, listCostEventLogs(t, ctx, ti, projectID, timestamp), 2)
}

// newCostEventParams builds a row shaped like the ones the Codex compliance
// COSTS importer writes: a codex:usage:metrics row fingerprinted by event id.
func newCostEventParams(orgID, projectID, eventHash string, timestamp time.Time) telemetry.LogParams {
	attrs := map[attr.Key]any{
		attr.ResourceURNKey:           codexUsageMetricsURN,
		attr.ProjectIDKey:             projectID,
		attr.OrganizationIDKey:        orgID,
		attr.GenAIUsageTotalTokensKey: int64(1000),
	}
	if eventHash != "" {
		attrs[attr.CodexComplianceEventHashKey] = eventHash
	}

	return telemetry.LogParams{
		Timestamp: timestamp,
		ToolInfo: telemetry.ToolInfo{
			ID:             "",
			URN:            codexUsageMetricsURN,
			Name:           "codex",
			ProjectID:      projectID,
			DeploymentID:   "",
			FunctionID:     nil,
			OrganizationID: orgID,
		},
		UserInfo:   telemetry.UserInfoByEmail("dedupe@example.test"),
		Attributes: attrs,
	}
}

func listCostEventLogs(t *testing.T, ctx context.Context, ti *testInstance, projectID string, timestamp time.Time) []repo.TelemetryLog {
	t.Helper()

	logs, err := ti.chClient.ListTelemetryLogs(ctx, repo.ListTelemetryLogsParams{
		GramProjectID: projectID,
		TimeStart:     timestamp.Add(-1 * time.Minute).UnixNano(),
		TimeEnd:       timestamp.Add(1 * time.Minute).UnixNano(),
		GramURNs:      []string{codexUsageMetricsURN},
		SortOrder:     "desc",
		Cursor:        "",
		Limit:         50,
	})
	require.NoError(t, err)
	return logs
}
