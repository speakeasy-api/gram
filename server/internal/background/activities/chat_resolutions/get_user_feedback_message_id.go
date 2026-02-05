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
	MessageIDs  []uuid.UUID
	HasFeedback bool
}

func (g *GetUserFeedbackMessageID) Do(ctx context.Context, args GetUserFeedbackMessageIDArgs) (*GetUserFeedbackMessageIDResult, error) {
	nullableMessageIDs, err := g.repo.ListUserFeedbackMessageIDs(ctx, args.ChatID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No user feedback exists
			return &GetUserFeedbackMessageIDResult{
				MessageIDs:  []uuid.UUID{},
				HasFeedback: false,
			}, nil
		}
		return nil, fmt.Errorf("failed to list user feedback message IDs: %w", err)
	}

	if len(nullableMessageIDs) == 0 {
		return &GetUserFeedbackMessageIDResult{
			MessageIDs:  []uuid.UUID{},
			HasFeedback: false,
		}, nil
	}

	// Convert NullUUID to UUID, filtering out invalid entries
	messageIDs := make([]uuid.UUID, 0, len(nullableMessageIDs))
	for _, nullableID := range nullableMessageIDs {
		if nullableID.Valid {
			messageIDs = append(messageIDs, nullableID.UUID)
		}
	}

	if len(messageIDs) == 0 {
		return &GetUserFeedbackMessageIDResult{
			MessageIDs:  []uuid.UUID{},
			HasFeedback: false,
		}, nil
	}

	return &GetUserFeedbackMessageIDResult{
		MessageIDs:  messageIDs,
		HasFeedback: true,
	}, nil
}
