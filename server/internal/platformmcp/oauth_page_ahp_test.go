package platformmcp

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOAuthPageAHPConsent(t *testing.T) {
	t.Parallel()
	projects := []AHPProjectOption{
		{ID: "project-first", Name: "First project", Slug: "first"},
		{ID: "project-second", Name: "<script>alert(1)</script>", Slug: "second"},
	}
	for _, tc := range []struct {
		name           string
		selected       string
		projects       []AHPProjectOption
		selectedOption string
	}{
		{"explicit selection", "project-second", projects, `value="project-second" selected`},
		{"no default", "", projects, `value="" disabled selected`},
		{"ineligible default", "project-other", projects, `value="" disabled selected`},
		{"no eligible projects", "", nil, `value="" disabled selected`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var page bytes.Buffer
			require.NoError(t, oauthPageTemplate.Execute(&page, oauthPageData{
				Kind: "connect", AHP: true, Projects: tc.projects, SelectedProjectID: tc.selected,
				ClientName: "<script>client</script>", State: "state-value", CSRFToken: "csrf-value", ScriptNonce: "nonce-value",
			}))
			rendered := page.String()
			require.Contains(t, rendered, "hook events and content, and receive hook configuration")
			require.NotContains(t, rendered, "such as adding MCP servers")
			require.Contains(t, rendered, tc.selectedOption)
			require.Regexp(t, `(?s)<select[^>]*name="project_id"[^>]*form="ahp-approve"[^>]*required`, rendered)
			require.Contains(t, rendered, `id="ahp-approve"`)
			require.Contains(t, rendered, `name="csrf_token" value="csrf-value"`)
			require.Contains(t, rendered, `name="state" value="state-value"`)
			require.Contains(t, rendered, `<script nonce="nonce-value">`)
			require.Contains(t, rendered, "&lt;script&gt;client&lt;/script&gt;")
			require.NotRegexp(t, `(?s)<button[^>]*value="deny"[^>]*\sdisabled(?:\s|>)`, rendered)
			if len(tc.projects) == 0 {
				require.Contains(t, rendered, "No eligible projects are available.")
				require.Regexp(t, `(?s)<button[^>]*value="approve"\s+disabled`, rendered)
			} else {
				require.Contains(t, rendered, `value="project-first"`)
				require.Contains(t, rendered, `value="project-second"`)
				require.Contains(t, rendered, "&lt;script&gt;alert(1)&lt;/script&gt;")
				require.NotRegexp(t, `(?s)<button[^>]*value="approve"\s+disabled`, rendered)
				require.NotContains(t, rendered, `value="project-first" selected`)
			}
		})
	}
}

func TestOAuthPageMCPConsentUnchanged(t *testing.T) {
	t.Parallel()
	var page bytes.Buffer
	require.NoError(t, oauthPageTemplate.Execute(&page, oauthPageData{Kind: "connect"}))
	require.Contains(t, page.String(), "such as adding MCP servers")
	require.NotContains(t, page.String(), `name="project_id"`)
	require.NotContains(t, page.String(), "hook events and content")
	require.NotRegexp(t, `(?s)<button[^>]*value="approve"\s+disabled`, page.String())
}
