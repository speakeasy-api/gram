package resolution_activities

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
)

type DeleteChatResolutions struct {
	repo *repo.Queries
}

func NewDeleteChatResolutions(db *pgxpool.Pool) *DeleteChatResolutions {
	return &DeleteChatResolutions{
		repo: repo.New(db),
	}
}

type DeleteChatResolutionsArgs struct {
	ChatID uuid.UUID
}

func (d *DeleteChatResolutions) Do(ctx context.Context, args DeleteChatResolutionsArgs) error {
	if err := d.repo.DeleteChatResolutions(ctx, args.ChatID); err != nil {
		return fmt.Errorf("failed to delete chat resolutions: %w", err)
	}
	return nil
}
