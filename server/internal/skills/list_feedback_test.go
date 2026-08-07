package skills_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	skillservice "github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func recordResolvedFeedback(t *testing.T, ti *testInstance, projectID, skillID, versionID uuid.UUID, skillName string, outcome skillservice.FeedbackOutcome, note *string) repo.SkillFeedback {
	t.Helper()
	row, err := ti.repo.CreateSkillFeedback(t.Context(), repo.CreateSkillFeedbackParams{
		ProjectID: projectID, SkillID: uuid.NullUUID{UUID: skillID, Valid: true}, SkillVersionID: uuid.NullUUID{UUID: versionID, Valid: true},
		SkillName: skillName, Source: string(skillservice.FeedbackSourceDev), Outcome: string(outcome), Note: conv.PtrToPGText(note),
		SessionID: pgtype.Text{String: "private-session", Valid: true}, UserID: pgtype.Text{String: "private-user", Valid: true},
		UserEmail: pgtype.Text{String: "private@example.test", Valid: true},
	})
	require.NoError(t, err)
	return row
}

func TestListSkillFeedbackCountsPagesAndSurvivesRename(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "feedback-review", "Review feedback.")
	skillID := uuid.MustParse(created.Skill.ID)
	versionID := uuid.MustParse(created.Version.ID)
	outcomes := []skillservice.FeedbackOutcome{
		skillservice.FeedbackOutcomeHelped,
		skillservice.FeedbackOutcomePartiallyHelped,
		skillservice.FeedbackOutcomeDidNotHelp,
		skillservice.FeedbackOutcomeMisleading,
		skillservice.FeedbackOutcomeHarmful,
	}
	var rows []repo.SkillFeedback
	for i, outcome := range outcomes {
		var note *string
		if i > 0 {
			noteValue := "note-" + string(outcome)
			note = &noteValue
		}
		rows = append(rows, recordResolvedFeedback(t, ti, ti.projectID, skillID, versionID, created.Skill.Name, outcome, note))
	}
	_, err := hooksrepo.New(ti.conn).InsertSkillObservation(ctx, hooksrepo.InsertSkillObservationParams{
		ProjectID: ti.projectID, IdempotencyKey: conv.ToPGText(uuid.NewString()), Provider: "test",
		UserID: pgtype.Text{}, UserEmail: pgtype.Text{}, Hostname: conv.ToPGText("machine"),
		SessionID: conv.ToPGText("private-session"), SkillName: created.Skill.Name, Source: conv.ToPGText("workspace"),
		SourceLevel: conv.ToPGText("project"), SourcePath: pgtype.Text{}, RawSha256: pgtype.Text{},
		SeenAt: conv.ToPGTimestamptz(time.Now().UTC()),
	})
	require.NoError(t, err)
	_, err = skillservice.ReconcileSkillObservations(ctx, ti.conn, ti.projectID, 10)
	require.NoError(t, err)
	_, err = ti.repo.CreateSkillFeedback(ctx, repo.CreateSkillFeedbackParams{
		ProjectID: ti.projectID, SkillID: uuid.NullUUID{}, SkillVersionID: uuid.NullUUID{}, SkillName: created.Skill.Name,
		Source: string(skillservice.FeedbackSourceDev), Outcome: string(skillservice.FeedbackOutcomeHarmful), Note: pgtype.Text{},
		SessionID: pgtype.Text{}, UserID: pgtype.Text{}, UserEmail: pgtype.Text{},
	})
	require.NoError(t, err)
	_, err = ti.service.Update(ctx, &gen.UpdatePayload{
		ID: created.Skill.ID, Name: "feedback-review-renamed", DisplayName: "Feedback review renamed", Summary: created.Skill.Summary,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	first, err := ti.service.ListFeedback(ctx, &gen.ListFeedbackPayload{ID: created.Skill.ID, Limit: 2, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, &gen.SkillFeedbackCounts{Total: 5, Helped: 1, PartiallyHelped: 1, DidNotHelp: 1, Misleading: 1, Harmful: 1}, first.Counts)
	require.Equal(t, int64(5), first.Metrics.FeedbackInWindow)
	require.Equal(t, int64(1), first.Metrics.ActivationsInWindow)
	require.Equal(t, int64(1), first.Metrics.FeedbackActivationsInWindow)
	require.Equal(t, int64(5), first.Metrics.Unreviewed)
	require.Equal(t, int64(0), first.Metrics.Converted)
	require.Len(t, first.Timeline, 30)
	require.Equal(t, first.Metrics.WindowStart, first.Timeline[0].BucketStart)
	var timelineTotal int64
	for _, point := range first.Timeline {
		timelineTotal += point.FeedbackCount
	}
	require.Equal(t, first.Metrics.FeedbackInWindow, timelineTotal)
	require.Len(t, first.Feedback, 2)
	require.NotNil(t, first.NextCursor)
	require.Equal(t, rows[4].ID.String(), first.Feedback[0].ID)
	require.Equal(t, rows[3].ID.String(), first.Feedback[1].ID)

	second, err := ti.service.ListFeedback(ctx, &gen.ListFeedbackPayload{ID: created.Skill.ID, Cursor: first.NextCursor, Limit: 3, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, second.Feedback, 3)
	require.Nil(t, second.NextCursor)
	require.Equal(t, rows[2].ID.String(), second.Feedback[0].ID)
	require.Nil(t, second.Feedback[2].Note)
	require.Equal(t, created.Version.ID, *second.Feedback[2].SkillVersionID)
	require.Nil(t, second.Feedback[2].ReviewedAt)
}

func TestListSkillFeedbackIsolatesProjectAndSkill(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	first := createSkill(t, ctx, ti, "feedback-first", "First.")
	second := createSkill(t, ctx, ti, "feedback-second", "Second.")
	recordResolvedFeedback(t, ti, ti.projectID, uuid.MustParse(first.Skill.ID), uuid.MustParse(first.Version.ID), first.Skill.Name, skillservice.FeedbackOutcomeHelped, nil)
	recordResolvedFeedback(t, ti, ti.projectID, uuid.MustParse(second.Skill.ID), uuid.MustParse(second.Version.ID), second.Skill.Name, skillservice.FeedbackOutcomeHarmful, nil)
	otherCtx, otherProjectID := createProjectContext(t, ctx, ti, authz.ScopeSkillWrite)
	other := createSkill(t, otherCtx, ti, "feedback-first", "Other project.")
	recordResolvedFeedback(t, ti, otherProjectID, uuid.MustParse(other.Skill.ID), uuid.MustParse(other.Version.ID), other.Skill.Name, skillservice.FeedbackOutcomeMisleading, nil)

	result, err := ti.service.ListFeedback(ctx, &gen.ListFeedbackPayload{ID: first.Skill.ID, Limit: 20, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Counts.Total)
	require.Equal(t, int64(1), result.Counts.Helped)
	require.Len(t, result.Feedback, 1)
	_, err = ti.service.ListFeedback(otherCtx, &gen.ListFeedbackPayload{ID: first.Skill.ID, Limit: 20, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestListSkillFeedbackValidatesAccessLimitCursorAndPrivacyShape(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "feedback-access", "Access.")
	noGrants := authztest.WithExactGrants(t, ctx)
	_, err := ti.service.ListFeedback(noGrants, &gen.ListFeedbackPayload{ID: created.Skill.ID, Limit: 20, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	for _, limit := range []int{0, 51} {
		_, err = ti.service.ListFeedback(ctx, &gen.ListFeedbackPayload{ID: created.Skill.ID, Limit: limit, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
	badCursor := "bad"
	_, err = ti.service.ListFeedback(ctx, &gen.ListFeedbackPayload{ID: created.Skill.ID, Cursor: &badCursor, Limit: 20, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
	typeOfFeedback := reflect.TypeFor[gen.SkillFeedback]()
	for _, privateField := range []string{"UserID", "UserEmail", "SessionID"} {
		_, found := typeOfFeedback.FieldByName(privateField)
		require.False(t, found)
	}
}
