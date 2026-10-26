package plugins

import (
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
	for _, instruction := range []string{"`list_projects`", "`get_internal_session_issuer`", "organization-scoped", "read-only", "IDP and SSO", "There is no issuer mutation", "company sign-in to Speakeasy", "registering the URI alone changes nothing", "`verified_domain_required`", "**Single sign-on**", "This workflow has no domain tool", "vendor trust is a separate step", "Finish setting up a new key", "Replace compromised key", "Rotate key", "Activate new key", "There is no key\nmutation tool", "`can_enable: true`", "false or missing", "same organization", "the service accepts every\nidentity provider", "the organization's Speakeasy issuer when the sign-in trusts none", "restoring the default needs only", "This workflow cannot choose Speakeasy", "does not prove live user access", "Never request or expose API keys"} {
		require.Contains(t, string(source), instruction)
	}
	require.NotContains(t, string(source), "`get_enterprise_issuer`")
	require.NotContains(t, string(source), "`enable_enterprise_issuer`")
	require.NotRegexp(t, `(?i)\bgram\b`, string(source))
	require.NotContains(t, string(source), "speakeasy-skill-feedback")
}
