package skills_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestResolveSkillSuggestionBaseUsesPreviousValidVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	first := createSkill(t, ctx, ti, "base-fallback", "First.")
	invalid, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID:               first.Skill.ID,
		Content:          "---\nname: base-fallback\n---\n\ninvalid\n",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.False(t, invalid.Version.SpecValid)
	latest, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID:               first.Skill.ID,
		Content:          skillManifest("base-fallback", "Latest.", "latest"),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	resolved, err := ti.repo.ResolveSkillSuggestionBase(ctx, repo.ResolveSkillSuggestionBaseParams{
		ProjectID: ti.projectID,
		SkillID:   uuid.MustParse(first.Skill.ID),
	})
	require.NoError(t, err)
	require.Equal(t, uuid.MustParse(latest.Version.ID), resolved.BaseVersionID)
	require.Equal(t, uuid.MustParse(first.Version.ID), resolved.PredecessorVersionID)
	require.NotEqual(t, uuid.MustParse(invalid.Version.ID), resolved.PredecessorVersionID)

	_, otherProjectID := createProjectContext(t, ctx, ti)
	_, err = ti.repo.ResolveSkillSuggestionBase(ctx, repo.ResolveSkillSuggestionBaseParams{
		ProjectID: otherProjectID,
		SkillID:   uuid.MustParse(first.Skill.ID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestResolveSkillSuggestionBasePrefersLineageParent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	first := createSkill(t, ctx, ti, "base-lineage", "First.")
	second, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID:               first.Skill.ID,
		Content:          skillManifest("base-lineage", "Second.", "second"),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	derived, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID:                   first.Skill.ID,
		Content:              skillManifest("base-lineage", "Derived.", "derived"),
		DerivedFromVersionID: conv.PtrEmpty(first.Version.ID),
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
	})
	require.NoError(t, err)

	resolved, err := ti.repo.ResolveSkillSuggestionBase(ctx, repo.ResolveSkillSuggestionBaseParams{
		ProjectID: ti.projectID,
		SkillID:   uuid.MustParse(first.Skill.ID),
	})
	require.NoError(t, err)
	require.Equal(t, uuid.MustParse(derived.Version.ID), resolved.BaseVersionID)
	require.Equal(t, uuid.MustParse(first.Version.ID), resolved.PredecessorVersionID)
	require.NotEqual(t, uuid.MustParse(second.Version.ID), resolved.PredecessorVersionID)
}

func TestResolveSkillRegressionBasesMatchesSuggestionBaseVersionPairs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	first := createSkill(t, ctx, ti, "batch-base-first", "First.")
	second := createSkill(t, ctx, ti, "batch-base-second", "Second.")
	_, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: first.Skill.ID, Content: skillManifest("batch-base-first", "Derived.", "derived"),
		DerivedFromVersionID: conv.PtrEmpty(first.Version.ID), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	rows, err := ti.repo.ResolveSkillRegressionBases(ctx, repo.ResolveSkillRegressionBasesParams{
		ProjectID: ti.projectID,
		SkillIds:  []uuid.UUID{uuid.MustParse(second.Skill.ID), uuid.MustParse(first.Skill.ID), uuid.New()},
	})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		single, err := ti.repo.ResolveSkillSuggestionBase(ctx, repo.ResolveSkillSuggestionBaseParams{ProjectID: ti.projectID, SkillID: row.SkillID})
		require.NoError(t, err)
		require.Equal(t, single.BaseVersionID, row.BaseVersionID)
		require.Equal(t, single.PredecessorVersionID, row.PredecessorVersionID)
	}
}

func TestSkillEditSuggestionLifecycleAndTenantIsolation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "suggestion-lifecycle", "Base.")
	skillID := uuid.MustParse(created.Skill.ID)
	baseID := uuid.MustParse(created.Version.ID)

	suggestion, err := seedSuggestion(t, ctx, ti, seedSuggestionParams{
		ProposedDiff:       diffTo(t, created.Version.Content, skillManifest("suggestion-lifecycle", "Proposed.", "proposal one")),
		Rationale:          "first rationale",
		ScoredSessionCount: 3,
		BaseVersionID:      baseID,
		ProjectID:          ti.projectID,
		SkillID:            skillID,
	})
	require.NoError(t, err)
	require.Equal(t, string(skills.EditSuggestionStatusOpen), suggestion.Status)
	_, err = seedSuggestion(t, ctx, ti, seedSuggestionParams{
		ProposedDiff:       "second open",
		Rationale:          "second open",
		ScoredSessionCount: 0,
		BaseVersionID:      baseID,
		ProjectID:          ti.projectID,
		SkillID:            skillID,
	})
	require.Error(t, err)

	updated, err := ti.repo.UpdateOpenSkillEditSuggestion(ctx, repo.UpdateOpenSkillEditSuggestionParams{
		Rationale:          "updated rationale",
		ScoredSessionCount: 5,
		ProjectID:          ti.projectID,
		SkillID:            skillID,
		BaseVersionID:      baseID,
	})
	require.NoError(t, err)
	require.Equal(t, suggestion.ID, updated.ID)
	require.Equal(t, int64(5), updated.ScoredSessionCount)

	open, err := ti.repo.GetOpenSkillEditSuggestion(ctx, repo.GetOpenSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: skillID})
	require.NoError(t, err)
	require.Equal(t, updated, open)

	_, otherProjectID := createProjectContext(t, ctx, ti)
	_, err = ti.repo.GetOpenSkillEditSuggestion(ctx, repo.GetOpenSkillEditSuggestionParams{ProjectID: otherProjectID, SkillID: skillID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = ti.repo.UpdateOpenSkillEditSuggestion(ctx, repo.UpdateOpenSkillEditSuggestionParams{
		Rationale:          "cross-tenant",
		ScoredSessionCount: 0,
		ProjectID:          otherProjectID,
		SkillID:            skillID,
		BaseVersionID:      baseID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	invalidVersion, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID:               created.Skill.ID,
		Content:          "---\nname: suggestion-lifecycle\n---\n\ninvalid base\n",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.False(t, invalidVersion.Version.SpecValid)
	stillOpen, err := ti.repo.GetOpenSkillEditSuggestion(ctx, repo.GetOpenSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: skillID})
	require.NoError(t, err)
	require.Equal(t, suggestion.ID, stillOpen.ID)

	_, err = ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID:               created.Skill.ID,
		Content:          skillManifest("suggestion-lifecycle", "New base.", "new base"),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	superseded, err := ti.repo.GetLatestSkillEditSuggestion(ctx, repo.GetLatestSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: skillID})
	require.NoError(t, err)
	require.Equal(t, suggestion.ID, superseded.ID)
	require.Equal(t, string(skills.EditSuggestionStatusSuperseded), superseded.Status)

	_, err = ti.repo.GetOpenSkillEditSuggestion(ctx, repo.GetOpenSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: skillID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	latest, err := ti.repo.GetLatestSkillEditSuggestion(ctx, repo.GetLatestSkillEditSuggestionParams{ProjectID: ti.projectID, SkillID: skillID})
	require.NoError(t, err)
	require.Equal(t, superseded.ID, latest.ID)
}

func TestSkillSuggestionBaseLockSerializesVersionCreation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "suggestion-lock", "Base.")
	skillID := uuid.MustParse(created.Skill.ID)

	lockTx := testenv.BeginTx(t, ctx, ti.conn)
	lockedRepo := repo.New(lockTx)
	_, err := lockedRepo.GetSkillForUpdate(ctx, repo.GetSkillForUpdateParams{ProjectID: ti.projectID, ID: skillID})
	require.NoError(t, err)
	base, err := lockedRepo.ResolveSkillSuggestionBase(ctx, repo.ResolveSkillSuggestionBaseParams{ProjectID: ti.projectID, SkillID: skillID})
	require.NoError(t, err)
	require.Equal(t, uuid.MustParse(created.Version.ID), base.BaseVersionID)

	type addVersionResult struct {
		result *gen.RecordSkillResult
		err    error
	}
	finished := make(chan addVersionResult, 1)
	go func() {
		result, addErr := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
			ID:               created.Skill.ID,
			Content:          skillManifest("suggestion-lock", "Moved.", "moved"),
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
		})
		finished <- addVersionResult{result: result, err: addErr}
	}()

	require.Never(t, func() bool {
		select {
		case <-finished:
			return true
		default:
			return false
		}
	}, 100*time.Millisecond, 10*time.Millisecond)
	require.NoError(t, lockTx.Commit(ctx))

	var completed addVersionResult
	require.Eventually(t, func() bool {
		select {
		case completed = <-finished:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, completed.err)
	require.NotNil(t, completed.result)
	require.NotEqual(t, base.BaseVersionID.String(), completed.result.Version.ID)
}

func TestSkillSuggestionWakeInputsUseProjectScopedActivityAndFeedback(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	active := createSkill(t, ctx, ti, "sweep-active", "Active.")
	inactive := createSkill(t, ctx, ti, "sweep-inactive", "Inactive.")
	invalid, err := ti.service.Create(ctx, &gen.CreatePayload{
		Content: "---\nname: sweep-invalid\n---\n\ninvalid\n", SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.False(t, invalid.Version.SpecValid)
	seenAt := time.Now().UTC().Add(-time.Hour)
	insertSkillObservation(t, ti, active.Skill.Name, "", "project", "", seenAt)
	insertSkillObservation(t, ti, invalid.Skill.Name, "", "project", "", seenAt)
	reconciled, err := skills.ReconcileSkillObservations(ctx, ti.conn, ti.projectID, 10)
	require.NoError(t, err)
	require.Equal(t, 2, reconciled.Processed)

	listed, err := ti.repo.ListRecentlyActiveSkills(ctx, repo.ListRecentlyActiveSkillsParams{
		ProjectID:        ti.projectID,
		ActiveSince:      pgtype.Timestamptz{Time: seenAt.Add(-time.Minute), Valid: true},
		CursorLastSeenAt: pgtype.Timestamptz{},
		CursorID:         uuid.NullUUID{},
		PageLimit:        10,
	})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, active.Skill.ID, listed[0].ID.String())
	require.NotEqual(t, inactive.Skill.ID, listed[0].ID.String())

	_, otherProjectID := createProjectContext(t, ctx, ti)
	_, err = skills.CaptureSkillContent(ctx, ti.conn, otherProjectID, "---\nname: sweep-invalid-only\n---\n\ninvalid\n")
	require.NoError(t, err)
	insertSkillObservationForProject(t, ti, otherProjectID, "test", "sweep-invalid-only", "", "project", "", seenAt)
	_, err = skills.ReconcileSkillObservations(ctx, ti.conn, otherProjectID, 10)
	require.NoError(t, err)
	projects, err := ti.repo.ListSkillSuggestionProjects(ctx, repo.ListSkillSuggestionProjectsParams{
		AfterProjectID: uuid.Nil,
		ActiveSince:    pgtype.Timestamptz{Time: seenAt.Add(-time.Minute), Valid: true},
		PageLimit:      100,
	})
	require.NoError(t, err)
	require.Contains(t, projects, ti.projectID)
	require.NotContains(t, projects, otherProjectID)

	first := createSkillFeedback(t, ti, ti.projectID, active.Skill.Name, "first")
	createSkillFeedback(t, ti, ti.projectID, active.Skill.Name, "second")
	count, err := ti.repo.CountUnreviewedSkillFeedback(ctx, repo.CountUnreviewedSkillFeedbackParams{ProjectID: ti.projectID, SkillName: active.Skill.Name})
	require.NoError(t, err)
	require.Equal(t, int64(2), count)
	_, err = ti.repo.MarkSkillFeedbackReviewed(ctx, repo.MarkSkillFeedbackReviewedParams{ProjectID: ti.projectID, SkillName: active.Skill.Name, Ids: []uuid.UUID{first.ID}})
	require.NoError(t, err)
	count, err = ti.repo.CountUnreviewedSkillFeedback(ctx, repo.CountUnreviewedSkillFeedbackParams{ProjectID: ti.projectID, SkillName: active.Skill.Name})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
}
