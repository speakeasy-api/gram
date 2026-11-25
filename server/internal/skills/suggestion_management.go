package skills

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/skilldiff"
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (s *Service) ListSuggestions(ctx context.Context, payload *gen.ListSuggestionsPayload) (*gen.ListSkillSuggestionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogProjectID(authCtx.ProjectID.String()),
	)
	if err := s.requireAccess(ctx, authCtx, authz.SkillCollection.Read); err != nil {
		return nil, err
	}
	if payload.Limit < 1 || payload.Limit > 50 {
		return nil, oops.E(oops.CodeBadRequest, nil, "skill suggestion limit must be between 1 and 50")
	}

	skillID, err := conv.PtrToNullUUID(payload.SkillID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill id")
	}
	cursorCreatedAt := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	cursorID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if payload.Cursor != nil {
		createdAt, id, decodeErr := decodeCreatedAtIDCursor(*payload.Cursor)
		if decodeErr != nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill suggestion cursor")
		}
		cursorCreatedAt = conv.ToPGTimestamptz(createdAt)
		cursorID = conv.ToNullUUID(id)
	}

	queries := repo.New(s.db)
	rows, err := queries.ListOpenSkillEditSuggestions(ctx, repo.ListOpenSkillEditSuggestionsParams{
		ProjectID:       *authCtx.ProjectID,
		SkillID:         skillID,
		CursorCreatedAt: cursorCreatedAt,
		CursorID:        cursorID,
		PageLimit:       conv.SafeInt32(payload.Limit + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list skill suggestions").LogError(ctx, logger)
	}
	totalOpenCount, err := queries.CountOpenSkillEditSuggestions(ctx, repo.CountOpenSkillEditSuggestionsParams{
		ProjectID: *authCtx.ProjectID,
		SkillID:   skillID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count open skill suggestions").LogError(ctx, logger)
	}

	hasMore := len(rows) > payload.Limit
	if hasMore {
		rows = rows[:payload.Limit]
	}
	var nextCursor *string
	if hasMore {
		last := rows[len(rows)-1].SkillEditSuggestion
		encoded := encodeCreatedAtIDCursor(last.CreatedAt.Time, last.ID)
		nextCursor = &encoded
	}

	suggestionIDs := make([]uuid.UUID, len(rows))
	for i := range rows {
		suggestionIDs[i] = rows[i].SkillEditSuggestion.ID
	}
	changes, err := queries.ListSkillEditSuggestionChanges(ctx, repo.ListSkillEditSuggestionChangesParams{
		ProjectID:     *authCtx.ProjectID,
		SuggestionIds: suggestionIDs,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list skill suggestion changes").LogError(ctx, logger)
	}

	// Only suggestions that edit a supporting file need their base files to
	// show whether those edits still apply. The page's base versions are read
	// in one query.
	var baseVersionIDs []uuid.UUID
	for i := range rows {
		suggestion := rows[i].SkillEditSuggestion
		if editsSupportingFile(changes, suggestion.ID) && !slices.Contains(baseVersionIDs, suggestion.BaseVersionID) {
			baseVersionIDs = append(baseVersionIDs, suggestion.BaseVersionID)
		}
	}
	versionFiles, err := LoadSkillVersionFilesForVersions(ctx, queries, *authCtx.ProjectID, baseVersionIDs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load skill suggestion base files").LogError(ctx, logger)
	}
	baseFiles := make(map[uuid.UUID][]skilltree.File, len(baseVersionIDs))
	for i := range rows {
		suggestion := rows[i].SkillEditSuggestion
		if files, ok := versionFiles[suggestion.BaseVersionID]; ok && editsSupportingFile(changes, suggestion.ID) {
			baseFiles[suggestion.ID] = files
		}
	}

	return &gen.ListSkillSuggestionsResult{
		Suggestions:    mv.BuildSkillEditSuggestionListView(rows, changes, baseFiles),
		TotalOpenCount: totalOpenCount,
		NextCursor:     nextCursor,
	}, nil
}

// editsSupportingFile reports whether any of a suggestion's changes targets a
// file other than SKILL.md.
func editsSupportingFile(changes []repo.ListSkillEditSuggestionChangesRow, suggestionID uuid.UUID) bool {
	return slices.ContainsFunc(changes, func(change repo.ListSkillEditSuggestionChangesRow) bool {
		return change.SuggestionID == suggestionID && !skilldiff.IsManifestPath(change.Path.String)
	})
}

// takenSupportingFilePaths returns the distinct supporting-file paths edited by
// the changes an approval takes: those named in changeIDs, or every change
// when changeIDs is empty. Unknown IDs are ignored here; approvalContent
// reports them.
func takenSupportingFilePaths(changes []repo.ListSkillEditSuggestionChangesRow, changeIDs []uuid.UUID) []string {
	var paths []string
	for _, change := range changes {
		if skilldiff.IsManifestPath(change.Path.String) {
			continue
		}
		if len(changeIDs) > 0 && !slices.Contains(changeIDs, change.ID) {
			continue
		}
		if !slices.Contains(paths, change.Path.String) {
			paths = append(paths, change.Path.String)
		}
	}
	return paths
}

type suggestionApproval struct {
	suggestion repo.SkillEditSuggestion
	evidence   mv.SkillEditSuggestionEvidence
	version    *types.SkillVersion
	outcome    string
}

func (s *Service) approveSuggestion(
	ctx context.Context,
	authCtx *contextvalues.AuthContext,
	logger *slog.Logger,
	suggestionID uuid.UUID,
	editedContent *string,
	changeIDs []uuid.UUID,
	files supportingFiles,
) (approval suggestionApproval, err error) {
	if len(changeIDs) > 0 && editedContent != nil {
		return approval, oops.E(oops.CodeBadRequest, nil, "cannot take a subset of proposed changes and edited content together")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return approval, oops.E(oops.CodeUnexpected, err, "begin approve skill suggestion transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	details, err := queries.GetSkillEditSuggestionDetails(ctx, repo.GetSkillEditSuggestionDetailsParams{
		ProjectID: *authCtx.ProjectID,
		ID:        suggestionID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return approval, oops.E(oops.CodeNotFound, nil, "skill suggestion not found")
		}
		return approval, oops.E(oops.CodeUnexpected, err, "load skill suggestion for approval").LogError(ctx, logger)
	}
	approval = suggestionApproval{
		suggestion: details.SkillEditSuggestion,
		evidence: mv.SkillEditSuggestionEvidence{
			SkillName:            details.SkillName,
			SkillDisplayName:     details.SkillDisplayName,
			BaseContent:          details.BaseContent,
			BaseFiles:            nil,
			Changes:              nil,
			FeedbackCount:        details.FeedbackCount,
			FeedbackSessionCount: details.FeedbackSessionCount,
		},
		version: nil,
		outcome: "",
	}
	if details.SkillEditSuggestion.Status != string(EditSuggestionStatusOpen) {
		return approval, oops.E(oops.CodeConflict, nil, "skill suggestion is not open")
	}

	initialTree, err := LoadSuggestionTree(ctx, queries, *authCtx.ProjectID, details.SkillEditSuggestion.SkillID, details.SkillEditSuggestion.BaseVersionID, details.BaseContent)
	if err != nil {
		return approval, oops.E(oops.CodeUnexpected, err, "load skill suggestion base files for approval").LogError(ctx, logger)
	}
	approval.evidence.BaseFiles = initialTree.Files
	content := ""
	if editedContent != nil {
		content = *editedContent
	} else {
		changes, listErr := queries.ListSkillEditSuggestionChanges(ctx, repo.ListSkillEditSuggestionChangesParams{
			ProjectID: *authCtx.ProjectID, SuggestionIds: []uuid.UUID{suggestionID},
		})
		if listErr != nil {
			return approval, oops.E(oops.CodeUnexpected, listErr, "load skill suggestion changes for approval").LogError(ctx, logger)
		}
		approval.evidence.Changes = changes
		proposed, _, proposeErr := approvalContent(initialTree, changes, changeIDs)
		if proposeErr != nil {
			return approval, proposeErr
		}
		content = proposed.Manifest
	}
	initialParsed, err := parseSkillManifest(content)
	if err != nil {
		code := oops.CodeConflict
		if editedContent != nil {
			code = oops.CodeBadRequest
		}
		return approval, oops.E(code, nil, "%s", manifestErrorMessage(err))
	}
	if err := queries.LockSkillName(ctx, repo.LockSkillNameParams{
		ProjectID: *authCtx.ProjectID,
		Name:      initialParsed.Name,
	}); err != nil {
		return approval, oops.E(oops.CodeUnexpected, err, "lock skill suggestion name").LogError(ctx, logger)
	}
	skill, err := queries.GetSkillForUpdate(ctx, repo.GetSkillForUpdateParams{
		ProjectID: *authCtx.ProjectID,
		ID:        details.SkillEditSuggestion.SkillID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return approval, oops.E(oops.CodeConflict, nil, "skill is no longer available")
		}
		return approval, oops.E(oops.CodeUnexpected, err, "lock skill for suggestion approval").LogError(ctx, logger)
	}
	approval.evidence.SkillName = skill.Name
	approval.evidence.SkillDisplayName = skill.DisplayName
	suggestion, err := queries.GetSkillEditSuggestionForUpdate(ctx, repo.GetSkillEditSuggestionForUpdateParams{
		ProjectID: *authCtx.ProjectID,
		ID:        suggestionID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return approval, oops.E(oops.CodeConflict, nil, "skill suggestion is no longer available")
		}
		return approval, oops.E(oops.CodeUnexpected, err, "re-read skill suggestion for approval").LogError(ctx, logger)
	}
	approval.suggestion = suggestion
	if suggestion.Status != string(EditSuggestionStatusOpen) {
		return approval, oops.E(oops.CodeConflict, nil, "skill suggestion is not open")
	}
	if !suggestion.UpdatedAt.Time.Equal(details.SkillEditSuggestion.UpdatedAt.Time) {
		return approval, oops.E(oops.CodeConflict, nil, "skill suggestion changed during approval")
	}
	base, err := queries.ResolveSkillSuggestionBase(ctx, repo.ResolveSkillSuggestionBaseParams{
		ProjectID: *authCtx.ProjectID,
		SkillID:   suggestion.SkillID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return approval, oops.E(oops.CodeConflict, nil, "skill has no approvable base version")
		}
		return approval, oops.E(oops.CodeUnexpected, err, "resolve skill suggestion base for approval").LogError(ctx, logger)
	}
	approval.evidence.BaseContent = base.BaseContent
	baseTree, err := LoadSuggestionTree(ctx, queries, *authCtx.ProjectID, suggestion.SkillID, base.BaseVersionID, base.BaseContent)
	if err != nil {
		return approval, oops.E(oops.CodeUnexpected, err, "load skill base files for approval").LogError(ctx, logger)
	}
	approval.evidence.BaseFiles = baseTree.Files
	if suggestion.BaseVersionID != base.BaseVersionID {
		superseded, supersedeErr := queries.SupersedeOpenSkillEditSuggestionByID(ctx, repo.SupersedeOpenSkillEditSuggestionByIDParams{
			ProjectID: *authCtx.ProjectID,
			SkillID:   suggestion.SkillID,
			ID:        suggestion.ID,
		})
		if supersedeErr != nil {
			return approval, oops.E(oops.CodeUnexpected, supersedeErr, "supersede stale skill suggestion").LogError(ctx, logger)
		}
		if err := dbtx.Commit(ctx); err != nil {
			return approval, oops.E(oops.CodeUnexpected, err, "commit stale skill suggestion supersession").LogError(ctx, logger)
		}
		approval.suggestion = superseded
		approval.outcome = "superseded"
		return approval, nil
	}

	changes, err := queries.ListSkillEditSuggestionChanges(ctx, repo.ListSkillEditSuggestionChangesParams{
		ProjectID: *authCtx.ProjectID, SuggestionIds: []uuid.UUID{suggestion.ID},
	})
	if err != nil {
		return approval, oops.E(oops.CodeUnexpected, err, "load skill suggestion changes for approval").LogError(ctx, logger)
	}
	approval.evidence.Changes = changes
	// A reviewer's edit replaces SKILL.md only. Without an explicit file list,
	// the suggestion's changes to supporting files still apply on top of it,
	// so approving an edited suggestion does not silently drop them.
	proposedTree := skilldiff.Tree{Manifest: content, Files: baseTree.Files}
	var remaining []repo.ListSkillEditSuggestionChangesRow
	if editedContent != nil && !files.explicit {
		proposedTree, err = applySupportingFileChanges(proposedTree, changes)
		if err != nil {
			return approval, err
		}
	}
	if editedContent == nil {
		// An explicit file list would silently discard a taken edit to a
		// supporting file, so the two cannot be combined.
		if conflicting := takenSupportingFilePaths(changes, changeIDs); files.explicit && len(conflicting) > 0 {
			return approval, oops.E(oops.CodeBadRequest, nil,
				"files cannot be sent while taking changes to supporting files (%s); approve without files so the suggested edits apply, or leave those changes out of change_ids",
				strings.Join(conflicting, ", "))
		}
		proposedTree, remaining, err = approvalContent(baseTree, changes, changeIDs)
		if err != nil {
			return approval, err
		}
	}

	// Files the reviewer sends replace the version's supporting files
	// outright; they only reach here alongside SKILL.md changes or edited
	// content. Otherwise the version keeps the base version's files with the
	// taken changes applied.
	if files.explicit {
		proposedTree.Files = entryFiles(files.entries)
	}
	rejectCode := oops.CodeConflict
	if editedContent != nil || files.explicit {
		rejectCode = oops.CodeBadRequest
	}
	// The no-op check compares whole skill trees, so an edit to a supporting
	// file alone is a real change.
	validated, err := ValidateSkillSuggestion(proposedTree, skill.Name, base.BaseCanonicalSha256)
	if err != nil {
		return approval, oops.E(rejectCode, nil, "%s", err.Error())
	}
	parsed, err := parseSkillManifest(validated.Content)
	if err != nil {
		return approval, oops.E(oops.CodeUnexpected, err, "parse validated skill suggestion").LogError(ctx, logger)
	}
	parsed, err = parsed.withSupportingFiles(validated.Files)
	if err != nil {
		return approval, oops.E(oops.CodeUnexpected, err, "digest approved skill files").LogError(ctx, logger)
	}

	// Taking a subset leaves the suggestion open carrying the rest. Writing
	// the remainder before the version exists lets recordVersion's replay
	// repoint it at the version this creates; closing it first, as a whole
	// approval does, is what keeps that replay off an approval's own work.
	var approved repo.SkillEditSuggestion
	if len(remaining) > 0 {
		// Drop only the changes being taken. The rest keep their own rationale
		// and evidence, and recordVersion's replay rebases them onto the
		// version this creates.
		if err := queries.DeleteSkillEditSuggestionChangesByIDs(ctx, repo.DeleteSkillEditSuggestionChangesByIDsParams{
			ProjectID: *authCtx.ProjectID, Ids: changeIDs,
		}); err != nil {
			return approval, oops.E(oops.CodeUnexpected, err, "drop the approved skill suggestion changes").LogError(ctx, logger)
		}
		approved = suggestion
	} else {
		approved, err = queries.ApproveOpenSkillEditSuggestion(ctx, repo.ApproveOpenSkillEditSuggestionParams{
			ApprovedByUserID: conv.ToPGText(authCtx.UserID),
			ProjectID:        *authCtx.ProjectID,
			SkillID:          suggestion.SkillID,
			ID:               suggestion.ID,
		})
		if err != nil {
			return approval, oops.E(oops.CodeUnexpected, err, "mark skill suggestion approved").LogError(ctx, logger)
		}
	}
	recorded, _, err := s.recordVersion(
		ctx,
		dbtx,
		queries,
		authCtx,
		logger,
		skill,
		parsed,
		false,
		false,
		uuid.NullUUID{UUID: suggestion.BaseVersionID, Valid: true},
	)
	if err != nil {
		return approval, err
	}
	if !recorded.CreatedVersion {
		return approval, oops.E(oops.CodeConflict, nil, "suggested content already exists as a skill version")
	}
	resultingVersionID := uuid.MustParse(recorded.Version.ID)
	if err := s.audit.LogSkillSuggestionApprove(ctx, dbtx, audit.LogSkillSuggestionEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		SkillURN:         urn.NewSkill(skill.ID),
		SkillName:        skill.Name,
		SkillDisplayName: skill.DisplayName,
		SuggestionMetadata: audit.SkillSuggestionMetadata{
			SuggestionID:       suggestion.ID,
			BaseVersionID:      suggestion.BaseVersionID,
			ResultingVersionID: uuid.NullUUID{UUID: resultingVersionID, Valid: true},
			Edited:             editedContent != nil,
		},
	}); err != nil {
		return approval, oops.E(oops.CodeUnexpected, err, "log skill suggestion approval").LogError(ctx, logger)
	}
	approval.outcome = "applied"
	if len(remaining) > 0 {
		// Re-read what the replay left behind so the reviewer sees the
		// remaining changes against the version they just created.
		approved, err = queries.GetOpenSkillEditSuggestion(ctx, repo.GetOpenSkillEditSuggestionParams{
			ProjectID: *authCtx.ProjectID,
			SkillID:   suggestion.SkillID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// The replay found nothing left to propose and retired it.
		case err != nil:
			return approval, oops.E(oops.CodeUnexpected, err, "re-read remaining skill suggestion").LogError(ctx, logger)
		default:
			approval.evidence.BaseContent = validated.Content
			approval.evidence.BaseFiles = proposedTree.Files
			approval.evidence.Changes, err = queries.ListSkillEditSuggestionChanges(ctx, repo.ListSkillEditSuggestionChangesParams{
				ProjectID: *authCtx.ProjectID, SuggestionIds: []uuid.UUID{approved.ID},
			})
			if err != nil {
				return approval, oops.E(oops.CodeUnexpected, err, "re-read remaining skill suggestion changes").LogError(ctx, logger)
			}
			approval.outcome = "partially_applied"
		}
	}
	if err := lockPluginPackageContent(ctx, dbtx, *authCtx.ProjectID); err != nil {
		return approval, oops.E(oops.CodeUnexpected, err, "lock approved skill package content").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return approval, oops.E(oops.CodeUnexpected, err, "commit skill suggestion approval").LogError(ctx, logger)
	}

	approval.suggestion = approved
	approval.version = recorded.Version
	return approval, nil
}

// applySupportingFileChanges applies a suggestion's changes to files other
// than SKILL.md onto tree, which carries the reviewer's edited SKILL.md. A
// change that no longer applies is a conflict rather than a silent drop.
func applySupportingFileChanges(tree skilldiff.Tree, changes []repo.ListSkillEditSuggestionChangesRow) (skilldiff.Tree, error) {
	for _, change := range changes {
		if skilldiff.IsManifestPath(change.Path.String) {
			continue
		}
		applied, err := tree.ApplyChange(change.Path.String, change.ProposedDiff)
		if err != nil {
			return skilldiff.Tree{}, oops.E(oops.CodeConflict, nil,
				"the suggested change to %s no longer applies; approve the edited SKILL.md with files to set the supporting files yourself", change.Path.String)
		}
		tree = applied
	}
	return tree, nil
}

// approvalContent resolves what a reviewer is taking from a suggestion: every
// proposed change, or a subset with the rest left to propose. Each change
// applies to the file it names, as the changes before it left that file, so a
// subset replays only the taken edits, in their proposed order, onto the
// current version. Binary files pass through untouched; a change aimed at one
// is a conflict.
func approvalContent(
	base skilldiff.Tree,
	changes []repo.ListSkillEditSuggestionChangesRow,
	changeIDs []uuid.UUID,
) (skilldiff.Tree, []repo.ListSkillEditSuggestionChangesRow, error) {
	if len(changes) == 0 {
		return skilldiff.Tree{}, nil, oops.E(oops.CodeConflict, nil, "skill suggestion no longer proposes any changes")
	}

	requested := make(map[uuid.UUID]struct{}, len(changeIDs))
	for _, id := range changeIDs {
		requested[id] = struct{}{}
	}

	taken := changes
	var remaining []repo.ListSkillEditSuggestionChangesRow
	if len(changeIDs) > 0 {
		taken = make([]repo.ListSkillEditSuggestionChangesRow, 0, len(requested))
		remaining = make([]repo.ListSkillEditSuggestionChangesRow, 0, len(changes))
		for _, change := range changes {
			if _, ok := requested[change.ID]; ok {
				taken = append(taken, change)
				delete(requested, change.ID)
				continue
			}
			remaining = append(remaining, change)
		}
		if len(requested) > 0 {
			return skilldiff.Tree{}, nil, oops.E(oops.CodeConflict, nil, "a selected proposed change is no longer part of this suggestion")
		}
	}

	tree := base
	for _, change := range taken {
		applied, err := tree.ApplyChange(change.Path.String, change.ProposedDiff)
		if err != nil {
			if len(changeIDs) > 0 {
				return skilldiff.Tree{}, nil, oops.E(oops.CodeConflict, nil, "a selected proposed change no longer applies to the current skill")
			}
			return skilldiff.Tree{}, nil, oops.E(oops.CodeConflict, nil, "skill suggestion no longer applies to its base version")
		}
		tree = applied
	}

	return tree, remaining, nil
}

func (s *Service) ApproveSuggestion(ctx context.Context, payload *gen.ApproveSuggestionPayload) (*gen.ApproveSkillSuggestionResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogProjectID(authCtx.ProjectID.String()),
	)
	if err := s.requireAccess(ctx, authCtx, authz.SkillCollection.Write); err != nil {
		return nil, err
	}
	suggestionID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill suggestion id")
	}

	// Duplicates are harmless: approvalContent resolves membership through a
	// set and the taken changes are deleted with an ANY match.
	changeIDs := make([]uuid.UUID, 0, len(payload.ChangeIds))
	for _, value := range payload.ChangeIds {
		parsed, parseErr := uuid.Parse(value)
		if parseErr != nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill suggestion change id")
		}
		changeIDs = append(changeIDs, parsed)
	}

	files, err := validateSupportingFiles(payload.Files)
	if err != nil {
		return nil, err
	}

	approval, err := s.approveSuggestion(ctx, authCtx, logger, suggestionID, payload.Content, changeIDs, files)
	if err != nil {
		return nil, err
	}
	if approval.version != nil {
		s.signalPluginPublishForSkills(ctx, authCtx, []uuid.UUID{approval.suggestion.SkillID}, true)
	}
	return &gen.ApproveSkillSuggestionResult{
		Suggestion: mv.BuildSkillEditSuggestionView(approval.suggestion, approval.evidence),
		Outcome:    approval.outcome,
		Version:    approval.version,
	}, nil
}

func (s *Service) DismissSuggestion(ctx context.Context, payload *gen.DismissSuggestionPayload) (*types.SkillEditSuggestion, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogProjectID(authCtx.ProjectID.String()),
	)
	if err := s.requireAccess(ctx, authCtx, authz.SkillCollection.Write); err != nil {
		return nil, err
	}
	suggestionID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill suggestion id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin dismiss skill suggestion transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	details, err := queries.GetSkillEditSuggestionDetails(ctx, repo.GetSkillEditSuggestionDetailsParams{
		ProjectID: *authCtx.ProjectID,
		ID:        suggestionID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, nil, "skill suggestion not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "load skill suggestion for dismissal").LogError(ctx, logger)
	}
	skill, err := queries.GetSkillForUpdate(ctx, repo.GetSkillForUpdateParams{
		ProjectID: *authCtx.ProjectID,
		ID:        details.SkillEditSuggestion.SkillID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeConflict, nil, "skill is no longer available")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock skill for suggestion dismissal").LogError(ctx, logger)
	}
	suggestion, err := queries.GetSkillEditSuggestionForUpdate(ctx, repo.GetSkillEditSuggestionForUpdateParams{
		ProjectID: *authCtx.ProjectID,
		ID:        suggestionID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeConflict, nil, "skill suggestion is no longer available")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock skill suggestion for dismissal").LogError(ctx, logger)
	}
	changes, err := queries.ListSkillEditSuggestionChanges(ctx, repo.ListSkillEditSuggestionChangesParams{
		ProjectID: *authCtx.ProjectID, SuggestionIds: []uuid.UUID{suggestion.ID},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load skill suggestion changes for dismissal").LogError(ctx, logger)
	}
	baseFiles, err := LoadSkillVersionFiles(ctx, queries, *authCtx.ProjectID, suggestion.SkillID, suggestion.BaseVersionID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load skill suggestion base files for dismissal").LogError(ctx, logger)
	}
	evidence := mv.SkillEditSuggestionEvidence{
		SkillName:            skill.Name,
		SkillDisplayName:     skill.DisplayName,
		BaseContent:          details.BaseContent,
		BaseFiles:            baseFiles,
		Changes:              changes,
		FeedbackCount:        details.FeedbackCount,
		FeedbackSessionCount: details.FeedbackSessionCount,
	}
	if suggestion.Status == string(EditSuggestionStatusDismissed) {
		return mv.BuildSkillEditSuggestionView(suggestion, evidence), nil
	}
	if suggestion.Status != string(EditSuggestionStatusOpen) {
		return nil, oops.E(oops.CodeConflict, nil, "skill suggestion cannot be dismissed")
	}
	dismissed, err := queries.DismissSkillEditSuggestion(ctx, repo.DismissSkillEditSuggestionParams{
		ProjectID: *authCtx.ProjectID,
		SkillID:   suggestion.SkillID,
		ID:        suggestion.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeConflict, nil, "skill suggestion is no longer open")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "dismiss skill suggestion").LogError(ctx, logger)
	}
	if err := s.audit.LogSkillSuggestionDismiss(ctx, dbtx, audit.LogSkillSuggestionEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		SkillURN:         urn.NewSkill(suggestion.SkillID),
		SkillName:        skill.Name,
		SkillDisplayName: skill.DisplayName,
		SuggestionMetadata: audit.SkillSuggestionMetadata{
			SuggestionID:       suggestion.ID,
			BaseVersionID:      suggestion.BaseVersionID,
			ResultingVersionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			Edited:             false,
		},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log skill suggestion dismissal").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit skill suggestion dismissal").LogError(ctx, logger)
	}
	return mv.BuildSkillEditSuggestionView(dismissed, evidence), nil
}

func (s *Service) ApproveAllSuggestions(ctx context.Context, payload *gen.ApproveAllSuggestionsPayload) (*gen.ApproveAllSkillSuggestionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogProjectID(authCtx.ProjectID.String()),
	)
	if err := s.requireAccess(ctx, authCtx, authz.SkillCollection.Write); err != nil {
		return nil, err
	}
	suggestionIDs := make([]uuid.UUID, 0, len(payload.SuggestionIds))
	seen := make(map[uuid.UUID]struct{}, len(payload.SuggestionIds))
	for _, value := range payload.SuggestionIds {
		id, err := uuid.Parse(value)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "suggestion_ids must contain UUIDs")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		suggestionIDs = append(suggestionIDs, id)
	}
	snapshot, err := repo.New(s.db).ListOpenSkillEditSuggestionsForApproval(ctx, repo.ListOpenSkillEditSuggestionsForApprovalParams{
		ProjectID:     *authCtx.ProjectID,
		SuggestionIds: suggestionIDs,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "snapshot open skill suggestions").LogError(ctx, logger)
	}

	items := make([]*gen.SkillSuggestionApprovalItem, 0, len(snapshot))
	// Each approval commits on its own; the republish waits for the batch so the
	// project is checked and signalled once rather than once per suggestion.
	versionedSkillIDs := make([]uuid.UUID, 0, len(snapshot))
	for _, suggestion := range snapshot {
		approval, approveErr := s.approveSuggestion(ctx, authCtx, logger, suggestion.ID, nil, nil, supportingFiles{explicit: false, entries: nil})
		item := &gen.SkillSuggestionApprovalItem{
			SuggestionID:       suggestion.ID.String(),
			SkillID:            suggestion.SkillID.String(),
			SkillName:          suggestion.SkillName,
			SkillDisplayName:   suggestion.SkillDisplayName,
			Outcome:            approval.outcome,
			ResultingVersionID: nil,
			Message:            nil,
		}
		if approval.evidence.SkillName != "" {
			item.SkillID = approval.suggestion.SkillID.String()
			item.SkillName = approval.evidence.SkillName
			item.SkillDisplayName = approval.evidence.SkillDisplayName
		}
		if approveErr != nil {
			var shareable *oops.ShareableError
			if errors.As(approveErr, &shareable) && shareable.Code != oops.CodeUnexpected {
				item.Outcome = "conflict"
				item.Message = conv.PtrEmpty(shareable.Error())
			} else {
				item.Outcome = "failed"
				item.Message = conv.PtrEmpty("suggestion approval failed")
			}
		} else if approval.version != nil {
			item.ResultingVersionID = conv.PtrEmpty(approval.version.ID)
			versionedSkillIDs = append(versionedSkillIDs, approval.suggestion.SkillID)
		}
		items = append(items, item)
	}
	s.signalPluginPublishForSkills(ctx, authCtx, versionedSkillIDs, true)

	return &gen.ApproveAllSkillSuggestionsResult{Items: items}, nil
}

func (s *Service) ListSuggestionFeedback(ctx context.Context, payload *gen.ListSuggestionFeedbackPayload) (*gen.ListSkillSuggestionFeedbackResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogProjectID(authCtx.ProjectID.String()),
	)
	if err := s.requireAccess(ctx, authCtx, authz.SkillCollection.Read); err != nil {
		return nil, err
	}
	if payload.Limit < 1 || payload.Limit > 50 {
		return nil, oops.E(oops.CodeBadRequest, nil, "skill suggestion feedback limit must be between 1 and 50")
	}
	changeID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill suggestion change id")
	}

	queries := repo.New(s.db)
	if _, err := queries.GetSkillEditSuggestionChange(ctx, repo.GetSkillEditSuggestionChangeParams{
		ProjectID: *authCtx.ProjectID,
		ID:        changeID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, nil, "proposed change not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get proposed change for feedback").LogError(ctx, logger)
	}
	rows, err := queries.ListSkillEditSuggestionFeedback(ctx, repo.ListSkillEditSuggestionFeedbackParams{
		ProjectID: *authCtx.ProjectID,
		ChangeID:  changeID,
		PageLimit: conv.SafeInt32(payload.Limit),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list proposed change feedback").LogError(ctx, logger)
	}

	return &gen.ListSkillSuggestionFeedbackResult{Feedback: mv.BuildSkillFeedbackListView(rows)}, nil
}
