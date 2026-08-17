package hooks

import (
	"context"
	"fmt"
	"log/slog"

	chatv1 "github.com/speakeasy-api/gram/infra/gen/gram/chat/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

// HookMessageHandler is the gram.chat.v1.HookMessagePersister subscriber. It
// owns nothing beyond the ack decision; ChatPersister does the work.
type HookMessageHandler struct {
	logger    *slog.Logger
	persister *ChatPersister
}

func NewHookMessageHandler(logger *slog.Logger, persister *ChatPersister) *HookMessageHandler {
	return &HookMessageHandler{
		logger:    logger.With(attr.SlogComponent("hook-message-handler")),
		persister: persister,
	}
}

// Handle persists one transcript row.
//
// Errors nack, which redelivers and eventually dead-letters. That is the right
// default even for a message that can never be written — a malformed one, say —
// because the alternative is acking it, and a row silently dropped here is a
// turn missing from a customer's transcript with nothing but a log line to say
// so. The DLQ is where those become visible.
//
// Redelivery is safe: every insert is keyed on the producer-minted id.
func (h *HookMessageHandler) Handle(ctx context.Context, msg *chatv1.HookMessage, _ gcp.MessageMetadata) error {
	stored, err := h.persister.Persist(ctx, msg)
	if err != nil {
		return fmt.Errorf("persist hook chat message: %w", err)
	}

	// Mirrors the inline path: a stored prompt from an adapter that falls back
	// to reading the native transcript marks the session, so the LiteLLM proxy's
	// copy of the same turn is suppressed rather than written alongside it.
	if stored && usesNativeTranscriptFallback(msg.GetAdapter()) {
		h.persister.markNativePromptSession(
			ctx,
			msg.GetProjectId(),
			msg.GetSession().GetSessionId(),
			msg.GetAdapter(),
		)
	}

	h.logger.DebugContext(ctx, "persisted hook chat message",
		attr.SlogEvent("hook_chat_message_persisted"),
		attr.SlogChatID(msg.GetChatId()),
		attr.SlogProjectID(msg.GetProjectId()),
		attr.SlogGenAIConversationID(msg.GetSession().GetSessionId()),
	)
	return nil
}
