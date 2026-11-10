package plugins

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratePlatformMCPRiskAudienceWorkflow(t *testing.T) {
	t.Parallel()
	files, err := PublicPlatformMCPFiles("https://example.com", "17")
	require.NoError(t, err)
	const path = "skills/manage-risk-policy-audience/SKILL.md"
	workflow := files["speakeasy/"+path]
	require.NotEmpty(t, workflow)
	require.Equal(t, workflow, files["agent-plugins/speakeasy/"+path])
	text := string(workflow)
	cursor := 0
	for _, name := range []string{"list_projects", "list_risk_policies", "get_risk_policy", "remove_self_from_risk_policy", "change_risk_policy_audience", "get_risk_policy", "change_risk_policy_audience", "update_risk_policy", "get_risk_policy"} {
		token := "`" + name + "`"
		index := strings.Index(text[cursor:], token)
		require.NotEqual(t, -1, index, "%s must appear in workflow order", name)
		cursor += index + len(token)
	}
	for _, guardrail := range []string{
		"external OAuth connection",
		"Stop without writing",
		"organization-scoped coordination",
		"always refuses",
		"Do not bypass this refusal",
		"before database transactions or receipt replay",
		"obtain confirmation again",
		"`confirmed: true`",
		"stable `idempotency_key`",
		"`confirm: true`",
		"positive user/role grants",
		"`add_principals`",
		"`remove_principals`",
		"Native directory groups are not supported",
		"Removing a direct user grant does not remove role-derived coverage",
		"Never use an audience delta to bypass a self-removal refusal",
		"atomically preserves all audience entries outside the delta",
		"historical commit, not current state",
	} {
		require.Contains(t, text, guardrail)
	}
	require.NotContains(t, text, "Gram")
}
