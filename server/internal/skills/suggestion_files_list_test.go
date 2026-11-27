package skills_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/skilldiff"
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

func TestSkillsListSuggestionsLoadsEachBaseVersionsFilesAcrossSkills(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	editedReference := strings.Replace(rollbackReference, "Notify the channel.", "Notify the channel and the on-call.", 1)
	referenceDiff, err := skilldiff.UnifiedFile("references/rollback.md", rollbackReference, editedReference)
	require.NoError(t, err)

	// Both suggestions carry the same reference edit. Only the first skill's
	// base version still has the text it edits, so a page that handed either
	// suggestion the other's base files would report the wrong applicability.
	seed := func(name, reference string) string {
		created := createSkill(t, ctx, ti, name, "Base.")
		skillID := uuid.MustParse(created.Skill.ID)
		seedVersionFiles(t, ti, skillID, uuid.MustParse(created.Version.ID), []skilltree.File{
			{Path: "references/rollback.md", Content: []byte(reference), ContentType: ""},
		})
		suggestion := createSuggestion(t, ti, created, created.Version.Content+"\nRead references/rollback.md.\n", "point at the runbook")
		_, err := ti.repo.CreateSkillEditSuggestionChange(ctx, repo.CreateSkillEditSuggestionChangeParams{
			ProposedDiff: referenceDiff,
			Rationale:    "page the on-call on rollback",
			Position:     1,
			Path:         conv.ToPGText("references/rollback.md"),
			ProjectID:    ti.projectID,
			SuggestionID: suggestion.ID,
		})
		require.NoError(t, err)
		return suggestion.ID.String()
	}
	applies := seed("list-files-applies", rollbackReference)
	conflicts := seed("list-files-conflicts", "# Rollback\n\nPage the release manager.\n")

	listed, err := ti.service.ListSuggestions(ctx, &gen.ListSuggestionsPayload{
		SkillID: nil, Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	byID := map[string]*types.SkillEditSuggestion{}
	for _, suggestion := range listed.Suggestions {
		byID[suggestion.ID] = suggestion
	}
	require.Contains(t, byID, applies)
	require.Contains(t, byID, conflicts)
	require.Len(t, byID[applies].Changes, 2)
	require.Equal(t, "references/rollback.md", byID[applies].Changes[1].Path)
	require.True(t, byID[applies].Changes[1].AppliesCleanly)
	require.Len(t, byID[conflicts].Changes, 2)
	require.Equal(t, "references/rollback.md", byID[conflicts].Changes[1].Path)
	require.False(t, byID[conflicts].Changes[1].AppliesCleanly)
}

func TestLoadSkillVersionFilesForVersionsGroupsByVersion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	first := createSkill(t, ctx, ti, "batch-files-first", "Base.")
	second := createSkill(t, ctx, ti, "batch-files-second", "Base.")
	empty := createSkill(t, ctx, ti, "batch-files-empty", "Base.")
	firstVersion := uuid.MustParse(first.Version.ID)
	secondVersion := uuid.MustParse(second.Version.ID)
	seedVersionFiles(t, ti, uuid.MustParse(first.Skill.ID), firstVersion, []skilltree.File{
		{Path: "references/b.md", Content: []byte("b\n"), ContentType: ""},
		{Path: "references/a.md", Content: []byte("a\n"), ContentType: ""},
	})
	seedVersionFiles(t, ti, uuid.MustParse(second.Skill.ID), secondVersion, []skilltree.File{
		{Path: "references/c.md", Content: []byte("c\n"), ContentType: ""},
	})

	files, err := skills.LoadSkillVersionFilesForVersions(ctx, ti.repo, ti.projectID, []uuid.UUID{firstVersion, secondVersion, uuid.MustParse(empty.Version.ID)})
	require.NoError(t, err)
	require.Len(t, files, 2, "a version without supporting files has no key")

	paths := func(version uuid.UUID) []string {
		var out []string
		for _, file := range files[version] {
			out = append(out, file.Path)
		}
		return out
	}
	require.Equal(t, []string{"references/a.md", "references/b.md"}, paths(firstVersion))
	require.Equal(t, []string{"references/c.md"}, paths(secondVersion))

	none, err := skills.LoadSkillVersionFilesForVersions(ctx, ti.repo, ti.projectID, nil)
	require.NoError(t, err)
	require.Empty(t, none)
}
