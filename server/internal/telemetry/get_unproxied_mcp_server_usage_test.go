package telemetry_test

import (
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/stretchr/testify/require"
)

func TestGetUnproxiedMcpServerUsage_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-24 * time.Hour).Format(time.RFC3339)
	to := now.Format(time.RFC3339)

	result, err := ti.service.GetUnproxiedMcpServerUsage(ctx, &gen.GetUnproxiedMcpServerUsagePayload{
		URL:  "https://vendor.example.com/mcp",
		From: from,
		To:   to,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Buckets)
}

func TestGetUnproxiedMcpServerUsage_InvalidURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-24 * time.Hour).Format(time.RFC3339)
	to := now.Format(time.RFC3339)

	result, err := ti.service.GetUnproxiedMcpServerUsage(ctx, &gen.GetUnproxiedMcpServerUsagePayload{
		URL:  "not a url",
		From: from,
		To:   to,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Buckets)
}
