package telemetry_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestGetProjectOverviewSessionMode(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsServiceWithSessionCapture(t, true)
	to := time.Now().UTC().Truncate(time.Second)

	result, err := ti.service.GetProjectOverview(ctx, &gen.GetProjectOverviewPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		From:             to.Add(-24 * time.Hour).Format(time.RFC3339),
		To:               to.Format(time.RFC3339),
	})
	require.NoError(t, err)
	require.Equal(t, "session", result.MetricsMode)
	require.NotNil(t, result.Summary)
	require.NotNil(t, result.Comparison)
	require.Empty(t, result.Summary.TopUsers)
	require.Empty(t, result.Summary.TopServers)
	require.Empty(t, result.Summary.LlmClientBreakdown)
}

func TestGetProjectOverviewToolCallMode(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsServiceWithSessionCapture(t, false)
	to := time.Now().UTC().Truncate(time.Second)

	result, err := ti.service.GetProjectOverview(ctx, &gen.GetProjectOverviewPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		From:             to.Add(-24 * time.Hour).Format(time.RFC3339),
		To:               to.Format(time.RFC3339),
	})
	require.NoError(t, err)
	require.Equal(t, "tool_call", result.MetricsMode)
	require.NotNil(t, result.Summary)
	require.NotNil(t, result.Comparison)
	require.Empty(t, result.Summary.TopUsers)
	require.Empty(t, result.Summary.TopServers)
	require.Empty(t, result.Summary.LlmClientBreakdown)
}

func TestGetProjectOverviewCountsHostedServers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsServiceWithSessionCapture(t, false)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	to := time.Now().UTC().Truncate(time.Second)

	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID: projectID, timestamp: to.Add(-2 * time.Minute), toolsetSlug: "payments",
		toolName: "charge", userEmail: "user-1@example.com", statusCode: 200,
	})
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID: projectID, timestamp: to.Add(-time.Minute), toolsetSlug: "support",
		toolName: "lookup", userEmail: "user-2@example.com", statusCode: 200,
	})
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	result, err := ti.service.GetProjectOverview(ctx, &gen.GetProjectOverviewPayload{
		From: to.Add(-24 * time.Hour).Format(time.RFC3339),
		To:   to.Format(time.RFC3339),
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), result.Summary.ActiveServersCount)
	require.ElementsMatch(t, []string{"payments", "support"}, []string{
		result.Summary.TopServers[0].ServerName,
		result.Summary.TopServers[1].ServerName,
	})
}
