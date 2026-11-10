package telemetry_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestGetProjectOverviewEmptyProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	to := time.Now().UTC().Truncate(time.Second)

	result, err := ti.service.GetProjectOverview(ctx, &gen.GetProjectOverviewPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		From:             to.Add(-24 * time.Hour).Format(time.RFC3339),
		To:               to.Format(time.RFC3339),
		IncludeUsage:     false,
	})
	require.NoError(t, err)
	require.NotNil(t, result.Summary)
	require.Zero(t, result.Summary.TotalChats)
	require.Zero(t, result.Summary.TotalToolCalls)
	require.Zero(t, result.Summary.ActiveServersCount)
	require.NotNil(t, result.Summary.TopServers)
	require.Empty(t, result.Summary.TopServers)
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
