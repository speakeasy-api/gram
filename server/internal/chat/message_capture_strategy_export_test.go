package chat

import (
	"context"

	"github.com/google/uuid"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// TestingLinkSetupAssistantThread exposes the unexported linkSetupAssistantThread
// helper so tests in the chat_test package can exercise the project-scoped
// assistant ownership gate end-to-end.
func (s *Service) TestingLinkSetupAssistantThread(ctx context.Context, projectID *uuid.UUID, chatID uuid.UUID, source, assistantIDHeader string) {
	s.linkSetupAssistantThread(ctx, projectID, chatID, source, assistantIDHeader)
}

// TestingNewPoisonedSession builds a CaptureSession in the state
// StartOrResumeChat produces when its upfront user-message persistence failed:
// the walk ran, rows were prepared, but the DB insert did not land. Calling
// CaptureMessage with this session exercises the atomic catch-up path.
//
// Exported for use from the chat_test package only.
func TestingNewPoisonedSession(
	chatID uuid.UUID,
	projectID uuid.UUID,
	userID, externalUserID, model string,
	source billing.ModelUsageSource,
	generation int32,
	newMessages []or.ChatMessages,
) openrouter.CaptureSession {
	req := openrouter.CompletionRequest{
		OrgID:          "",
		ProjectID:      projectID.String(),
		ChatID:         chatID,
		Messages:       newMessages,
		UsageSource:    source,
		Model:          model,
		UserID:         userID,
		ExternalUserID: externalUserID,
		HTTPMetadata:   nil,
		Tools:          nil,
		Temperature:    nil,
		Stream:         false,
		UserEmail:      "",
		APIKeyID:       "",
		JSONSchema:     nil,
	}

	rows := buildPendingRows(req, projectID, userID, externalUserID, newMessages, generation)

	return &chatCaptureSession{
		generation:  generation,
		pendingRows: rows,
	}
}
