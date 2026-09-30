package plugins

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratePlatformMCPPackageEmitsPrivateAccessTrafficCheck(t *testing.T) {
	t.Parallel()

	files, err := PublicPlatformMCPFiles("https://example.com", "17")
	require.NoError(t, err)
	const skillPath = "skills/configure-private-mcp-access/SKILL.md"
	source, err := platformMCPSkillsFS.ReadFile("platform_mcp_" + skillPath)
	require.NoError(t, err)
	claudeSkill := files["speakeasy/"+skillPath]
	require.Equal(t, source, claudeSkill)
	require.Equal(t, claudeSkill, files["agent-plugins/speakeasy/"+skillPath])
	require.Contains(t, string(claudeSkill), "`get_mcp_network_traffic`")
	require.Contains(t, string(claudeSkill), "zero does not prove a route is unused")
}
