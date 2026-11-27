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

func TestSkillFileURI(t *testing.T) {
	t.Parallel()

	require.Equal(t, "skill://default/pdf/references/FORMS.md", mcpskills.SkillFileURI("default", "pdf", "references/FORMS.md"))
	require.Equal(t, "skill://default/pdf/assets/my%20logo%23v2.png", mcpskills.SkillFileURI("default", "pdf", "assets/my logo#v2.png"))
}

func TestParseSkillResourceURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		uri      string
		wantSlug string
		wantName string
		wantPath string
		wantOK   bool
	}{
		{name: "manifest", uri: "skill://default/pdf/SKILL.md", wantSlug: "default", wantName: "pdf", wantPath: "", wantOK: true},
		{name: "supporting file", uri: "skill://default/pdf/references/FORMS.md", wantSlug: "default", wantName: "pdf", wantPath: "references/FORMS.md", wantOK: true},
		{name: "top-level file", uri: "skill://default/pdf/LICENSE.txt", wantSlug: "default", wantName: "pdf", wantPath: "LICENSE.txt", wantOK: true},
		{name: "escaped segment", uri: "skill://default/pdf/assets/my%20logo.png", wantSlug: "default", wantName: "pdf", wantPath: "assets/my logo.png", wantOK: true},
		{name: "unnecessary escape", uri: "skill://default/pdf/scripts/%72un.py", wantOK: false},
		{name: "escaped slash", uri: "skill://default/pdf/scripts%2Frun.py", wantOK: false},
		{name: "parent segment", uri: "skill://default/pdf/../other/SKILL.md", wantOK: false},
		{name: "dot segment", uri: "skill://default/pdf/./SKILL.md", wantOK: false},
		{name: "empty segment", uri: "skill://default/pdf/scripts//run.py", wantOK: false},
		{name: "lowercase manifest", uri: "skill://default/pdf/skill.md", wantOK: false},
		{name: "trailing slash", uri: "skill://default/pdf/scripts/", wantOK: false},
		{name: "directory", uri: "skill://default/pdf", wantOK: false},
		{name: "invalid name", uri: "skill://default/PDF/references/FORMS.md", wantOK: false},
		{name: "invalid escape", uri: "skill://default/pdf/a%zz", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			slug, name, filePath, ok := mcpskills.ParseSkillResourceURI(tc.uri)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.wantSlug, slug)
			require.Equal(t, tc.wantName, name)
			require.Equal(t, tc.wantPath, filePath)
		})
	}
}

func TestParseSkillResourceURIRoundTrip(t *testing.T) {
	t.Parallel()

	for _, filePath := range []string{"references/REFERENCE.md", "assets/a b/c#d?.png", "scripts/run.py"} {
		t.Run(filePath, func(t *testing.T) {
			t.Parallel()

			slug, name, parsed, ok := mcpskills.ParseSkillResourceURI(mcpskills.SkillFileURI("default", "pdf", filePath))
			require.True(t, ok)
			require.Equal(t, "default", slug)
			require.Equal(t, "pdf", name)
			require.Equal(t, filePath, parsed)
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
