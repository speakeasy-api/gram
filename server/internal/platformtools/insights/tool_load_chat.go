package insights

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type LoadChat struct {
	provider func() ChatService
}

type loadChatInput struct {
	ID         string `json:"id" jsonschema:"The ID of the chat to load."`
	Generation *int   `json:"generation,omitempty" jsonschema:"Transcript generation to load (0 = oldest). Omit for the latest generation."`
}

func NewLoadChatTool(provider func() ChatService) *LoadChat {
	return &LoadChat{provider: provider}
}

func (s *LoadChat) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "insights",
		HandlerName: "load_chat",
		Name:        "platform_load_chat",
		Description: "Load a single chat conversation's transcript by its ID. Use list_chats to discover chat IDs first.",
		InputSchema: core.BuildInputSchema[loadChatInput](
			core.WithPropertyNumberRange("generation", 0, 100000),
		),
		Variables:   nil,
		Annotations: readOnlyToolAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *LoadChat) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	svc := s.provider()
	if svc == nil {
		return fmt.Errorf("chat service not configured")
	}

	input := loadChatInput{ID: "", Generation: nil}
	if err := decodeToolInput(payload, &input); err != nil {
		return err
	}
	if input.ID == "" {
		return fmt.Errorf("id is required")
	}

	result, err := svc.LoadChat(ctx, &chat.LoadChatPayload{
		SessionToken:      nil,
		ProjectSlugInput:  nil,
		ChatSessionsToken: nil,
		ID:                input.ID,
		Generation:        input.Generation,
	})
	if err != nil {
		return fmt.Errorf("load chat: %w", err)
	}

	return encodeToolResult(wr, result)
}
