package telemetry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

func TestBuildMCPNetworkTrafficResult_ZeroFillsAndTracksLastSeen(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, time.September, 25, 0, 0, 0, 0, time.UTC)
	to := from.Add(4 * time.Hour)
	publicSeen := from.Add(time.Hour + 20*time.Minute)
	privateSeen := from.Add(3*time.Hour + 5*time.Minute)

	result := buildMCPNetworkTrafficResult(from, to, 4, []repo.MCPNetworkTrafficRow{
		{Hour: from.Add(time.Hour), Surface: "public", RequestCount: 5, LastSeen: publicSeen},
		{Hour: from.Add(3 * time.Hour), Surface: "private", RequestCount: 2, LastSeen: privateSeen},
		{Hour: from.Add(3 * time.Hour), Surface: "public", RequestCount: 1, LastSeen: from.Add(3 * time.Hour)},
		// Outside the window and unknown surfaces are ignored.
		{Hour: from.Add(-time.Hour), Surface: "public", RequestCount: 99, LastSeen: from.Add(-time.Minute)},
		{Hour: from, Surface: "other", RequestCount: 7, LastSeen: from},
	})

	require.Equal(t, from.Format(time.RFC3339), result.From)
	require.Equal(t, to.Format(time.RFC3339), result.To)
	require.Len(t, result.Points, 4)
	for i, point := range result.Points {
		require.Equal(t, from.Add(time.Duration(i)*time.Hour).Format(time.RFC3339), point.BucketStart)
	}
	require.Equal(t, []int64{0, 5, 0, 1}, []int64{result.Points[0].PublicRequests, result.Points[1].PublicRequests, result.Points[2].PublicRequests, result.Points[3].PublicRequests})
	require.Equal(t, []int64{0, 0, 0, 2}, []int64{result.Points[0].PrivateRequests, result.Points[1].PrivateRequests, result.Points[2].PrivateRequests, result.Points[3].PrivateRequests})

	require.NotNil(t, result.LastPublicAt)
	require.Equal(t, from.Add(3*time.Hour).Format(time.RFC3339), *result.LastPublicAt)
	require.NotNil(t, result.LastPrivateAt)
	require.Equal(t, privateSeen.Format(time.RFC3339), *result.LastPrivateAt)
}

func TestBuildMCPNetworkTrafficResult_NoTrafficLeavesLastSeenUnset(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, time.September, 25, 0, 0, 0, 0, time.UTC)
	result := buildMCPNetworkTrafficResult(from, from.Add(2*time.Hour), 2, nil)

	require.Len(t, result.Points, 2)
	require.Nil(t, result.LastPublicAt)
	require.Nil(t, result.LastPrivateAt)
}
