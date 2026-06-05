package chats

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type LoadChat struct {
	chat ChatService
}

type loadChatInput struct {
	ID         string `json:"id" jsonschema:"The chat ID to load."`
	Generation *int   `json:"generation,omitempty" jsonschema:"Generation to load. Omit to receive the latest; walk from max_generation down to 0 to page through history."`
}

func NewLoadChatTool(chatSvc ChatService) *LoadChat {
	return &LoadChat{chat: chatSvc}
}

func (s *LoadChat) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "chats",
		HandlerName: "load_chat",
		Name:        "platform_load_chat",
		Description: "Load a chat's messages by ID. Returns the latest generation by default; pass `generation` to page through history.",
		InputSchema: core.BuildInputSchema[loadChatInput](),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *LoadChat) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.chat == nil {
		return fmt.Errorf("chat service not configured")
	}

	input := loadChatInput{ID: "", Generation: nil}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if input.ID == "" {
		return fmt.Errorf("id is required")
	}
	if input.Generation != nil && *input.Generation < 0 {
		return fmt.Errorf("generation must be >= 0")
	}

	result, err := s.chat.LoadChat(ctx, &chat.LoadChatPayload{
		SessionToken:      nil,
		ProjectSlugInput:  nil,
		ChatSessionsToken: nil,
		ID:                input.ID,
		Generation:        input.Generation,
	})
	if err != nil {
		return fmt.Errorf("load chat: %w", err)
	}

	return core.EncodeResult(wr, result)
}
