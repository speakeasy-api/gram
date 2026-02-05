package resolution_activities

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
)

type GetUserFeedbackMessageID struct {
	repo *repo.Queries
}

func NewGetUserFeedbackMessageID(db *pgxpool.Pool) *GetUserFeedbackMessageID {
	return &GetUserFeedbackMessageID{
		repo: repo.New(db),
	}
}

type GetUserFeedbackMessageIDArgs struct {
	ChatID uuid.UUID
}

type GetUserFeedbackMessageIDResult struct {
	MessageID uuid.UUID
	HasFeedback bool
}

func (g *GetUserFeedbackMessageID) Do(ctx context.Context, args GetUserFeedbackMessageIDArgs) (*GetUserFeedbackMessageIDResult, error) {
	messageID, err := g.repo.GetUserFeedbackMessageID(ctx, args.ChatID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No user feedback exists
			return &GetUserFeedbackMessageIDResult{
				MessageID:   uuid.UUID{},
				HasFeedback: false,
			}, nil
		}
		return nil, fmt.Errorf("failed to get user feedback message ID: %w", err)
	}

	if !messageID.Valid {
		// User feedback exists but has no message ID (shouldn't happen but handle it)
		return &GetUserFeedbackMessageIDResult{
			MessageID:   uuid.UUID{},
			HasFeedback: false,
		}, nil
	}

	return &GetUserFeedbackMessageIDResult{
		MessageID:   messageID.UUID,
		HasFeedback: true,
	}, nil
}
