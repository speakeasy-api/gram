package plugins

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratePlatformMCPPackageEmitsSignalAuthoringWorkflow(t *testing.T) {
	t.Parallel()

	files, err := PublicPlatformMCPFiles("https://example.com", "17")
	require.NoError(t, err)

	const path = "skills/author-signals-and-sensors/SKILL.md"

	source, err := platformMCPSkillsFS.ReadFile("platform_mcp_" + path)
	require.NoError(t, err)
	require.Equal(t, source, files["speakeasy/"+path])
	require.Equal(t, source, files["agent-plugins/speakeasy/"+path])

	for _, tool := range []string{"list_projects", "find_signals", "find_sensors", "get_sensor", "create_sensor", "create_signal", "update_sensor", "update_signal", "preview_sensor_match"} {
		require.Contains(t, string(source), "`"+tool+"`")
	}
	require.Contains(t, string(source), "explicit confirmation")
	remaining := string(source)
	for _, guardrail := range []string{"## Preview and confirm", "`confirmed:false`", "explicit confirmation", "identical proposal", "`confirmed:true`", "`expected_version`", "`preview_token`", "`idempotency_key`", "Keep that key and all inputs unchanged", "A version conflict requires another read, preview, and", "confirmation;", "## Verify", "`snapshot_scope: verification_unavailable`"} {
		_, after, found := strings.Cut(remaining, guardrail)
		require.True(t, found, "missing or out-of-order guardrail: %s", guardrail)
		remaining = after
	}
	require.Contains(t, string(source), "target_available")
	require.NotContains(t, strings.ToLower(string(source)), "gram")
	require.NotContains(t, string(source), "speakeasy-skill-feedback")
	require.Contains(t, string(source), "Never ask for API keys")
}
