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
	suggestion, err := seedSuggestion(t, t.Context(), ti, seedSuggestionParams{
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

// suggestionChangeIDs returns the proposed changes on a suggestion in review
// order, which is what a reviewer clicks apply on.
func suggestionChangeIDs(t *testing.T, ti *testInstance, suggestionID uuid.UUID) []uuid.UUID {
	t.Helper()
	changes := suggestionChanges(t, t.Context(), ti, suggestionID)
	ids := make([]uuid.UUID, len(changes))
	for i, change := range changes {
		ids[i] = change.ID
	}
	return ids
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
	require.Len(t, first.Suggestions[0].Changes, 1)
	require.Equal(t, suggestionChanges(t, ctx, ti, betaSuggestion.ID)[0].ProposedDiff, first.Suggestions[0].Changes[0].ProposedDiff)
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
		ID: suggestion.ID.String(), Content: &edited, ChangeIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
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
	stale, err := seedSuggestion(t, ctx, ti, seedSuggestionParams{
		ProposedDiff: diffTo(t, staleSkill.Version.Content, skillManifest(staleSkill.Skill.Name, "Stale proposed.", "stale")), Rationale: "stale rationale",
		ScoredSessionCount: 1, BaseVersionID: uuid.MustParse(staleSkill.Version.ID),
		ProjectID: ti.projectID, SkillID: uuid.MustParse(staleSkill.Skill.ID),
	})
	require.NoError(t, err)
	staleResult, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: stale.ID.String(), Content: nil, ChangeIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
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
		ID: duplicate.ID.String(), Content: nil, ChangeIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
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
			ID: suggestion.ID.String(), Content: nil, ChangeIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
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
	require.NotContains(t, string(record.Metadata), suggestionChanges(t, ctx, ti, dismissSuggestion.ID)[0].ProposedDiff)
	require.NotContains(t, string(record.Metadata), dismissSuggestion.Rationale)

	approvedSkill := createSkill(t, ctx, ti, "suggestion-dismiss-approved", "Base.")
	approvedSuggestion := createSuggestion(t, ti, approvedSkill, skillManifest(approvedSkill.Skill.Name, "Proposed.", "proposal"), "rationale")
	_, err = ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: approvedSuggestion.ID.String(), Content: nil, ChangeIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
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
	stale, err := seedSuggestion(t, ctx, ti, seedSuggestionParams{
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

func TestSkillsApproveAllSuggestionsOnlyProcessesSuppliedIDs(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	suppliedSkill := createSkill(t, ctx, ti, "suggestion-bulk-supplied", "Base.")
	supplied := createSuggestion(t, ti, suppliedSkill, skillManifest(suppliedSkill.Skill.Name, "Applied.", "applied"), "rationale")
	remainingSkill := createSkill(t, ctx, ti, "suggestion-bulk-remaining", "Base.")
	remaining := createSuggestion(t, ti, remainingSkill, skillManifest(remainingSkill.Skill.Name, "Remaining.", "remaining"), "rationale")

	result, err := ti.service.ApproveAllSuggestions(ctx, &gen.ApproveAllSuggestionsPayload{
		SuggestionIds: []string{supplied.ID.String(), supplied.ID.String()},
		SessionToken:  nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, supplied.ID.String(), result.Items[0].SuggestionID)
	require.Equal(t, "applied", result.Items[0].Outcome)

	preserved, err := ti.repo.GetSkillEditSuggestionForUpdate(ctx, repo.GetSkillEditSuggestionForUpdateParams{
		ProjectID: ti.projectID, ID: remaining.ID,
	})
	require.NoError(t, err)
	require.Equal(t, string(skills.EditSuggestionStatusOpen), preserved.Status)
}

func TestSkillsApproveAllSuggestionsRejectsInvalidIDs(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.ApproveAllSuggestions(ctx, &gen.ApproveAllSuggestionsPayload{
		SuggestionIds: []string{"not-a-uuid"}, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSkillsApproveAllSuggestionsAcceptsMoreThan500IDs(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	suggestionIDs := make([]string, 501)
	for i := range suggestionIDs {
		suggestionIDs[i] = uuid.NewString()
	}

	result, err := ti.service.ApproveAllSuggestions(ctx, &gen.ApproveAllSuggestionsPayload{
		SuggestionIds: suggestionIDs, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.Items)
}

func TestSkillsApproveAllSuggestionsReturnsConflictForSuppliedDismissedSuggestion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "suggestion-bulk-dismissed", "Base.")
	suggestion := createSuggestion(t, ti, created, skillManifest(created.Skill.Name, "Proposed.", "proposal"), "rationale")
	_, err := ti.service.DismissSuggestion(ctx, &gen.DismissSuggestionPayload{
		ID: suggestion.ID.String(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	dismissed, err := ti.repo.GetSkillEditSuggestionForUpdate(ctx, repo.GetSkillEditSuggestionForUpdateParams{
		ProjectID: ti.projectID, ID: suggestion.ID,
	})
	require.NoError(t, err)

	result, err := ti.service.ApproveAllSuggestions(ctx, &gen.ApproveAllSuggestionsPayload{
		SuggestionIds: []string{suggestion.ID.String()}, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, suggestion.ID.String(), result.Items[0].SuggestionID)
	require.Equal(t, "conflict", result.Items[0].Outcome)

	preserved, err := ti.repo.GetSkillEditSuggestionForUpdate(ctx, repo.GetSkillEditSuggestionForUpdateParams{
		ProjectID: ti.projectID, ID: suggestion.ID,
	})
	require.NoError(t, err)
	require.Equal(t, dismissed, preserved)
}

func TestSkillsApproveAllSuggestionsWithoutIDsOnlyProcessesOpenSuggestions(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	openSkill := createSkill(t, ctx, ti, "suggestion-bulk-open-only", "Base.")
	openSuggestion := createSuggestion(t, ti, openSkill, skillManifest(openSkill.Skill.Name, "Applied.", "applied"), "rationale")
	dismissedSkill := createSkill(t, ctx, ti, "suggestion-bulk-omitted-dismissed", "Base.")
	dismissedSuggestion := createSuggestion(t, ti, dismissedSkill, skillManifest(dismissedSkill.Skill.Name, "Dismissed.", "dismissed"), "rationale")
	_, err := ti.service.DismissSuggestion(ctx, &gen.DismissSuggestionPayload{
		ID: dismissedSuggestion.ID.String(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	result, err := ti.service.ApproveAllSuggestions(ctx, &gen.ApproveAllSuggestionsPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, openSuggestion.ID.String(), result.Items[0].SuggestionID)
	require.Equal(t, "applied", result.Items[0].Outcome)

	preserved, err := ti.repo.GetSkillEditSuggestionForUpdate(ctx, repo.GetSkillEditSuggestionForUpdateParams{
		ProjectID: ti.projectID, ID: dismissedSuggestion.ID,
	})
	require.NoError(t, err)
	require.Equal(t, string(skills.EditSuggestionStatusDismissed), preserved.Status)
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
		ID: suggestion.ID.String(), Content: nil, ChangeIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
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
		ID: suggestion.ID.String(), Content: nil, ChangeIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
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
		ID: suggestion.ID.String(), Content: nil, ChangeIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: uuid.NewString(), Content: nil, ChangeIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: "not-a-uuid", Content: nil, ChangeIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	openSkill := createSkill(t, ctx, ti, "suggestion-invalid-edit", "Base.")
	openSuggestion := createSuggestion(t, ti, openSkill, skillManifest(openSkill.Skill.Name, "Proposed.", "proposal"), "rationale")
	_, err = ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: openSuggestion.ID.String(), Content: conv.PtrEmpty(strings.Repeat("x", 65537)),
		ChangeIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// The two edited lines sit far enough apart that they render as separate
// hunks rather than one merged hunk.
const twoChangeBody = `Announce the window in the release channel.
Confirm the rollback artifact exists.
Check the error budget.
Watch the canary for ten minutes.
Compare latency against the previous release.
Stop on any regression.
Drain the old revision.
Verify the health checks pass.
Update the status page.
Note the release in the changelog.
Tag the deployment.
Close the window.`

func TestSkillsApproveSuggestionTakesOneChangeAndKeepsTheRestOpen(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created, err := ti.service.Create(ctx, &gen.CreatePayload{
		Content:          skillManifest("suggestion-approve-partial", "Runbook.", twoChangeBody),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	budget := strings.Replace(twoChangeBody, "Check the error budget.", "Check the error budget and page the on-call.", 1)
	both := strings.Replace(budget, "Close the window.", "Close the window and record the duration.", 1)
	afterBudget := skillManifest(created.Skill.Name, "Runbook.", budget)
	proposed := skillManifest(created.Skill.Name, "Runbook.", both)

	suggestion := createSuggestion(t, ti, created, afterBudget, "page the on-call")
	_, err = ti.repo.CreateSkillEditSuggestionChange(ctx, repo.CreateSkillEditSuggestionChangeParams{
		ProposedDiff: diffTo(t, afterBudget, proposed),
		Rationale:    "record the duration",
		Position:     1,
		ProjectID:    ti.projectID,
		SuggestionID: suggestion.ID,
	})
	require.NoError(t, err)

	changeIDs := suggestionChangeIDs(t, ti, suggestion.ID)
	require.Len(t, changeIDs, 2)
	first := changeIDs[0].String()
	partial, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: suggestion.ID.String(), Content: nil, ChangeIds: []string{first}, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "partially_applied", partial.Outcome)
	require.NotNil(t, partial.Version)
	require.Contains(t, partial.Version.Content, "page the on-call")
	require.NotContains(t, partial.Version.Content, "record the duration")

	// The change that was taken is gone; the other keeps its own rationale and
	// now proposes against the version the first one created.
	require.Equal(t, string(skills.EditSuggestionStatusOpen), partial.Suggestion.Status)
	require.Equal(t, partial.Version.ID, partial.Suggestion.BaseVersionID)
	require.Len(t, partial.Suggestion.Changes, 1)
	require.Equal(t, "record the duration", partial.Suggestion.Changes[0].Rationale)
	require.True(t, partial.Suggestion.Changes[0].AppliesCleanly)
	require.Contains(t, partial.Suggestion.Changes[0].ProposedDiff, "+Close the window and record the duration.")
	require.NotContains(t, partial.Suggestion.Changes[0].ProposedDiff, "+Check the error budget and page the on-call.")
	require.Equal(t, proposed, partial.Suggestion.ProposedContent)

	second := suggestionChangeIDs(t, ti, suggestion.ID)[0].String()
	final, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: suggestion.ID.String(), Content: nil, ChangeIds: []string{second}, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "applied", final.Outcome)
	require.NotNil(t, final.Version)
	require.Equal(t, proposed, final.Version.Content)
	require.Equal(t, string(skills.EditSuggestionStatusApproved), final.Suggestion.Status)
}

func TestSkillsApproveSuggestionTakesSelectedChangesAsOneVersion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created, err := ti.service.Create(ctx, &gen.CreatePayload{
		Content:          skillManifest("suggestion-approve-selected", "Runbook.", twoChangeBody),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	announce := strings.Replace(twoChangeBody, "Announce the window in the release channel.", "Announce the window in the release channel and page the on-call.", 1)
	budget := strings.Replace(announce, "Watch the canary for ten minutes.", "Watch the canary for twenty minutes.", 1)
	all := strings.Replace(budget, "Close the window.", "Close the window and record the duration.", 1)
	afterAnnounce := skillManifest(created.Skill.Name, "Runbook.", announce)
	afterBudget := skillManifest(created.Skill.Name, "Runbook.", budget)
	proposed := skillManifest(created.Skill.Name, "Runbook.", all)

	suggestion := createSuggestion(t, ti, created, afterAnnounce, "page the on-call")
	_, err = ti.repo.CreateSkillEditSuggestionChange(ctx, repo.CreateSkillEditSuggestionChangeParams{
		ProposedDiff: diffTo(t, afterAnnounce, afterBudget),
		Rationale:    "watch longer",
		Position:     1,
		ProjectID:    ti.projectID,
		SuggestionID: suggestion.ID,
	})
	require.NoError(t, err)
	_, err = ti.repo.CreateSkillEditSuggestionChange(ctx, repo.CreateSkillEditSuggestionChangeParams{
		ProposedDiff: diffTo(t, afterBudget, proposed),
		Rationale:    "record the duration",
		Position:     2,
		ProjectID:    ti.projectID,
		SuggestionID: suggestion.ID,
	})
	require.NoError(t, err)

	changeIDs := suggestionChangeIDs(t, ti, suggestion.ID)
	require.Len(t, changeIDs, 3)
	selected := []string{changeIDs[0].String(), changeIDs[2].String()}
	partial, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: suggestion.ID.String(), Content: nil, ChangeIds: selected, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "partially_applied", partial.Outcome)
	require.NotNil(t, partial.Version)
	// One version carries both selected edits; the unselected one is untouched.
	require.Contains(t, partial.Version.Content, "page the on-call")
	require.Contains(t, partial.Version.Content, "record the duration")
	require.NotContains(t, partial.Version.Content, "twenty minutes")

	require.Equal(t, string(skills.EditSuggestionStatusOpen), partial.Suggestion.Status)
	require.Equal(t, partial.Version.ID, partial.Suggestion.BaseVersionID)
	require.Len(t, partial.Suggestion.Changes, 1)
	require.Equal(t, "watch longer", partial.Suggestion.Changes[0].Rationale)
	require.True(t, partial.Suggestion.Changes[0].AppliesCleanly)
}

func TestSkillsApproveSuggestionSelectingEveryChangeApprovesFully(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created, err := ti.service.Create(ctx, &gen.CreatePayload{
		Content:          skillManifest("suggestion-approve-select-all", "Runbook.", twoChangeBody),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	budget := strings.Replace(twoChangeBody, "Check the error budget.", "Check the error budget and page the on-call.", 1)
	both := strings.Replace(budget, "Close the window.", "Close the window and record the duration.", 1)
	afterBudget := skillManifest(created.Skill.Name, "Runbook.", budget)
	proposed := skillManifest(created.Skill.Name, "Runbook.", both)

	suggestion := createSuggestion(t, ti, created, afterBudget, "page the on-call")
	_, err = ti.repo.CreateSkillEditSuggestionChange(ctx, repo.CreateSkillEditSuggestionChangeParams{
		ProposedDiff: diffTo(t, afterBudget, proposed),
		Rationale:    "record the duration",
		Position:     1,
		ProjectID:    ti.projectID,
		SuggestionID: suggestion.ID,
	})
	require.NoError(t, err)

	changeIDs := suggestionChangeIDs(t, ti, suggestion.ID)
	require.Len(t, changeIDs, 2)
	// Duplicated IDs collapse instead of double-applying a change.
	selected := []string{changeIDs[0].String(), changeIDs[1].String(), changeIDs[0].String()}
	final, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: suggestion.ID.String(), Content: nil, ChangeIds: selected, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "applied", final.Outcome)
	require.NotNil(t, final.Version)
	require.Equal(t, proposed, final.Version.Content)
	require.Equal(t, string(skills.EditSuggestionStatusApproved), final.Suggestion.Status)
}

func TestSkillsApproveSuggestionRejectsUnknownChangeAndEditedCombination(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "suggestion-approve-change-guards", "Base.")
	suggestion := createSuggestion(t, ti, created, skillManifest(created.Skill.Name, "Proposed.", "proposal"), "rationale")

	missing := uuid.NewString()
	_, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: suggestion.ID.String(), Content: nil, ChangeIds: []string{missing}, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.ErrorContains(t, err, "no longer part of this suggestion")
	requireOopsCode(t, err, oops.CodeConflict)

	edited := skillManifest(created.Skill.Name, "Edited.", "edited")
	only := suggestionChangeIDs(t, ti, suggestion.ID)[0].String()
	_, err = ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: suggestion.ID.String(), Content: &edited, ChangeIds: []string{only}, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
