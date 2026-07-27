package skills

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/skilldiff"
)

// ReplayOpenSuggestionOntoBase repoints an open suggestion at the skill's
// current base version. Because a suggestion stores a diff rather than a whole
// manifest, it survives edits it does not overlap; it is superseded only when
// its diff no longer applies or the newer version already carries the edit.
func ReplayOpenSuggestionOntoBase(ctx context.Context, queries *repo.Queries, projectID, skillID uuid.UUID) error {
	base, err := queries.ResolveSkillSuggestionBase(ctx, repo.ResolveSkillSuggestionBaseParams{ProjectID: projectID, SkillID: skillID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("resolve skill suggestion base after version change: %w", err)
	}

	open, err := queries.GetOpenSkillEditSuggestion(ctx, repo.GetOpenSkillEditSuggestionParams{ProjectID: projectID, SkillID: skillID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("get open skill suggestion for replay: %w", err)
	case open.BaseVersionID == base.BaseVersionID:
		return nil
	}

	replayed, err := replayDiff(base.BaseContent, open.ProposedDiff)
	switch {
	case err != nil:
		return err
	case replayed != "":
		if _, err := queries.RebaseOpenSkillEditSuggestion(ctx, repo.RebaseOpenSkillEditSuggestionParams{
			BaseVersionID: base.BaseVersionID, ProposedDiff: replayed,
			ProjectID: projectID, SkillID: skillID, ID: open.ID,
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("replay open skill suggestion onto current version: %w", err)
		}

		return nil
	}

	if _, err := queries.SupersedeOpenSkillEditSuggestion(ctx, repo.SupersedeOpenSkillEditSuggestionParams{
		ProjectID: projectID, SkillID: skillID, CurrentBaseVersionID: base.BaseVersionID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("supersede skill suggestion after version change: %w", err)
	}

	return nil
}

// replayDiff re-renders a stored diff against newer content. It returns an
// empty diff when the suggestion has nothing left to propose, either because
// the edit conflicts with the newer content or because it is already applied.
func replayDiff(baseContent, proposedDiff string) (string, error) {
	proposed, err := skilldiff.Apply(baseContent, proposedDiff)
	if errors.Is(err, skilldiff.ErrConflict) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("replay open skill suggestion: %w", err)
	}

	replayed, err := skilldiff.Unified(baseContent, proposed)
	if err != nil {
		return "", fmt.Errorf("re-render replayed skill suggestion: %w", err)
	}
	if strings.TrimSpace(replayed) == "" {
		return "", nil
	}

	return replayed, nil
}
