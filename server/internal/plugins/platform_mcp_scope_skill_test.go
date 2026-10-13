package plugins

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratePlatformMCPScopeWorkflow(t *testing.T) {
	t.Parallel()
	files, err := PublicPlatformMCPFiles("https://example.com", "17")
	require.NoError(t, err)
	const path = "skills/manage-mcp-sign-in-scopes/SKILL.md"
	source, err := platformMCPSkillsFS.ReadFile("platform_mcp_" + path)
	require.NoError(t, err)
	require.Equal(t, source, files["speakeasy/"+path])
	require.Equal(t, source, files["agent-plugins/speakeasy/"+path])
	for _, instruction := range []string{"`list_projects`", "`get_mcp`", "`get_mcp_scopes`", "`set_mcp_scope_pin`", "`confirmed: false`", "explicit confirmation", "`confirmed: true`", "`expected_scopes`", "`expected_version`", "`confirmation_version`", "resource URL and affected", "clearing an old", "concurrent change", "verified: false", "not a ceiling", "other** live MCP servers"} {
		require.Contains(t, string(source), instruction)
	}
	require.NotContains(t, strings.ToLower(string(source)), "gram")
	require.NotContains(t, string(source), "speakeasy-skill-feedback")
	require.Contains(t, string(source), "Never request or expose OAuth tokens")
}
