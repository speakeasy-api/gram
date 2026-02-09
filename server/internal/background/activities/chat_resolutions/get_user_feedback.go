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

type GetUserFeedbackForChat struct {
	repo *repo.Queries
}

func NewGetUserFeedbackForChat(db *pgxpool.Pool) *GetUserFeedbackForChat {
	return &GetUserFeedbackForChat{
		repo: repo.New(db),
	}
}

type GetUserFeedbackForChatArgs struct {
	ProjectID uuid.UUID
	ChatID    uuid.UUID
}

type UserFeedback struct {
	ID              uuid.UUID
	MessageID       uuid.UUID
	MessageIndex    int
	Resolution      string
	ResolutionNotes string
}

type GetUserFeedbackForChatResult struct {
	UserFeedback []UserFeedback
}

func (g *GetUserFeedbackForChat) Do(ctx context.Context, args GetUserFeedbackForChatArgs) (*GetUserFeedbackForChatResult, error) {
	feedback, err := g.repo.ListUserFeedbackForChat(ctx, args.ChatID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No user feedback exists
			return &GetUserFeedbackForChatResult{
				UserFeedback: []UserFeedback{},
			}, nil
		}
		return nil, fmt.Errorf("failed to list user feedback message IDs: %w", err)
	}

	messages, err := g.repo.ListChatMessages(ctx, repo.ListChatMessagesParams{
		ChatID:    args.ChatID,
		ProjectID: args.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list chat messages: %w", err)
	}

	// Map feedback message IDs to their indices
	messageIndexMap := make(map[uuid.UUID]int)
	for i, msg := range messages {
		messageIndexMap[msg.ID] = i
	}

	userFeedback := make([]UserFeedback, 0, len(feedback))
	for _, fb := range feedback {
		userFeedback = append(userFeedback, UserFeedback{
			ID:              fb.ID,
			MessageID:       fb.MessageID,
			MessageIndex:    messageIndexMap[fb.MessageID],
			Resolution:      fb.UserResolution,
			ResolutionNotes: fb.UserResolutionNotes.String,
		})
	}

	return &GetUserFeedbackForChatResult{
		UserFeedback: userFeedback,
	}, nil
}
