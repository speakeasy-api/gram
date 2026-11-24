package skills_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/skilldiff"
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
	"github.com/speakeasy-api/gram/server/internal/skills/suggest"
)

func TestSuggestionEngineDetectsANoOpOnASkillWithSupportingFiles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	name := "engine-noop-files"
	created, err := ti.service.Create(ctx, &gen.CreatePayload{
		Content: "---\nname: " + name + "\ndescription: Base.\n---\n\nRead references/rollback.md.\n",
		Files: []*types.SkillFileInput{
			{Path: "references/rollback.md", Content: []byte(rollbackReference), ContentType: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, name, now)
	createSuggestionFeedback(t, ti, name, 5)

	// Reordering the frontmatter is canonically no change. The base version is
	// identified by its whole tree, so only a whole-tree comparison sees that.
	equivalent := "---\ndescription: Base.\nname: " + name + "\n---\n\nRead references/rollback.md.\n"
	completion := &suggestionCompletionStub{responses: []string{proposeManifest(created.Version.Content, equivalent, "No real change.", 1)}}
	engine := newSuggestionEngine(t, ti, suggest.DefaultConfig(), &suggestionInsightsStub{}, completion)

	result, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID), Now: now})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultDeclined, result.Kind)
	require.Len(t, completion.Requests(), 1, "a no-op is a terminal decline, not a retry")
}

func TestSuggestionEngineProposesAnEditToAReferenceFile(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "engine-reference", "Base.")
	skillID := uuid.MustParse(created.Skill.ID)
	seedVersionFiles(t, ti, skillID, uuid.MustParse(created.Version.ID), []skilltree.File{
		{Path: "references/rollback.md", Content: []byte(rollbackReference), ContentType: ""},
		{Path: "assets/logo.png", Content: logoPNG, ContentType: "image/png"},
	})
	now := time.Now().UTC()
	activateSuggestionSkill(t, ctx, ti, created.Skill.Name, now)
	createSuggestionFeedback(t, ti, created.Skill.Name, 5)

	response, err := json.Marshal(map[string]any{
		"decision": "propose",
		"changes": []map[string]any{{
			"path": "references/rollback.md", "find": "Notify the channel.", "replace": "Notify the channel and the on-call.",
			"rationale": "Rollbacks go unnoticed by the on-call.", "evidence": []int{1, 2},
		}},
		"rationale": "Rollbacks go unnoticed by the on-call.",
	})
	require.NoError(t, err)
	completion := &suggestionCompletionStub{responses: []string{string(response)}}
	config := suggest.DefaultConfig()
	config.ActivityWindow = 30 * 24 * time.Hour
	engine := newSuggestionEngine(t, ti, config, &suggestionInsightsStub{}, completion)

	result, err := engine.Run(ctx, suggest.RunInput{ProjectID: ti.projectID, SkillID: skillID, Now: now})
	require.NoError(t, err)
	require.Equal(t, suggest.ResultProposed, result.Kind)

	requests := completion.Requests()
	require.Len(t, requests, 1)
	require.Contains(t, requests[0].Prompt, `"supporting_files":[{"path":"references/rollback.md"`)
	require.Contains(t, requests[0].Prompt, `{"path":"assets/logo.png","reason":"binary"}`)

	changes := suggestionChanges(t, ctx, ti, result.SuggestionID.UUID)
	require.Len(t, changes, 1)
	require.Equal(t, "references/rollback.md", changes[0].Path.String)
	require.True(t, strings.HasPrefix(changes[0].ProposedDiff, "--- a/references/rollback.md\n+++ b/references/rollback.md\n"))
	require.Equal(t, int64(2), suggestionFeedbackCount(t, ctx, ti, result.SuggestionID.UUID))
}

const rollbackReference = "# Rollback\n\nRevert the deploy.\nNotify the channel.\n"

var logoPNG = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0}

// seedVersionFiles stores supporting files on an existing skill version.
func seedVersionFiles(t *testing.T, ti *testInstance, skillID, versionID uuid.UUID, files []skilltree.File) {
	t.Helper()

	entries, err := skilltree.Validate(files)
	require.NoError(t, err)
	for _, entry := range entries {
		require.NoError(t, ti.repo.CreateSkillVersionFile(t.Context(), repo.CreateSkillVersionFileParams{
			Path:           entry.Path,
			Content:        entry.Content,
			Sha256:         entry.SHA256,
			SizeBytes:      entry.Size,
			ContentType:    entry.ContentType,
			ProjectID:      ti.projectID,
			SkillID:        skillID,
			SkillVersionID: versionID,
		}))
	}
}

func versionFiles(t *testing.T, ti *testInstance, skillID uuid.UUID, versionID string) map[string][]byte {
	t.Helper()

	files, err := skills.LoadSkillVersionFiles(t.Context(), ti.repo, ti.projectID, skillID, uuid.MustParse(versionID))
	require.NoError(t, err)
	byPath := make(map[string][]byte, len(files))
	for _, file := range files {
		byPath[file.Path] = file.Content
	}
	return byPath
}

func TestSkillsApproveSuggestionEditsAReferenceFile(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created, err := ti.service.Create(ctx, &gen.CreatePayload{
		Content:          skillManifest("suggestion-reference-edit", "Runbook.", twoChangeBody),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	skillID := uuid.MustParse(created.Skill.ID)
	seedVersionFiles(t, ti, skillID, uuid.MustParse(created.Version.ID), []skilltree.File{
		{Path: "references/rollback.md", Content: []byte(rollbackReference), ContentType: ""},
		{Path: "assets/logo.png", Content: logoPNG, ContentType: "image/png"},
	})

	// The suggestion edits SKILL.md first and the reference second, so taking
	// the SKILL.md change alone makes recordVersion replay the reference change
	// onto the version it creates. That replay only succeeds if the new
	// version already carries the reference file.
	manifestEdit := skillManifest(created.Skill.Name, "Runbook.", strings.Replace(twoChangeBody, "Close the window.", "Close the window and record the duration.", 1))
	suggestion := createSuggestion(t, ti, created, manifestEdit, "record the duration")
	editedReference := strings.Replace(rollbackReference, "Notify the channel.", "Notify the channel and the on-call.", 1)
	referenceDiff, err := skilldiff.UnifiedFile("references/rollback.md", rollbackReference, editedReference)
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

	listed, err := ti.service.ListSuggestions(ctx, &gen.ListSuggestionsPayload{
		SkillID: &created.Skill.ID, Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, listed.Suggestions, 1)
	require.True(t, listed.Suggestions[0].AppliesCleanly)
	require.Len(t, listed.Suggestions[0].Changes, 2)
	require.Equal(t, skilltree.ManifestPath, listed.Suggestions[0].Changes[0].Path, "a change stored without a path edits SKILL.md")
	require.Equal(t, "references/rollback.md", listed.Suggestions[0].Changes[1].Path)
	require.True(t, listed.Suggestions[0].Changes[1].AppliesCleanly)
	require.Equal(t, manifestEdit, listed.Suggestions[0].ProposedContent)

	changeIDs := suggestionChangeIDs(t, ti, suggestion.ID)
	partial, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: suggestion.ID.String(), Content: nil, ChangeIds: []string{changeIDs[0].String()}, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "partially_applied", partial.Outcome)
	require.Equal(t, manifestEdit, partial.Version.Content)
	require.Equal(t, map[string][]byte{
		"assets/logo.png":        logoPNG,
		"references/rollback.md": []byte(rollbackReference),
	}, versionFiles(t, ti, skillID, partial.Version.ID), "taking a SKILL.md change carries the supporting files over unchanged")
	require.Equal(t, partial.Version.ID, partial.Suggestion.BaseVersionID)
	require.Len(t, partial.Suggestion.Changes, 1)
	require.Equal(t, "references/rollback.md", partial.Suggestion.Changes[0].Path)
	require.True(t, partial.Suggestion.Changes[0].AppliesCleanly)

	final, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: suggestion.ID.String(), Content: nil, ChangeIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "applied", final.Outcome)
	require.NotNil(t, final.Version)
	require.NotEqual(t, partial.Version.ID, final.Version.ID, "a supporting-file edit alone records a new version")
	require.Equal(t, manifestEdit, final.Version.Content)
	require.Equal(t, map[string][]byte{
		"assets/logo.png":        logoPNG,
		"references/rollback.md": []byte(editedReference),
	}, versionFiles(t, ti, skillID, final.Version.ID))
	require.Equal(t, string(skills.EditSuggestionStatusApproved), final.Suggestion.Status)
}

func TestSkillsApproveSuggestionRejectsAChangeToABinaryFile(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "suggestion-binary-edit", "Runbook.")
	skillID := uuid.MustParse(created.Skill.ID)
	seedVersionFiles(t, ti, skillID, uuid.MustParse(created.Version.ID), []skilltree.File{
		{Path: "assets/logo.png", Content: logoPNG, ContentType: "image/png"},
	})

	suggestion, err := ti.repo.CreateSkillEditSuggestion(ctx, repo.CreateSkillEditSuggestionParams{
		Rationale:          "edit the logo",
		ScoredSessionCount: 1,
		BaseVersionID:      uuid.MustParse(created.Version.ID),
		ProjectID:          ti.projectID,
		SkillID:            skillID,
	})
	require.NoError(t, err)
	diff, err := skilldiff.UnifiedFile("assets/logo.png", "PNG\n", "JPG\n")
	require.NoError(t, err)
	_, err = ti.repo.CreateSkillEditSuggestionChange(ctx, repo.CreateSkillEditSuggestionChangeParams{
		ProposedDiff: diff,
		Rationale:    "edit the logo",
		Position:     0,
		Path:         conv.ToPGText("assets/logo.png"),
		ProjectID:    ti.projectID,
		SuggestionID: suggestion.ID,
	})
	require.NoError(t, err)

	_, err = ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: suggestion.ID.String(), Content: nil, ChangeIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Equal(t, map[string][]byte{"assets/logo.png": logoPNG}, versionFiles(t, ti, skillID, created.Version.ID))
}
