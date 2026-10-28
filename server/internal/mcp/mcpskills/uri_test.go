package mcpskills_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
)

func TestSkillURI(t *testing.T) {
	t.Parallel()

	require.Equal(t, "skill://git-workflow/SKILL.md", mcpskills.SkillURI("git-workflow"))
}

func TestParseSkillURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		uri      string
		wantName string
		wantOK   bool
	}{
		{name: "single segment", uri: "skill://git-workflow/SKILL.md", wantName: "git-workflow", wantOK: true},
		{name: "digits and hyphens", uri: "skill://pdf2-processing/SKILL.md", wantName: "pdf2-processing", wantOK: true},
		{name: "nested path", uri: "skill://acme/billing/refunds/SKILL.md", wantName: "", wantOK: false},
		{name: "supporting file", uri: "skill://git-workflow/references/FORMS.md", wantName: "", wantOK: false},
		{name: "directory", uri: "skill://git-workflow", wantName: "", wantOK: false},
		{name: "trailing slash", uri: "skill://git-workflow/SKILL.md/", wantName: "", wantOK: false},
		{name: "lowercase file", uri: "skill://git-workflow/skill.md", wantName: "", wantOK: false},
		{name: "other scheme", uri: "file://git-workflow/SKILL.md", wantName: "", wantOK: false},
		{name: "empty name", uri: "skill:///SKILL.md", wantName: "", wantOK: false},
		{name: "uppercase name", uri: "skill://Git-Workflow/SKILL.md", wantName: "", wantOK: false},
		{name: "leading hyphen", uri: "skill://-git/SKILL.md", wantName: "", wantOK: false},
		{name: "consecutive hyphens", uri: "skill://git--workflow/SKILL.md", wantName: "", wantOK: false},
		{name: "empty", uri: "", wantName: "", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			name, ok := mcpskills.ParseSkillURI(tc.uri)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.wantName, name)
		})
	}
}

func TestParseSkillURIRoundTrip(t *testing.T) {
	t.Parallel()

	name, ok := mcpskills.ParseSkillURI(mcpskills.SkillURI("release-notes"))
	require.True(t, ok)
	require.Equal(t, "release-notes", name)
}
