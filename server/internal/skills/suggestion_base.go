package skills

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func supersedeOpenSuggestionAfterBaseChange(ctx context.Context, queries *repo.Queries, projectID, skillID uuid.UUID) error {
	base, err := queries.ResolveSkillSuggestionBase(ctx, repo.ResolveSkillSuggestionBaseParams{ProjectID: projectID, SkillID: skillID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("resolve skill suggestion base after version change: %w", err)
	}
	if _, err := queries.SupersedeOpenSkillEditSuggestion(ctx, repo.SupersedeOpenSkillEditSuggestionParams{
		ProjectID: projectID, SkillID: skillID, CurrentBaseVersionID: base.BaseVersionID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("supersede skill suggestion after version change: %w", err)
	}
	return nil
}
