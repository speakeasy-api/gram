package chats

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/gen/types"
)

// ChatService is the subset of the chat management service that the managed
// assistant's chat-history tools call. The concrete chat service satisfies it;
// tools pass nil auth tokens because the assistant runtime supplies project and
// auth context out of band.
type ChatService interface {
	ListChats(ctx context.Context, payload *chat.ListChatsPayload) (*chat.ListChatsResult, error)
	LoadChat(ctx context.Context, payload *chat.LoadChatPayload) (*chat.Chat, error)
}

func readOnlyToolAnnotations() *types.ToolAnnotations {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := false
	return &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    &readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  &idempotent,
		OpenWorldHint:   &openWorld,
	}
}

func decodeToolInput(payload io.Reader, dst any) error {
	body, err := io.ReadAll(payload)
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

func encodeToolResult(wr io.Writer, result any) error {
	if err := json.NewEncoder(wr).Encode(result); err != nil {
		return fmt.Errorf("encode response: %w", err)
	}
	return nil
}
