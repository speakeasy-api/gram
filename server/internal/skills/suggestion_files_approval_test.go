package skills_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/skilldiff"
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

// mixedSuggestion is a suggestion whose first change edits SKILL.md and whose
// second edits references/rollback.md.
type mixedSuggestion struct {
	skill          *gen.RecordSkillResult
	suggestionID   uuid.UUID
	manifestChange uuid.UUID
	fileChange     uuid.UUID
	manifestEdit   string
}

func createMixedSuggestion(t *testing.T, ctx context.Context, ti *testInstance, name string) mixedSuggestion {
	t.Helper()

	created, err := ti.service.Create(ctx, &gen.CreatePayload{
		Content:          skillManifest(name, "Runbook.", twoChangeBody),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	seedVersionFiles(t, ti, uuid.MustParse(created.Skill.ID), uuid.MustParse(created.Version.ID), []skilltree.File{
		{Path: "references/rollback.md", Content: []byte(rollbackReference), ContentType: ""},
	})

	manifestEdit := skillManifest(name, "Runbook.", strings.Replace(twoChangeBody, "Close the window.", "Close the window and record the duration.", 1))
	suggestion := createSuggestion(t, ti, created, manifestEdit, "record the duration")
	referenceDiff, err := skilldiff.UnifiedFile("references/rollback.md", rollbackReference,
		strings.Replace(rollbackReference, "Notify the channel.", "Notify the channel and the on-call.", 1))
	require.NoError(t, err)
	_, err = ti.repo.CreateSkillEditSuggestionChange(ctx, repo.CreateSkillEditSuggestionChangeParams{
		ProposedDiff: referenceDiff,
		Rationale:    "page the on-call on rollback",
		Position:     1,
		Path:         conv.ToPGText("references/rollback.md"),
		ProjectID:    ti.projectID,
		SuggestionID: suggestion.ID,
	})
	require.NoError(t, err)

	ids := suggestionChangeIDs(t, ti, suggestion.ID)
	require.Len(t, ids, 2)
	return mixedSuggestion{skill: created, suggestionID: suggestion.ID, manifestChange: ids[0], fileChange: ids[1], manifestEdit: manifestEdit}
}

func explicitFiles() []*types.SkillFileInput {
	return []*types.SkillFileInput{{Path: "references/notes.md", Content: []byte("Replacement notes.\n"), ContentType: nil}}
}

func TestSkillsApproveSuggestionRejectsExplicitFilesWithSupportingFileChanges(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	tests := []struct {
		name      string
		slug      string
		changeIDs func(mixedSuggestion) []string
	}{
		{name: "whole suggestion", slug: "approve-files-conflict-all", changeIDs: func(mixedSuggestion) []string { return nil }},
		{name: "selected file change", slug: "approve-files-conflict-file", changeIDs: func(f mixedSuggestion) []string { return []string{f.fileChange.String()} }},
		{name: "both changes selected", slug: "approve-files-conflict-both", changeIDs: func(f mixedSuggestion) []string {
			return []string{f.manifestChange.String(), f.fileChange.String()}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture := createMixedSuggestion(t, ctx, ti, tc.slug)

			_, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
				ID: fixture.suggestionID.String(), Content: nil, ChangeIds: tc.changeIDs(fixture), Files: explicitFiles(),
				SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
			})
			requireOopsCode(t, err, oops.CodeBadRequest)
			require.ErrorContains(t, err, "references/rollback.md")
			require.ErrorContains(t, err, "approve without files")
			require.ErrorContains(t, err, "change_ids")

			// Nothing was recorded and the suggestion is still open with both changes.
			require.Len(t, suggestionChangeIDs(t, ti, fixture.suggestionID), 2)
		})
	}
}

func TestSkillsApproveSuggestionAcceptsExplicitFilesWithOnlySkillMDChanges(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	fixture := createMixedSuggestion(t, ctx, ti, "approve-files-manifest-only")

	approved, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: fixture.suggestionID.String(), Content: nil, ChangeIds: []string{fixture.manifestChange.String()}, Files: explicitFiles(),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	// The explicit file set drops references/rollback.md, so the replay retires
	// the remaining change to it and nothing is left to propose.
	require.Equal(t, "applied", approved.Outcome)
	require.Equal(t, fixture.manifestEdit, approved.Version.Content)
	require.Equal(t, map[string][]byte{"references/notes.md": []byte("Replacement notes.\n")},
		versionFiles(t, ti, uuid.MustParse(fixture.skill.Skill.ID), approved.Version.ID))
}

func TestSkillsApproveSuggestionAcceptsExplicitFilesWithEditedContent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	fixture := createMixedSuggestion(t, ctx, ti, "approve-files-edited")
	edited := skillManifest(fixture.skill.Skill.Name, "Runbook.", "Reviewer rewrite.")

	approved, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: fixture.suggestionID.String(), Content: &edited, ChangeIds: nil, Files: explicitFiles(),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "applied", approved.Outcome)
	require.Equal(t, edited, approved.Version.Content)
	require.Equal(t, map[string][]byte{"references/notes.md": []byte("Replacement notes.\n")},
		versionFiles(t, ti, uuid.MustParse(fixture.skill.Skill.ID), approved.Version.ID))
}

func TestSkillsApproveSuggestionWithoutFilesAppliesSupportingFileChanges(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	fixture := createMixedSuggestion(t, ctx, ti, "approve-files-omitted")

	approved, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: fixture.suggestionID.String(), Content: nil, ChangeIds: nil, Files: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "applied", approved.Outcome)
	require.Equal(t, fixture.manifestEdit, approved.Version.Content)
	require.Equal(t, map[string][]byte{
		"references/rollback.md": []byte(strings.Replace(rollbackReference, "Notify the channel.", "Notify the channel and the on-call.", 1)),
	}, versionFiles(t, ti, uuid.MustParse(fixture.skill.Skill.ID), approved.Version.ID))
}
