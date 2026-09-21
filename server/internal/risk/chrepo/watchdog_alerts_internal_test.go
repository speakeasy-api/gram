package chrepo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func watchdogAlertTestParams() RiskSignalWindowParams {
	from := time.Date(2026, 1, 1, 0, 0, 0, 123456789, time.FixedZone("test-offset", 3600))
	return RiskSignalWindowParams{OrganizationID: "org-test", ProjectID: "project-test", From: from, To: from.Add(time.Hour), WideFrom: from.Add(-time.Hour)}
}

func TestWatchdogAlertsQuery(t *testing.T) {
	t.Parallel()
	p := watchdogAlertTestParams()
	sb, err := watchdogAlertsQuery(p, WatchdogAlertLimit)
	require.NoError(t, err)
	query, args, err := sb.ToSql()
	require.NoError(t, err)
	require.Equal(t, []any{p.OrganizationID, p.ProjectID, "2025-12-31 23:00:00.123456789", "2026-01-01 00:00:00.123456789"}, args)
	for _, clause := range []string{
		"organization_id = ?", "project_id = ?",
		"created_at >= toDateTime64(?, 9, 'UTC')", "created_at < toDateTime64(?, 9, 'UTC')",
		"ROW_NUMBER() OVER (PARTITION BY id ORDER BY " + latestCopyOrderSQL + ") AS rn",
		") AS latest WHERE rn = 1 AND dead_letter_reason = '' AND excluded_at IS NULL AND false_positive_at IS NULL",
		"uniqExactIf(" + signalUserExpr + ", " + signalUserNonEmpty + ")",
		"arraySort(groupUniqArrayIf(201)(risk_policy_id, risk_policy_id != ''))",
		"arraySort(groupUniqArrayIf(201)(chat_source, chat_source != ''))",
		"min(message_created_at) AS first_seen", "max(message_created_at) AS last_seen",
		"argMaxIf(match_redacted, tuple(message_created_at, id), match_redacted != '') AS sample_evidence",
		"GROUP BY rule_id ORDER BY finding_count DESC, rule_id ASC LIMIT 1001", watchdogSettingsSQL,
	} {
		require.Contains(t, query, clause)
	}
	for _, forbidden := range []string{"risk_policy_id IN", "message_created_at >=", "message_created_at <", "GROUP BY risk_policy_id", "description", "rationale", "argMin(match,"} {
		require.NotContains(t, query, forbidden)
	}
}

func TestWatchdogAlertGroupsQuery(t *testing.T) {
	t.Parallel()
	for dimension, expression := range map[string]string{"data_type": "category", "team": "team", "app": "chat_source", "user": signalUserExpr} {
		t.Run(dimension, func(t *testing.T) {
			t.Parallel()
			sb, err := watchdogAlertGroupsQuery(watchdogAlertTestParams(), []string{"rule-a", "rule-b"}, dimension)
			require.NoError(t, err)
			query, args, err := sb.ToSql()
			require.NoError(t, err)
			require.Len(t, args, 6)
			require.Equal(t, []any{"rule-a", "rule-b"}, args[4:])
			require.Contains(t, query, "rule_id IN (?,?)")
			require.Contains(t, query, expression+" AS value, count() AS count")
			require.Contains(t, query, "GROUP BY rule_id, value ORDER BY rule_id ASC, count DESC, value ASC LIMIT 201 BY rule_id")
			require.Contains(t, query, "rn = 1 AND dead_letter_reason = '' AND excluded_at IS NULL AND false_positive_at IS NULL")
			require.Contains(t, query, watchdogSettingsSQL)
			if dimension == "user" {
				require.Contains(t, query, "AND "+signalUserNonEmpty)
			}
		})
	}
	_, err := watchdogAlertGroupsQuery(watchdogAlertTestParams(), []string{"rule-a"}, "severity")
	require.Error(t, err)
}

func TestWatchdogAlertsValidation(t *testing.T) {
	t.Parallel()
	for _, limit := range []uint64{0, 1002} {
		_, err := watchdogAlertsQuery(watchdogAlertTestParams(), limit)
		require.Error(t, err)
	}
	for _, mutate := range []func(*RiskSignalWindowParams){
		func(p *RiskSignalWindowParams) { p.OrganizationID = "" },
		func(p *RiskSignalWindowParams) { p.ProjectID = "" },
		func(p *RiskSignalWindowParams) { p.From = time.Time{} },
		func(p *RiskSignalWindowParams) { p.To = p.From },
		func(p *RiskSignalWindowParams) { p.To = p.From.Add(-time.Second) },
	} {
		p := watchdogAlertTestParams()
		mutate(&p)
		_, err := watchdogAlertsQuery(p, 1001)
		require.Error(t, err)
		_, err = watchdogAlertGroupsQuery(p, []string{"rule-a"}, "app")
		require.Error(t, err)
	}
}

func TestWatchdogAlertGroupRuleLimit(t *testing.T) {
	t.Parallel()
	for _, ids := range [][]string{nil, make([]string, 101)} {
		_, err := watchdogAlertGroupsQuery(watchdogAlertTestParams(), ids, "app")
		require.Error(t, err)
	}
}
