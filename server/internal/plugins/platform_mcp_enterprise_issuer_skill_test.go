package plugins

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratePlatformMCPEnterpriseIssuerWorkflow(t *testing.T) {
	t.Parallel()
	files, err := PublicPlatformMCPFiles("https://example.com", "17")
	require.NoError(t, err)
	const path = "skills/enable-enterprise-issuer/SKILL.md"
	source, err := platformMCPSkillsFS.ReadFile("platform_mcp_" + path)
	require.NoError(t, err)
	require.Equal(t, source, files["speakeasy/"+path])
	require.Equal(t, source, files["agent-plugins/speakeasy/"+path])
	for _, instruction := range []string{"`list_projects`", "`get_internal_session_issuer`", "organization-scoped", "read-only", "IDP and SSO", "There is no issuer mutation", "company sign-in to Speakeasy", "passwords without MFA don't qualify", "`verified_domain_required`", "vendor trust is a separate step", "Recovery is not available", "`can_enable: true`", "false or missing", "same organization", "does not prove live user access", "Never request or expose API keys"} {
		require.Contains(t, string(source), instruction)
	}
	require.NotContains(t, string(source), "`get_enterprise_issuer`")
	require.NotContains(t, string(source), "`enable_enterprise_issuer`")
	require.NotContains(t, strings.ToLower(string(source)), "gram")
	require.NotContains(t, string(source), "speakeasy-skill-feedback")
}
