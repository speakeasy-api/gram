package mcpskills_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
)

func TestSkillURI(t *testing.T) {
	t.Parallel()

	require.Equal(t, "skill://default/git-workflow/SKILL.md", mcpskills.SkillURI("default", "git-workflow"))
}

func TestParseSkillURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		uri      string
		wantSlug string
		wantName string
		wantOK   bool
	}{
		{name: "project and name", uri: "skill://default/git-workflow/SKILL.md", wantSlug: "default", wantName: "git-workflow", wantOK: true},
		{name: "digits and hyphens", uri: "skill://team_2-x/pdf2-processing/SKILL.md", wantSlug: "team_2-x", wantName: "pdf2-processing", wantOK: true},
		{name: "bare name", uri: "skill://git-workflow/SKILL.md", wantSlug: "", wantName: "", wantOK: false},
		{name: "nested path", uri: "skill://acme/billing/refunds/SKILL.md", wantSlug: "", wantName: "", wantOK: false},
		{name: "supporting file", uri: "skill://default/git-workflow/references/FORMS.md", wantSlug: "", wantName: "", wantOK: false},
		{name: "directory", uri: "skill://default/git-workflow", wantSlug: "", wantName: "", wantOK: false},
		{name: "trailing slash", uri: "skill://default/git-workflow/SKILL.md/", wantSlug: "", wantName: "", wantOK: false},
		{name: "lowercase file", uri: "skill://default/git-workflow/skill.md", wantSlug: "", wantName: "", wantOK: false},
		{name: "other scheme", uri: "file://default/git-workflow/SKILL.md", wantSlug: "", wantName: "", wantOK: false},
		{name: "empty slug", uri: "skill:///git-workflow/SKILL.md", wantSlug: "", wantName: "", wantOK: false},
		{name: "empty name", uri: "skill://default//SKILL.md", wantSlug: "", wantName: "", wantOK: false},
		{name: "uppercase slug", uri: "skill://Default/git-workflow/SKILL.md", wantSlug: "", wantName: "", wantOK: false},
		{name: "uppercase name", uri: "skill://default/Git-Workflow/SKILL.md", wantSlug: "", wantName: "", wantOK: false},
		{name: "leading hyphen", uri: "skill://default/-git/SKILL.md", wantSlug: "", wantName: "", wantOK: false},
		{name: "consecutive hyphens", uri: "skill://default/git--workflow/SKILL.md", wantSlug: "", wantName: "", wantOK: false},
		{name: "colon qualifier", uri: "skill://default:git-workflow/SKILL.md", wantSlug: "", wantName: "", wantOK: false},
		{name: "empty", uri: "", wantSlug: "", wantName: "", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			slug, name, ok := mcpskills.ParseSkillURI(tc.uri)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.wantSlug, slug)
			require.Equal(t, tc.wantName, name)
		})
	}
}

func TestParseSkillURIRoundTrip(t *testing.T) {
	t.Parallel()

	slug, name, ok := mcpskills.ParseSkillURI(mcpskills.SkillURI("default", "release-notes"))
	require.True(t, ok)
	require.Equal(t, "default", slug)
	require.Equal(t, "release-notes", name)
}
