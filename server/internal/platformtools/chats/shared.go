package chats

import (
	"context"

	"github.com/speakeasy-api/gram/server/gen/chat"
)

// ChatService is the subset of the chat management service that the managed
// assistant's chat-history tools call. The concrete chat service satisfies it;
// tools pass nil auth tokens because the assistant runtime supplies project and
// auth context out of band.
type ChatService interface {
	ListChats(ctx context.Context, payload *chat.ListChatsPayload) (*chat.ListChatsResult, error)
	LoadChat(ctx context.Context, payload *chat.LoadChatPayload) (*chat.Chat, error)
}
