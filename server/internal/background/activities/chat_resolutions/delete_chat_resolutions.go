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

type DeleteChatResolutionsAfterFeedbackArgs struct {
	ChatID                   uuid.UUID
	UserFeedbackMessageID    uuid.UUID
	HasUserFeedback          bool
}

func (d *DeleteChatResolutions) Do(ctx context.Context, args DeleteChatResolutionsArgs) error {
	if err := d.repo.DeleteChatResolutions(ctx, args.ChatID); err != nil {
		return fmt.Errorf("failed to delete chat resolutions: %w", err)
	}
	return nil
}

// DoAfterFeedback deletes only resolutions that came after the user feedback message.
// This preserves the user's feedback while allowing re-analysis of subsequent messages.
func (d *DeleteChatResolutions) DoAfterFeedback(ctx context.Context, args DeleteChatResolutionsAfterFeedbackArgs) error {
	if !args.HasUserFeedback {
		// No user feedback exists, delete all resolutions
		if err := d.repo.DeleteChatResolutions(ctx, args.ChatID); err != nil {
			return fmt.Errorf("failed to delete chat resolutions: %w", err)
		}
		return nil
	}

	// Delete only resolutions after the user feedback message
	if err := d.repo.DeleteChatResolutionsAfterMessage(ctx, repo.DeleteChatResolutionsAfterMessageParams{
		ChatID:         args.ChatID,
		AfterMessageID: args.UserFeedbackMessageID,
	}); err != nil {
		return fmt.Errorf("failed to delete chat resolutions after message: %w", err)
	}
	return nil
}
