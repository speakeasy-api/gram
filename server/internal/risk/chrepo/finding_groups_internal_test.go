package chrepo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGroupRiskFindingsByChatQuery(t *testing.T) {
	t.Parallel()

	sb, err := groupRiskFindingsByChatQuery(GroupRiskFindingsByChatParams{OrganizationID: "org-test", ProjectID: "project-test", PolicyIDs: []string{"policy-a", "policy-b"}, CursorChatID: "chat-cursor", Limit: 26})
	require.NoError(t, err)
	query, args, err := sb.ToSql()
	require.NoError(t, err)
	require.Equal(t, []any{"org-test", "project-test", "policy-a", "policy-b", "chat-cursor"}, args)
	for _, clause := range []string{
		"organization_id = ?", "project_id = ?", "dead_letter_reason = ''", notShadowCond,
		"risk_policy_id IN (?,?)", "chat_id != ''", "chat_id <= ?",
		"ORDER BY " + latestCopyOrderSQL + " LIMIT 1 BY id",
		") AS latest WHERE " + liveStateCond,
		"argMax(external_user_id, created_at) AS external_user_id", "count() AS findings_count", "max(created_at) AS latest_detected",
		"GROUP BY chat_id ORDER BY chat_id DESC LIMIT 26",
	} {
		require.Contains(t, query, clause)
	}
	for _, forbidden := range []string{"match_redacted", "description", "user_email"} {
		require.NotContains(t, query, forbidden)
	}

	sb, err = groupRiskFindingsByChatQuery(GroupRiskFindingsByChatParams{OrganizationID: "org-test", ProjectID: "project-test", PolicyIDs: []string{"policy-a"}, CursorChatID: "", Limit: 26})
	require.NoError(t, err)
	query, _, err = sb.ToSql()
	require.NoError(t, err)
	require.NotContains(t, query, "chat_id <= ?")

	_, err = groupRiskFindingsByChatQuery(GroupRiskFindingsByChatParams{OrganizationID: "org-test", ProjectID: "project-test", PolicyIDs: nil, CursorChatID: "", Limit: 26})
	require.ErrorIs(t, err, errEmptyPolicyIDs)
}

func TestRuleCountsByCategoryQuery(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := RiskOverviewWindowParams{OrganizationID: "org-test", ProjectID: "project-test", From: from, To: from.Add(7 * 24 * time.Hour)}
	query, args, err := ruleCountsByCategoryQuery(p, "secrets", 1001).ToSql()
	require.NoError(t, err)
	require.Equal(t, []any{"org-test", "project-test", p.From, p.To, "secrets"}, args)
	for _, clause := range []string{
		"created_at >= ?", "created_at < ?",
		"ROW_NUMBER() OVER (PARTITION BY id ORDER BY " + latestCopyOrderSQL + ") AS rn",
		"rn = 1 AND dead_letter_reason = '' AND excluded_at IS NULL AND false_positive_at IS NULL AND category = ?",
		"uniqExact(id) AS findings", "GROUP BY rule_id, source ORDER BY findings DESC, rule_id ASC LIMIT 1001",
	} {
		require.Contains(t, query, clause)
	}
	require.NotContains(t, query, "risk_policy_id IN")
}
