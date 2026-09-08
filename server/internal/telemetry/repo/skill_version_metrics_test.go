package repo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildSkillVersionMetricsQueryRestrictsTelemetryToMappedSessions(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	query, _, err := buildSkillVersionMetricsQuery(AttributeMetricsQueryParams{
		ProjectIDs:           []string{"00000000-0000-0000-0000-000000000001"},
		TimeStart:            now.Add(-24 * time.Hour).UnixNano(),
		TimeEnd:              now.UnixNano(),
		GroupBy:              skillVersionDimension,
		SortBy:               "total_cost",
		Filters:              nil,
		CanonicalIdentityOrg: "",
		IntervalSeconds:      int64(time.Hour.Seconds()),
	}, false)
	require.NoError(t, err)

	require.Contains(t, query, "chat_id IN (SELECT session_id FROM skill_session_versions")
}

func TestBuildSkillInsightsQueryRestrictsTelemetryToMappedSessions(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	query, _, err := buildSkillInsightsQuery(QuerySkillInsightsParams{
		OrganizationID:      "test-organization",
		ProjectID:           "00000000-0000-0000-0000-000000000001",
		SkillIDs:            []string{"00000000-0000-0000-0000-000000000002"},
		SkillVersionIDs:     nil,
		From:                now.Add(-24 * time.Hour),
		To:                  now,
		IntervalSeconds:     int64(time.Hour.Seconds()),
		IncludeSessionUsage: true,
	})
	require.NoError(t, err)
	require.Contains(t, query, "chat_id IN (SELECT DISTINCT session_id FROM skill_session_versions")
}

func TestBuildSkillInsightsQueryWithoutSessionUsageSkipsRawTelemetry(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	query, _, err := buildSkillInsightsQuery(QuerySkillInsightsParams{
		OrganizationID:      "test-organization",
		ProjectID:           "00000000-0000-0000-0000-000000000001",
		SkillIDs:            []string{"00000000-0000-0000-0000-000000000002"},
		SkillVersionIDs:     nil,
		From:                now.Add(-24 * time.Hour),
		To:                  now,
		IntervalSeconds:     int64(time.Hour.Seconds()),
		IncludeSessionUsage: false,
	})
	require.NoError(t, err)
	require.NotContains(t, query, "telemetry_logs")
	require.NotContains(t, query, "sessions AS")
	require.Contains(t, query, "toFloat64(0) AS total_session_cost")
}
