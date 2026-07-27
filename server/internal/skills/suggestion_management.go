package skills

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/skilldiff"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (s *Service) ListSuggestions(ctx context.Context, payload *gen.ListSuggestionsPayload) (*gen.ListSkillSuggestionsResult, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeSkillRead)
	if err != nil {
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

	return &gen.ListSkillSuggestionsResult{
		Suggestions:    mv.BuildSkillEditSuggestionListView(rows, changes),
		TotalOpenCount: totalOpenCount,
		NextCursor:     nextCursor,
	}, nil
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
	changeID *uuid.UUID,
) (approval suggestionApproval, err error) {
	if changeID != nil && editedContent != nil {
		return approval, oops.E(oops.CodeBadRequest, nil, "cannot take a single proposed change and edited content together")
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
		content, _, err = approvalContent(details.BaseContent, changes, changeID)
		if err != nil {
			return approval, err
		}
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
	var remaining []repo.ListSkillEditSuggestionChangesRow
	if editedContent == nil {
		content, remaining, err = approvalContent(base.BaseContent, changes, changeID)
		if err != nil {
			return approval, err
		}
	}

	validated, err := ValidateSkillSuggestion(content, skill.Name, base.BaseCanonicalSha256)
	if err != nil {
		code := oops.CodeConflict
		if editedContent != nil {
			code = oops.CodeBadRequest
		}
		return approval, oops.E(code, nil, "%s", err.Error())
	}
	parsed, err := parseSkillManifest(validated.Content)
	if err != nil {
		return approval, oops.E(oops.CodeUnexpected, err, "parse validated skill suggestion").LogError(ctx, logger)
	}

	// Taking one change leaves the suggestion open carrying the rest. Writing
	// the remainder before the version exists lets recordVersion's replay
	// repoint it at the version this creates; closing it first, as a whole
	// approval does, is what keeps that replay off an approval's own work.
	var approved repo.SkillEditSuggestion
	if len(remaining) > 0 {
		// Drop only the change being taken. The rest keep their own rationale
		// and evidence, and recordVersion's replay rebases them onto the
		// version this creates.
		if err := queries.DeleteSkillEditSuggestionChange(ctx, repo.DeleteSkillEditSuggestionChangeParams{
			ProjectID: *authCtx.ProjectID, ID: *changeID,
		}); err != nil {
			return approval, oops.E(oops.CodeUnexpected, err, "drop the approved skill suggestion change").LogError(ctx, logger)
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
	recorded, err := s.recordVersion(
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
			approval.evidence.Changes, err = queries.ListSkillEditSuggestionChanges(ctx, repo.ListSkillEditSuggestionChangesParams{
				ProjectID: *authCtx.ProjectID, SuggestionIds: []uuid.UUID{approved.ID},
			})
			if err != nil {
				return approval, oops.E(oops.CodeUnexpected, err, "re-read remaining skill suggestion changes").LogError(ctx, logger)
			}
			approval.outcome = "partially_applied"
		}
	}
	if err := dbtx.Commit(ctx); err != nil {
		return approval, oops.E(oops.CodeUnexpected, err, "commit skill suggestion approval").LogError(ctx, logger)
	}

	approval.suggestion = approved
	approval.version = recorded.Version
	return approval, nil
}

// approvalContent resolves what a reviewer is taking from a suggestion: every
// proposed change, or a single one with the rest left to propose. Each change
// applies to what the changes before it produce, so taking one on its own
// replays only that edit onto the current manifest.
func approvalContent(
	baseContent string,
	changes []repo.ListSkillEditSuggestionChangesRow,
	changeID *uuid.UUID,
) (string, []repo.ListSkillEditSuggestionChangesRow, error) {
	if len(changes) == 0 {
		return "", nil, oops.E(oops.CodeConflict, nil, "skill suggestion no longer proposes any changes")
	}

	if changeID == nil {
		content := baseContent
		for _, change := range changes {
			applied, err := skilldiff.Apply(content, change.ProposedDiff)
			if err != nil {
				return "", nil, oops.E(oops.CodeConflict, nil, "skill suggestion no longer applies to its base version")
			}
			content = applied
		}

		return content, nil, nil
	}

	var taken *repo.ListSkillEditSuggestionChangesRow
	remaining := make([]repo.ListSkillEditSuggestionChangesRow, 0, len(changes))
	for i, change := range changes {
		if change.ID == *changeID {
			taken = &changes[i]
			continue
		}
		remaining = append(remaining, change)
	}
	if taken == nil {
		return "", nil, oops.E(oops.CodeConflict, nil, "that proposed change is no longer part of this suggestion")
	}

	content, err := skilldiff.Apply(baseContent, taken.ProposedDiff)
	if err != nil {
		return "", nil, oops.E(oops.CodeConflict, nil, "that proposed change no longer applies to the current skill")
	}

	return content, remaining, nil
}

func (s *Service) ApproveSuggestion(ctx context.Context, payload *gen.ApproveSuggestionPayload) (*gen.ApproveSkillSuggestionResult, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeSkillWrite)
	if err != nil {
		return nil, err
	}
	suggestionID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill suggestion id")
	}

	var changeID *uuid.UUID
	if payload.ChangeID != nil {
		parsed, parseErr := uuid.Parse(*payload.ChangeID)
		if parseErr != nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill suggestion change id")
		}
		changeID = &parsed
	}

	approval, err := s.approveSuggestion(ctx, authCtx, logger, suggestionID, payload.Content, changeID)
	if err != nil {
		return nil, err
	}
	return &gen.ApproveSkillSuggestionResult{
		Suggestion: mv.BuildSkillEditSuggestionView(approval.suggestion, approval.evidence),
		Outcome:    approval.outcome,
		Version:    approval.version,
	}, nil
}

func (s *Service) DismissSuggestion(ctx context.Context, payload *gen.DismissSuggestionPayload) (*types.SkillEditSuggestion, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeSkillWrite)
	if err != nil {
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
	evidence := mv.SkillEditSuggestionEvidence{
		SkillName:            skill.Name,
		SkillDisplayName:     skill.DisplayName,
		BaseContent:          details.BaseContent,
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

func (s *Service) ApproveAllSuggestions(ctx context.Context, _ *gen.ApproveAllSuggestionsPayload) (*gen.ApproveAllSkillSuggestionsResult, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeSkillWrite)
	if err != nil {
		return nil, err
	}
	snapshot, err := repo.New(s.db).ListOpenSkillEditSuggestionsForApproval(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "snapshot open skill suggestions").LogError(ctx, logger)
	}

	items := make([]*gen.SkillSuggestionApprovalItem, 0, len(snapshot))
	for _, suggestion := range snapshot {
		approval, approveErr := s.approveSuggestion(ctx, authCtx, logger, suggestion.ID, nil, nil)
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
		}
		items = append(items, item)
	}

	return &gen.ApproveAllSkillSuggestionsResult{Items: items}, nil
}
