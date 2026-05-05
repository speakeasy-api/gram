package resolution_activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
)

type GetUserFeedbackForChat struct {
	logger *slog.Logger
	repo   *repo.Queries
}

func NewGetUserFeedbackForChat(logger *slog.Logger, db *pgxpool.Pool) *GetUserFeedbackForChat {
	return &GetUserFeedbackForChat{
		logger: logger,
		repo:   repo.New(db),
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
	// Generation is the chat generation pinned for this analysis run. Subsequent
	// activities in the workflow must pass this value back so every read uses
	// the same snapshot, otherwise message indices computed here can be
	// invalidated by a generation bump in flight.
	Generation   int32
	UserFeedback []UserFeedback
}

func (g *GetUserFeedbackForChat) Do(ctx context.Context, args GetUserFeedbackForChatArgs) (*GetUserFeedbackForChatResult, error) {
	generation, err := g.repo.GetMaxGenerationForChat(ctx, args.ChatID)
	if err != nil {
		return nil, fmt.Errorf("get max generation for chat: %w", err)
	}

	feedback, err := g.repo.ListUserFeedbackForChat(ctx, args.ChatID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &GetUserFeedbackForChatResult{
				Generation:   generation,
				UserFeedback: []UserFeedback{},
			}, nil
		}
		return nil, fmt.Errorf("list user feedback: %w", err)
	}

	messages, err := g.repo.ListChatMessagesByGeneration(ctx, repo.ListChatMessagesByGenerationParams{
		ChatID:     args.ChatID,
		ProjectID:  args.ProjectID,
		Generation: generation,
	})
	if err != nil {
		return nil, fmt.Errorf("list chat messages by generation: %w", err)
	}

	messageIndexMap := make(map[uuid.UUID]int, len(messages))
	for i, msg := range messages {
		messageIndexMap[msg.ID] = i
	}

	userFeedback := make([]UserFeedback, 0, len(feedback))
	for _, fb := range feedback {
		idx, ok := messageIndexMap[fb.MessageID]
		if !ok {
			// Feedback references a message from an older generation that has
			// been replaced; without a stable index we cannot attribute it to a
			// segment in the pinned snapshot.
			g.logger.WarnContext(ctx, "skipping user feedback for message outside pinned chat generation",
				attr.SlogChatID(args.ChatID.String()),
				attr.SlogMessageID(fb.MessageID.String()),
			)
			continue
		}
		userFeedback = append(userFeedback, UserFeedback{
			ID:              fb.ID,
			MessageID:       fb.MessageID,
			MessageIndex:    idx,
			Resolution:      fb.UserResolution,
			ResolutionNotes: fb.UserResolutionNotes.String,
		})
	}

	return &GetUserFeedbackForChatResult{
		Generation:   generation,
		UserFeedback: userFeedback,
	}, nil
}
