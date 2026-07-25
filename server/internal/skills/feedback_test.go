package skills_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func createSkillFeedback(t *testing.T, ti *testInstance, projectID uuid.UUID, skillName, note string) repo.SkillFeedback {
	t.Helper()

	feedback, err := ti.repo.CreateSkillFeedback(t.Context(), repo.CreateSkillFeedbackParams{
		ProjectID:      projectID,
		SkillID:        uuid.NullUUID{},
		SkillVersionID: uuid.NullUUID{},
		SkillName:      skillName,
		Source:         string(skills.FeedbackSourceDev),
		Outcome:        string(skills.FeedbackOutcomeHelped),
		Note:           pgtype.Text{String: note, Valid: note != ""},
		SessionID:      pgtype.Text{},
		UserID:         pgtype.Text{},
		UserEmail:      pgtype.Text{},
	})
	require.NoError(t, err)

	return feedback
}

func TestSkillFeedbackValues(t *testing.T) {
	t.Parallel()

	outcomes := []skills.FeedbackOutcome{
		skills.FeedbackOutcomeHelped,
		skills.FeedbackOutcomePartiallyHelped,
		skills.FeedbackOutcomeDidNotHelp,
		skills.FeedbackOutcomeMisleading,
		skills.FeedbackOutcomeHarmful,
	}
	for _, outcome := range outcomes {
		require.True(t, outcome.Valid())
	}
	require.False(t, skills.FeedbackOutcome("unknown").Valid())

	require.True(t, skills.FeedbackSourceDev.Valid())
	require.True(t, skills.FeedbackSourceAssistant.Valid())
	require.False(t, skills.FeedbackSource("unknown").Valid())

	require.Equal(t, skills.EditSuggestionStatusOpen, skills.EditSuggestionStatus("open"))
	require.Equal(t, skills.EditSuggestionStatusApproved, skills.EditSuggestionStatus("approved"))
	require.Equal(t, skills.EditSuggestionStatusDismissed, skills.EditSuggestionStatus("dismissed"))
	require.Equal(t, skills.EditSuggestionStatusSuperseded, skills.EditSuggestionStatus("superseded"))
}

func TestCreateSkillFeedbackUnresolvedNameRoundTrip(t *testing.T) {
	t.Parallel()

	_, ti := newTestService(t)
	feedback, err := ti.repo.CreateSkillFeedback(t.Context(), repo.CreateSkillFeedbackParams{
		ProjectID:      ti.projectID,
		SkillID:        uuid.NullUUID{},
		SkillVersionID: uuid.NullUUID{},
		SkillName:      "unresolved-skill",
		Source:         string(skills.FeedbackSourceAssistant),
		Outcome:        string(skills.FeedbackOutcomePartiallyHelped),
		Note:           pgtype.Text{String: "Useful, but incomplete", Valid: true},
		SessionID:      pgtype.Text{String: "session-1", Valid: true},
		UserID:         pgtype.Text{String: "user-1", Valid: true},
		UserEmail:      pgtype.Text{String: "user@example.test", Valid: true},
	})
	require.NoError(t, err)
	require.Equal(t, ti.projectID, feedback.ProjectID)
	require.False(t, feedback.SkillID.Valid)
	require.False(t, feedback.SkillVersionID.Valid)
	require.Equal(t, "unresolved-skill", feedback.SkillName)
	require.Equal(t, string(skills.FeedbackSourceAssistant), feedback.Source)
	require.Equal(t, string(skills.FeedbackOutcomePartiallyHelped), feedback.Outcome)
	require.Equal(t, "Useful, but incomplete", feedback.Note.String)
	require.Equal(t, "session-1", feedback.SessionID.String)
	require.Equal(t, "user-1", feedback.UserID.String)
	require.Equal(t, "user@example.test", feedback.UserEmail.String)
	require.False(t, feedback.ReviewedAt.Valid)
	require.True(t, feedback.CreatedAt.Valid)
}

func TestSkillFeedbackSameNameTenantIsolation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, otherProjectID := createProjectContext(t, ctx, ti)
	createSkillFeedback(t, ti, ti.projectID, "shared-name", "first project")
	createSkillFeedback(t, ti, otherProjectID, "shared-name", "second project")

	first, err := ti.repo.ListRecentSkillFeedback(t.Context(), repo.ListRecentSkillFeedbackParams{
		ProjectID: ti.projectID,
		SkillName: "shared-name",
		PageLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Equal(t, "first project", first[0].Note.String)

	second, err := ti.repo.ListRecentSkillFeedback(t.Context(), repo.ListRecentSkillFeedbackParams{
		ProjectID: otherProjectID,
		SkillName: "shared-name",
		PageLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, "second project", second[0].Note.String)
}

func TestCreateSkillFeedbackResolvedSkillVersionRoundTrip(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "resolved-skill", "Resolved skill")
	skillID := uuid.MustParse(created.Skill.ID)
	versionID := uuid.MustParse(created.Version.ID)

	feedback, err := ti.repo.CreateSkillFeedback(t.Context(), repo.CreateSkillFeedbackParams{
		ProjectID:      ti.projectID,
		SkillID:        uuid.NullUUID{UUID: skillID, Valid: true},
		SkillVersionID: uuid.NullUUID{UUID: versionID, Valid: true},
		SkillName:      created.Skill.Name,
		Source:         string(skills.FeedbackSourceDev),
		Outcome:        string(skills.FeedbackOutcomeDidNotHelp),
		Note:           pgtype.Text{},
		SessionID:      pgtype.Text{},
		UserID:         pgtype.Text{},
		UserEmail:      pgtype.Text{},
	})
	require.NoError(t, err)
	require.Equal(t, skillID, feedback.SkillID.UUID)
	require.Equal(t, versionID, feedback.SkillVersionID.UUID)
	require.False(t, feedback.Note.Valid)
	require.False(t, feedback.SessionID.Valid)
	require.False(t, feedback.UserID.Valid)
	require.False(t, feedback.UserEmail.Valid)

	_, otherProjectID := createProjectContext(t, ctx, ti)
	_, err = ti.repo.CreateSkillFeedback(t.Context(), repo.CreateSkillFeedbackParams{
		ProjectID:      otherProjectID,
		SkillID:        uuid.NullUUID{UUID: skillID, Valid: true},
		SkillVersionID: uuid.NullUUID{UUID: versionID, Valid: true},
		SkillName:      created.Skill.Name,
		Source:         string(skills.FeedbackSourceDev),
		Outcome:        string(skills.FeedbackOutcomeHarmful),
		Note:           pgtype.Text{},
		SessionID:      pgtype.Text{},
		UserID:         pgtype.Text{},
		UserEmail:      pgtype.Text{},
	})
	require.Error(t, err)
}

func TestListSkillFeedbackDeterministicAndBounded(t *testing.T) {
	t.Parallel()

	_, ti := newTestService(t)
	for _, note := range []string{"first", "second", "third", "fourth"} {
		createSkillFeedback(t, ti, ti.projectID, "ordered-skill", note)
	}
	createSkillFeedback(t, ti, ti.projectID, "other-skill", "excluded")

	recent, err := ti.repo.ListRecentSkillFeedback(t.Context(), repo.ListRecentSkillFeedbackParams{
		ProjectID: ti.projectID,
		SkillName: "ordered-skill",
		PageLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, recent, 4)
	recentAgain, err := ti.repo.ListRecentSkillFeedback(t.Context(), repo.ListRecentSkillFeedbackParams{
		ProjectID: ti.projectID,
		SkillName: "ordered-skill",
		PageLimit: 10,
	})
	require.NoError(t, err)
	require.Equal(t, recent, recentAgain)
	recentPage, err := ti.repo.ListRecentSkillFeedback(t.Context(), repo.ListRecentSkillFeedbackParams{
		ProjectID: ti.projectID,
		SkillName: "ordered-skill",
		PageLimit: 2,
	})
	require.NoError(t, err)
	require.Equal(t, recent[:2], recentPage)

	unreviewed, err := ti.repo.ListUnreviewedSkillFeedback(t.Context(), repo.ListUnreviewedSkillFeedbackParams{
		ProjectID: ti.projectID,
		SkillName: "ordered-skill",
		PageLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, unreviewed, 4)
	for i := range recent {
		require.Equal(t, recent[len(recent)-1-i].ID, unreviewed[i].ID)
	}
	unreviewedPage, err := ti.repo.ListUnreviewedSkillFeedback(t.Context(), repo.ListUnreviewedSkillFeedbackParams{
		ProjectID: ti.projectID,
		SkillName: "ordered-skill",
		PageLimit: 2,
	})
	require.NoError(t, err)
	require.Equal(t, unreviewed[:2], unreviewedPage)
}

func TestMarkSkillFeedbackReviewedSelectiveAndIdempotent(t *testing.T) {
	t.Parallel()

	_, ti := newTestService(t)
	selected := createSkillFeedback(t, ti, ti.projectID, "reviewed-skill", "selected")
	unselected := createSkillFeedback(t, ti, ti.projectID, "reviewed-skill", "unselected")
	otherName := createSkillFeedback(t, ti, ti.projectID, "other-skill", "other name")
	params := repo.MarkSkillFeedbackReviewedParams{
		ProjectID: ti.projectID,
		SkillName: "reviewed-skill",
		Ids:       []uuid.UUID{selected.ID, otherName.ID, uuid.New()},
	}

	marked, err := ti.repo.MarkSkillFeedbackReviewed(t.Context(), params)
	require.NoError(t, err)
	require.EqualValues(t, 1, marked)
	marked, err = ti.repo.MarkSkillFeedbackReviewed(t.Context(), params)
	require.NoError(t, err)
	require.Zero(t, marked)

	recent, err := ti.repo.ListRecentSkillFeedback(t.Context(), repo.ListRecentSkillFeedbackParams{
		ProjectID: ti.projectID,
		SkillName: "reviewed-skill",
		PageLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, recent, 2)
	for _, feedback := range recent {
		if feedback.ID == selected.ID {
			require.True(t, feedback.ReviewedAt.Valid)
		} else {
			require.Equal(t, unselected.ID, feedback.ID)
			require.False(t, feedback.ReviewedAt.Valid)
		}
	}

	unreviewed, err := ti.repo.ListUnreviewedSkillFeedback(t.Context(), repo.ListUnreviewedSkillFeedbackParams{
		ProjectID: ti.projectID,
		SkillName: "reviewed-skill",
		PageLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, unreviewed, 1)
	require.Equal(t, unselected.ID, unreviewed[0].ID)

	other, err := ti.repo.ListUnreviewedSkillFeedback(t.Context(), repo.ListUnreviewedSkillFeedbackParams{
		ProjectID: ti.projectID,
		SkillName: "other-skill",
		PageLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, other, 1)
	require.Equal(t, otherName.ID, other[0].ID)
}
