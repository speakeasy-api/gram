package telemetry_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
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
