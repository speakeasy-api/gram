package skills_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func createSuggestion(t *testing.T, ti *testInstance, skill *gen.RecordSkillResult, content, rationale string) repo.SkillEditSuggestion {
	t.Helper()
	suggestion, err := ti.repo.CreateSkillEditSuggestion(t.Context(), repo.CreateSkillEditSuggestionParams{
		ProposedDiff:       diffTo(t, skill.Version.Content, content),
		Rationale:          rationale,
		ScoredSessionCount: 3,
		BaseVersionID:      uuid.MustParse(skill.Version.ID),
		ProjectID:          ti.projectID,
		SkillID:            uuid.MustParse(skill.Skill.ID),
	})
	require.NoError(t, err)
	return suggestion
}

func TestSkillsListSuggestionsOpenOnlyPaginationCountAndFilter(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	alpha := createSkill(t, ctx, ti, "suggestion-list-alpha", "Alpha base.")
	beta := createSkill(t, ctx, ti, "suggestion-list-beta", "Beta base.")
	dismissedSkill := createSkill(t, ctx, ti, "suggestion-list-dismissed", "Dismissed base.")
	watermarkSkill := createSkill(t, ctx, ti, "suggestion-list-watermark", "Watermark base.")
	alphaSuggestion := createSuggestion(t, ti, alpha, skillManifest(alpha.Skill.Name, "Alpha proposed.", "alpha proposal"), "alpha rationale")
	betaSuggestion := createSuggestion(t, ti, beta, skillManifest(beta.Skill.Name, "Beta proposed.", "beta proposal"), "beta rationale")
	dismissed := createSuggestion(t, ti, dismissedSkill, skillManifest(dismissedSkill.Skill.Name, "Dismissed proposed.", "dismissed proposal"), "dismissed rationale")
	_, err := ti.service.DismissSuggestion(ctx, &gen.DismissSuggestionPayload{
		ID: dismissed.ID.String(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	_, err = ti.repo.CreateSkillEditSuggestionWatermark(ctx, repo.CreateSkillEditSuggestionWatermarkParams{
		Rationale: "watermark", ScoredSessionCount: 6,
		BaseVersionID: uuid.MustParse(watermarkSkill.Version.ID), ProjectID: ti.projectID, SkillID: uuid.MustParse(watermarkSkill.Skill.ID),
	})
	require.NoError(t, err)

	first, err := ti.service.ListSuggestions(ctx, &gen.ListSuggestionsPayload{
		SkillID: nil, Cursor: nil, Limit: 1, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), first.TotalOpenCount)
	require.Len(t, first.Suggestions, 1)
	require.Equal(t, betaSuggestion.ID.String(), first.Suggestions[0].ID)
	require.NotNil(t, first.NextCursor)
	require.Equal(t, beta.Skill.Name, first.Suggestions[0].SkillName)
	require.Equal(t, beta.Skill.DisplayName, first.Suggestions[0].SkillDisplayName)
	require.Equal(t, betaSuggestion.ProposedDiff, first.Suggestions[0].ProposedDiff)
	require.Equal(t, skillManifest(beta.Skill.Name, "Beta proposed.", "beta proposal"), first.Suggestions[0].ProposedContent)
	require.True(t, first.Suggestions[0].AppliesCleanly)
	require.Equal(t, betaSuggestion.Rationale, first.Suggestions[0].Rationale)

	second, err := ti.service.ListSuggestions(ctx, &gen.ListSuggestionsPayload{
		SkillID: nil, Cursor: first.NextCursor, Limit: 1, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), second.TotalOpenCount)
	require.Len(t, second.Suggestions, 1)
	require.Equal(t, alphaSuggestion.ID.String(), second.Suggestions[0].ID)
	require.Nil(t, second.NextCursor)

	filtered, err := ti.service.ListSuggestions(ctx, &gen.ListSuggestionsPayload{
		SkillID: conv.PtrEmpty(alpha.Skill.ID), Cursor: nil, Limit: 20,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), filtered.TotalOpenCount)
	require.Len(t, filtered.Suggestions, 1)
	require.Equal(t, alphaSuggestion.ID.String(), filtered.Suggestions[0].ID)

	empty, err := ti.service.ListSuggestions(ctx, &gen.ListSuggestionsPayload{
		SkillID: conv.PtrEmpty(watermarkSkill.Skill.ID), Cursor: nil, Limit: 20,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Zero(t, empty.TotalOpenCount)
	require.NotNil(t, empty.Suggestions)
	require.Empty(t, empty.Suggestions)

	_, err = ti.service.ListSuggestions(ctx, &gen.ListSuggestionsPayload{
		SkillID: nil, Cursor: conv.PtrEmpty("invalid"), Limit: 20,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.ListSuggestions(ctx, &gen.ListSuggestionsPayload{
		SkillID: nil, Cursor: nil, Limit: 0,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = ti.service.ListSuggestions(ctx, &gen.ListSuggestionsPayload{
		SkillID: nil, Cursor: nil, Limit: 51,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSkillsApproveSuggestionHappyEditedAndAuditPrivacy(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "suggestion-approve-edited", "Base.")
	const proposedMarker = "sensitive-proposed-marker"
	const rationaleMarker = "sensitive-rationale-marker"
	const editedMarker = "sensitive-edited-marker"
	suggestion := createSuggestion(t, ti, created, skillManifest(created.Skill.Name, "Proposed.", proposedMarker), rationaleMarker)
	approveBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillSuggestionApprove)
	require.NoError(t, err)
	addVersionBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillAddVersion)
	require.NoError(t, err)

	edited := skillManifest(created.Skill.Name, "Edited.", editedMarker)
	result, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: suggestion.ID.String(), Content: &edited, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "applied", result.Outcome)
	require.NotNil(t, result.Version)
	require.Equal(t, edited, result.Version.Content)
	require.NotNil(t, result.Version.DerivedFromVersionID)
	require.Equal(t, created.Version.ID, *result.Version.DerivedFromVersionID)
	require.Equal(t, string(skills.EditSuggestionStatusApproved), result.Suggestion.Status)
	require.NotNil(t, result.Suggestion.ApprovedByUserID)
	require.NotNil(t, result.Suggestion.ApprovedAt)

	approveAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillSuggestionApprove)
	require.NoError(t, err)
	require.Equal(t, approveBefore+1, approveAfter)
	addVersionAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillAddVersion)
	require.NoError(t, err)
	require.Equal(t, addVersionBefore+1, addVersionAfter)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSkillSuggestionApprove)
	require.NoError(t, err)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, suggestion.ID.String(), metadata["suggestion_id"])
	require.Equal(t, created.Version.ID, metadata["base_version_id"])
	require.Equal(t, result.Version.ID, metadata["resulting_version_id"])
	require.Equal(t, true, metadata["edited"])
	require.Empty(t, record.BeforeSnapshot)
	require.Empty(t, record.AfterSnapshot)
	for _, sensitive := range []string{proposedMarker, rationaleMarker, editedMarker} {
		require.NotContains(t, string(record.Metadata), sensitive)
	}
}

func TestSkillsApproveSuggestionStaleAndHistoricalDuplicate(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	staleSkill := createSkill(t, ctx, ti, "suggestion-approve-stale", "First.")
	_, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: staleSkill.Skill.ID, Content: skillManifest(staleSkill.Skill.Name, "Second.", "second"),
		DerivedFromVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	stale, err := ti.repo.CreateSkillEditSuggestion(ctx, repo.CreateSkillEditSuggestionParams{
		ProposedDiff: diffTo(t, staleSkill.Version.Content, skillManifest(staleSkill.Skill.Name, "Stale proposed.", "stale")), Rationale: "stale rationale",
		ScoredSessionCount: 1, BaseVersionID: uuid.MustParse(staleSkill.Version.ID),
		ProjectID: ti.projectID, SkillID: uuid.MustParse(staleSkill.Skill.ID),
	})
	require.NoError(t, err)
	staleResult, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: stale.ID.String(), Content: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "superseded", staleResult.Outcome)
	require.Nil(t, staleResult.Version)
	require.Equal(t, string(skills.EditSuggestionStatusSuperseded), staleResult.Suggestion.Status)

	duplicateSkill := createSkill(t, ctx, ti, "suggestion-approve-duplicate", "First.")
	firstContent := duplicateSkill.Version.Content
	current, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: duplicateSkill.Skill.ID, Content: skillManifest(duplicateSkill.Skill.Name, "Current.", "current"),
		DerivedFromVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	duplicate := createSuggestion(t, ti, current, firstContent, "duplicate rationale")
	_, err = ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: duplicate.ID.String(), Content: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)
	preserved, err := ti.repo.GetSkillEditSuggestionForUpdate(ctx, repo.GetSkillEditSuggestionForUpdateParams{
		ProjectID: ti.projectID, ID: duplicate.ID,
	})
	require.NoError(t, err)
	require.Equal(t, string(skills.EditSuggestionStatusOpen), preserved.Status)
	got, err := ti.service.Get(ctx, &gen.GetPayload{
		ID: duplicateSkill.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, current.Version.ID, got.LatestVersion.ID)
}

func TestSkillsApproveSuggestionRejectsConcurrentSuggestionUpdate(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "suggestion-approve-lock", "Base.")
	suggestion := createSuggestion(t, ti, created, skillManifest(created.Skill.Name, "Proposed.", "proposal"), "rationale")

	lockTx := testenv.BeginTx(t, ctx, ti.conn)
	_, err := repo.New(lockTx).GetSkillForUpdate(ctx, repo.GetSkillForUpdateParams{
		ProjectID: ti.projectID, ID: uuid.MustParse(created.Skill.ID),
	})
	require.NoError(t, err)
	type approveResult struct {
		result *gen.ApproveSkillSuggestionResult
		err    error
	}
	finished := make(chan approveResult, 1)
	go func() {
		result, approveErr := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
			ID: suggestion.ID.String(), Content: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		})
		finished <- approveResult{result: result, err: approveErr}
	}()
	require.Never(t, func() bool {
		select {
		case <-finished:
			return true
		default:
			return false
		}
	}, 100*time.Millisecond, 10*time.Millisecond)
	_, err = repo.New(lockTx).UpdateOpenSkillEditSuggestion(ctx, repo.UpdateOpenSkillEditSuggestionParams{
		ProposedDiff:       diffTo(t, created.Version.Content, skillManifest(created.Skill.Name, "Updated while approval waited.", "updated")),
		Rationale:          "updated rationale",
		ScoredSessionCount: 5,
		ProjectID:          ti.projectID,
		SkillID:            suggestion.SkillID,
		BaseVersionID:      suggestion.BaseVersionID,
	})
	require.NoError(t, err)
	require.NoError(t, lockTx.Commit(ctx))

	var completed approveResult
	require.Eventually(t, func() bool {
		select {
		case completed = <-finished:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	requireOopsCode(t, completed.err, oops.CodeConflict)
	require.Nil(t, completed.result)
}

func TestSkillsDismissSuggestionIdempotentAndConflicts(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	dismissSkill := createSkill(t, ctx, ti, "suggestion-dismiss", "Base.")
	dismissSuggestion := createSuggestion(t, ti, dismissSkill, skillManifest(dismissSkill.Skill.Name, "Proposed.", "proposal"), "private rationale")
	dismissBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillSuggestionDismiss)
	require.NoError(t, err)

	first, err := ti.service.DismissSuggestion(ctx, &gen.DismissSuggestionPayload{
		ID: dismissSuggestion.ID.String(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, string(skills.EditSuggestionStatusDismissed), first.Status)
	second, err := ti.service.DismissSuggestion(ctx, &gen.DismissSuggestionPayload{
		ID: dismissSuggestion.ID.String(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	dismissAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillSuggestionDismiss)
	require.NoError(t, err)
	require.Equal(t, dismissBefore+1, dismissAfter)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSkillSuggestionDismiss)
	require.NoError(t, err)
	require.NotContains(t, string(record.Metadata), dismissSuggestion.ProposedDiff)
	require.NotContains(t, string(record.Metadata), dismissSuggestion.Rationale)

	approvedSkill := createSkill(t, ctx, ti, "suggestion-dismiss-approved", "Base.")
	approvedSuggestion := createSuggestion(t, ti, approvedSkill, skillManifest(approvedSkill.Skill.Name, "Proposed.", "proposal"), "rationale")
	_, err = ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: approvedSuggestion.ID.String(), Content: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	_, err = ti.service.DismissSuggestion(ctx, &gen.DismissSuggestionPayload{
		ID: approvedSuggestion.ID.String(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)

	supersededSkill := createSkill(t, ctx, ti, "suggestion-dismiss-superseded", "Base.")
	supersededSuggestion := createSuggestion(t, ti, supersededSkill, skillManifest(supersededSkill.Skill.Name, "Proposed.", "proposal"), "rationale")
	_, err = ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: supersededSkill.Skill.ID, Content: skillManifest(supersededSkill.Skill.Name, "Moved.", "moved"),
		DerivedFromVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	_, err = ti.service.DismissSuggestion(ctx, &gen.DismissSuggestionPayload{
		ID: supersededSuggestion.ID.String(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestSkillsApproveAllSuggestionsContinuesThroughMixedOutcomes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	appliedSkill := createSkill(t, ctx, ti, "suggestion-bulk-applied", "Base.")
	applied := createSuggestion(t, ti, appliedSkill, skillManifest(appliedSkill.Skill.Name, "Proposed.", "proposal"), "rationale")

	staleSkill := createSkill(t, ctx, ti, "suggestion-bulk-stale", "First.")
	_, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: staleSkill.Skill.ID, Content: skillManifest(staleSkill.Skill.Name, "Second.", "second"),
		DerivedFromVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	stale, err := ti.repo.CreateSkillEditSuggestion(ctx, repo.CreateSkillEditSuggestionParams{
		ProposedDiff: diffTo(t, staleSkill.Version.Content, skillManifest(staleSkill.Skill.Name, "Proposed.", "proposal")), Rationale: "rationale",
		ScoredSessionCount: 1, BaseVersionID: uuid.MustParse(staleSkill.Version.ID),
		ProjectID: ti.projectID, SkillID: uuid.MustParse(staleSkill.Skill.ID),
	})
	require.NoError(t, err)

	conflictSkill := createSkill(t, ctx, ti, "suggestion-bulk-conflict", "First.")
	firstContent := conflictSkill.Version.Content
	current, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: conflictSkill.Skill.ID, Content: skillManifest(conflictSkill.Skill.Name, "Current.", "current"),
		DerivedFromVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	conflict := createSuggestion(t, ti, current, firstContent, "rationale")
	approveBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillSuggestionApprove)
	require.NoError(t, err)

	result, err := ti.service.ApproveAllSuggestions(ctx, &gen.ApproveAllSuggestionsPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 3)
	outcomes := make(map[string]string, len(result.Items))
	for _, item := range result.Items {
		outcomes[item.SuggestionID] = item.Outcome
		require.NotEmpty(t, item.SkillID)
		require.NotEmpty(t, item.SkillName)
		require.NotEmpty(t, item.SkillDisplayName)
	}
	require.Equal(t, "applied", outcomes[applied.ID.String()])
	require.Equal(t, "superseded", outcomes[stale.ID.String()])
	require.Equal(t, "conflict", outcomes[conflict.ID.String()])
	approveAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillSuggestionApprove)
	require.NoError(t, err)
	require.Equal(t, approveBefore+1, approveAfter)
	preserved, err := ti.repo.GetSkillEditSuggestionForUpdate(ctx, repo.GetSkillEditSuggestionForUpdateParams{
		ProjectID: ti.projectID, ID: conflict.ID,
	})
	require.NoError(t, err)
	require.Equal(t, string(skills.EditSuggestionStatusOpen), preserved.Status)
}

func TestSkillsSuggestionManagementRBACDenied(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "suggestion-rbac", "Base.")
	suggestion := createSuggestion(t, ti, created, skillManifest(created.Skill.Name, "Proposed.", "proposal"), "rationale")
	deniedCtx := authztest.WithExactGrants(t, ctx)

	_, err := ti.service.ListSuggestions(deniedCtx, &gen.ListSuggestionsPayload{
		SkillID: nil, Cursor: nil, Limit: 20, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ApproveSuggestion(deniedCtx, &gen.ApproveSuggestionPayload{
		ID: suggestion.ID.String(), Content: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.DismissSuggestion(deniedCtx, &gen.DismissSuggestionPayload{
		ID: suggestion.ID.String(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ApproveAllSuggestions(deniedCtx, &gen.ApproveAllSuggestionsPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	readOnlyCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, ti.projectID.String()))
	_, err = ti.service.ListSuggestions(readOnlyCtx, &gen.ListSuggestionsPayload{
		SkillID: nil, Cursor: nil, Limit: 20, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	_, err = ti.service.ApproveSuggestion(readOnlyCtx, &gen.ApproveSuggestionPayload{
		ID: suggestion.ID.String(), Content: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.DismissSuggestion(readOnlyCtx, &gen.DismissSuggestionPayload{
		ID: suggestion.ID.String(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ApproveAllSuggestions(readOnlyCtx, &gen.ApproveAllSuggestionsPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	open, err := ti.repo.GetOpenSkillEditSuggestion(ctx, repo.GetOpenSkillEditSuggestionParams{
		ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID),
	})
	require.NoError(t, err)
	require.Equal(t, suggestion.ID, open.ID)
}

func TestSkillsApproveSuggestionNonOpenConflicts(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "suggestion-non-open", "Base.")
	suggestion := createSuggestion(t, ti, created, skillManifest(created.Skill.Name, "Proposed.", "proposal"), "rationale")
	_, err := ti.service.DismissSuggestion(ctx, &gen.DismissSuggestionPayload{
		ID: suggestion.ID.String(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	_, err = ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: suggestion.ID.String(), Content: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: uuid.NewString(), Content: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: "not-a-uuid", Content: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	openSkill := createSkill(t, ctx, ti, "suggestion-invalid-edit", "Base.")
	openSuggestion := createSuggestion(t, ti, openSkill, skillManifest(openSkill.Skill.Name, "Proposed.", "proposal"), "rationale")
	_, err = ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: openSuggestion.ID.String(), Content: conv.PtrEmpty(strings.Repeat("x", 65537)),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
