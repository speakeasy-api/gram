package chrepo

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func watchdogTestParams() ListRiskFindingsParams {
	from := time.Date(2026, 1, 1, 0, 0, 0, 123456789, time.FixedZone("test-offset", 3600))
	to := from.Add(time.Hour)
	return ListRiskFindingsParams{OrganizationID: "org-test", ProjectID: "project-test", PolicyIDs: []string{"policy-test"}, From: &from, To: &to, CursorTime: &to, CursorID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, Limit: 10}
}

func TestWatchdogListQuery(t *testing.T) {
	t.Parallel()
	p := watchdogTestParams()
	sb, err := watchdogListQuery(p)
	require.NoError(t, err)
	query, args, err := sb.ToSql()
	require.NoError(t, err)
	require.Equal(t, []any{p.OrganizationID, p.ProjectID, p.PolicyIDs[0], "2025-12-31 23:00:00.123456789", "2026-01-01 00:00:00.123456789", "2025-12-31 23:00:00.123456789", "2026-01-01 00:00:00.123456789", p.CursorID.UUID}, args)
	for _, clause := range []string{"organization_id = ?", "project_id = ?", "risk_policy_id IN (?)", "dead_letter_reason = ''", "message_created_at >= toDateTime64(?, 9, 'UTC')", "message_created_at < toDateTime64(?, 9, 'UTC')", "created_at >= toDateTime64(?, 9, 'UTC')", "ORDER BY " + latestCopyOrderSQL + " LIMIT 1 BY id) AS latest WHERE " + liveStateCond, watchdogIdentitySQL, "(message_created_at, id) < (toDateTime64(?, 9, 'UTC'), ?)", "ORDER BY message_created_at DESC, id DESC LIMIT 10", watchdogSettingsSQL} {
		require.Contains(t, query, clause)
	}
	for _, forbidden := range []string{"SELECT *", "match_redacted", "description", "fingerprint", "content", "user_email"} {
		require.NotContains(t, query, forbidden)
	}
	require.Equal(t, 1, strings.Count(query, "excluded_at IS NULL"))
}

func TestWatchdogWholeWindowQueries(t *testing.T) {
	t.Parallel()
	p := watchdogTestParams()
	for dimension, expression := range map[string]string{"severity": "risk_policy_id", "data_type": "category", "team": "team", "app": "chat_source", "user": watchdogIdentitySQL} {
		t.Run(dimension, func(t *testing.T) {
			t.Parallel()
			sb, err := watchdogGroupQuery(p, dimension)
			require.NoError(t, err)
			query, args, err := sb.ToSql()
			require.NoError(t, err)
			require.Len(t, args, 6)
			require.Contains(t, query, "SELECT "+expression+" AS value, count() AS count")
			require.Contains(t, query, "GROUP BY value ORDER BY count DESC, value ASC LIMIT 201")
			require.Contains(t, query, "LIMIT 1 BY id) AS latest WHERE "+liveStateCond)
			require.Contains(t, query, watchdogSettingsSQL)
			require.NotContains(t, query, "(message_created_at, id) <")
		})
	}
	sb, err := watchdogCountQuery(p)
	require.NoError(t, err)
	query, args, err := sb.ToSql()
	require.NoError(t, err)
	require.Len(t, args, 6)
	require.Contains(t, query, "SELECT count() FROM")
	require.Contains(t, query, "LIMIT 1 BY id) AS latest WHERE "+liveStateCond)
	require.Contains(t, query, watchdogSettingsSQL)
	require.NotContains(t, query, "(message_created_at, id) <")
	require.NotContains(t, query, "LIMIT 10")
}

func TestWatchdogValidation(t *testing.T) {
	t.Parallel()
	p := watchdogTestParams()
	_, err := watchdogGroupQuery(p, "team; SELECT secret")
	require.Error(t, err)
	p.PolicyIDs = nil
	_, err = watchdogCountQuery(p)
	require.ErrorIs(t, err, errEmptyPolicyIDs)
	p = watchdogTestParams()
	p.From = nil
	_, err = watchdogCountQuery(p)
	require.Error(t, err)
	p = watchdogTestParams()
	p.To = p.From
	_, err = watchdogCountQuery(p)
	require.Error(t, err)
	p = watchdogTestParams()
	p.Limit = 0
	_, err = watchdogListQuery(p)
	require.Error(t, err)
}
