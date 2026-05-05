package resolution_activities

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
)

// ErrTypeGenerationBumped is the Temporal application error type returned when
// the chat's generation has advanced past the snapshot the workflow pinned at
// the start of analysis. The workflow uses this to abandon stale in-flight
// work and continue-as-new on the new generation.
const ErrTypeGenerationBumped = "ChatResolutionGenerationBumped"

// ErrGenerationBumped is the underlying sentinel; it is wrapped in a
// non-retryable Temporal application error before crossing the activity
// boundary so the workflow can detect and recover.
var ErrGenerationBumped = errors.New("chat generation bumped during analysis")

func newGenerationBumpedError(pinned, current int32) error {
	return temporal.NewNonRetryableApplicationError(
		fmt.Sprintf("chat generation bumped during analysis: pinned=%d current=%d", pinned, current),
		ErrTypeGenerationBumped,
		ErrGenerationBumped,
	)
}

// IsGenerationBumped reports whether err originated from a generation-bumped
// activity, including after Temporal has wrapped it as it crossed the activity
// boundary.
func IsGenerationBumped(err error) bool {
	if errors.Is(err, ErrGenerationBumped) {
		return true
	}
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == ErrTypeGenerationBumped
	}
	return false
}

// loadMessagesAtPinnedGeneration verifies that the chat's current generation
// still matches the pinned one and returns the messages at that generation. On
// mismatch it returns a non-retryable Temporal error so the workflow can
// continue-as-new on the new generation instead of acting on stale indices.
func loadMessagesAtPinnedGeneration(ctx context.Context, queries *repo.Queries, chatID, projectID uuid.UUID, expectedGeneration int32) ([]repo.ChatMessage, error) {
	currentGen, err := queries.GetMaxGenerationForChat(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("get max generation for chat: %w", err)
	}
	if currentGen != expectedGeneration {
		return nil, newGenerationBumpedError(expectedGeneration, currentGen)
	}
	messages, err := queries.ListChatMessagesByGeneration(ctx, repo.ListChatMessagesByGenerationParams{
		ChatID:     chatID,
		ProjectID:  projectID,
		Generation: expectedGeneration,
	})
	if err != nil {
		return nil, fmt.Errorf("list chat messages by generation: %w", err)
	}
	return messages, nil
}
