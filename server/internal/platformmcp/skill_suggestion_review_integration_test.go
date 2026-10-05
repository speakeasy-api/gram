package platformmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/skilldiff"
)

// seedSkillSuggestion proposes one change to a skill's current version, the
// shape the suggestion generator writes.
func seedSkillSuggestion(t *testing.T, ctx context.Context, fixture *skillsVerticalFixture, skillID, baseVersionID, baseContent, proposedContent string) (suggestionID, changeID string) {
	t.Helper()

	queries := skillsrepo.New(fixture.conn)
	diff, err := skilldiff.Unified(baseContent, proposedContent)
	require.NoError(t, err)
	suggestion, err := queries.CreateSkillEditSuggestion(ctx, skillsrepo.CreateSkillEditSuggestionParams{
		Rationale: "Agents need an escalation step.", ScoredSessionCount: 1,
		BaseVersionID: uuid.MustParse(baseVersionID), ProjectID: fixture.project.ID, SkillID: uuid.MustParse(skillID),
	})
	require.NoError(t, err)
	change, err := queries.CreateSkillEditSuggestionChange(ctx, skillsrepo.CreateSkillEditSuggestionChangeParams{
		ProposedDiff: diff, Rationale: suggestion.Rationale, Position: 0,
		ProjectID: fixture.project.ID, SuggestionID: suggestion.ID,
	})
	require.NoError(t, err)
	return suggestion.ID.String(), change.ID.String()
}

func TestPlatformMCPApproveSkillSuggestionRecordsTheVersionAndClosesTheSuggestion(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newSkillsVerticalFixture(t, ctx, "platform_mcp_skill_suggestion_approve", skillsVerticalOptions{capabilityEnabled: true, grantSkillRead: true, grantSkillWrite: true})
	base := skillsFixtureManifest("suggested-skill", "Receives a suggestion.", "Body.")
	proposed := skillsFixtureManifest("suggested-skill", "Receives a suggestion.", "Body with an escalation step.")
	created := callSkillsTool[SkillAuthoringResult](t, ctx, fixture.session, "create_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"content":      base,
	})
	suggestionID, changeID := seedSkillSuggestion(t, ctx, fixture, created.Skill.ID, created.Version.ID, base, proposed)

	unconfirmed := callSkillsRefusal(t, ctx, fixture.session, "approve_skill_suggestion", map[string]any{
		"project_slug":  fixture.project.Slug,
		"suggestion_id": suggestionID,
		"change_ids":    []string{changeID},
		"confirmed":     false,
	})
	require.Equal(t, "confirmation_required", unconfirmed.Code)

	approved := callSkillsTool[ApproveSkillSuggestionOutput](t, ctx, fixture.session, "approve_skill_suggestion", map[string]any{
		"project_slug":  fixture.project.Slug,
		"suggestion_id": suggestionID,
		"change_ids":    []string{changeID},
		"confirmed":     true,
	})
	require.Equal(t, SkillSuggestionApplied, approved.Outcome)
	require.NotNil(t, approved.Version)
	require.NotEqual(t, created.Version.ID, approved.Version.ID)
	require.Equal(t, approved.Version.ID, approved.Skill.LatestVersionID)

	current := callSkillsTool[GetSkillOutput](t, ctx, fixture.session, "get_skill", map[string]any{
		"project_slug":    fixture.project.Slug,
		"skill_id":        created.Skill.ID,
		"include_content": true,
	})
	require.Equal(t, approved.Version.ID, current.LatestVersion.ID)
	require.Equal(t, proposed, current.LatestVersion.Content)

	open := callSkillsTool[ListSkillSuggestionsOutput](t, ctx, fixture.session, "list_skill_suggestions", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     created.Skill.ID,
	})
	require.Empty(t, open.Suggestions, "an approved suggestion is no longer open")

	repeat := callSkillsRefusal(t, ctx, fixture.session, "approve_skill_suggestion", map[string]any{
		"project_slug":  fixture.project.Slug,
		"suggestion_id": suggestionID,
		"change_ids":    []string{changeID},
		"confirmed":     true,
	})
	require.Equal(t, "conflict", repeat.Code, "a retry never records a second version")
}

func TestPlatformMCPDismissSkillSuggestionLeavesTheSkillUnchanged(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newSkillsVerticalFixture(t, ctx, "platform_mcp_skill_suggestion_dismiss", skillsVerticalOptions{capabilityEnabled: true, grantSkillRead: true, grantSkillWrite: true})
	base := skillsFixtureManifest("dismissed-skill", "Receives a suggestion.", "Body.")
	created := callSkillsTool[SkillAuthoringResult](t, ctx, fixture.session, "create_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"content":      base,
	})
	suggestionID, _ := seedSkillSuggestion(t, ctx, fixture, created.Skill.ID, created.Version.ID, base, skillsFixtureManifest("dismissed-skill", "Receives a suggestion.", "Unwanted body."))

	for range 2 {
		dismissed := callSkillsTool[DismissSkillSuggestionOutput](t, ctx, fixture.session, "dismiss_skill_suggestion", map[string]any{
			"project_slug":  fixture.project.Slug,
			"suggestion_id": suggestionID,
			"confirmed":     true,
		})
		require.Equal(t, "dismissed", dismissed.Suggestion.Status, "a repeat dismissal is a no-op")
	}

	current := callSkillsTool[GetSkillOutput](t, ctx, fixture.session, "get_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     created.Skill.ID,
	})
	require.Equal(t, created.Version.ID, current.LatestVersion.ID)
}

func TestPlatformMCPSkillSuggestionReviewRequiresSkillWrite(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newSkillsVerticalFixture(t, ctx, "platform_mcp_skill_suggestion_reader", skillsVerticalOptions{capabilityEnabled: true, grantSkillRead: true})

	for _, call := range []struct {
		name      string
		arguments map[string]any
	}{
		{name: "approve_skill_suggestion", arguments: map[string]any{"project_slug": fixture.project.Slug, "suggestion_id": uuid.NewString(), "change_ids": []string{uuid.NewString()}, "confirmed": true}},
		{name: "dismiss_skill_suggestion", arguments: map[string]any{"project_slug": fixture.project.Slug, "suggestion_id": uuid.NewString(), "confirmed": true}},
	} {
		refusal := callSkillsRefusal(t, ctx, fixture.session, call.name, call.arguments)
		require.Equal(t, "forbidden", refusal.Code, call.name)
	}
}
