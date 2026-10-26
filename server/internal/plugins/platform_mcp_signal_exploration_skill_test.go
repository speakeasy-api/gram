package plugins

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratePlatformMCPPackageEmitsSignalExplorationWorkflow(t *testing.T) {
	t.Parallel()
	files, err := PublicPlatformMCPFiles("https://example.com", "17")
	require.NoError(t, err)
	const path = "skills/explore-signal-patterns/SKILL.md"
	source, err := platformMCPSkillsFS.ReadFile("platform_mcp_" + path)
	require.NoError(t, err)
	require.Equal(t, source, files["speakeasy/"+path])
	require.Equal(t, source, files["speakeasy-codex/"+path])
	for _, tool := range []string{"list_projects", "describe_analytics_catalog", "list_analytics_dimension_values", "run_analytics_query", "get_signal_definition", "find_signal_sessions"} {
		require.Contains(t, string(source), "`"+tool+"`")
	}
	require.Contains(t, string(source), "unknown census coverage")
	require.NotContains(t, strings.ToLower(string(source)), "gram")
	require.NotContains(t, string(source), "speakeasy-skill-feedback")
	require.Contains(t, string(source), "Do not ask for credentials")
}
