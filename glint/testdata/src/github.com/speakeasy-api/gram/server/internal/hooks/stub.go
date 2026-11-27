package hooks

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	tsrepo "github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
)

func bad() {
	q := repo.New(nil)
	q.CreateChatMessage(context.Background(), nil)                                           // want "do not call CreateChatMessage directly; write chat messages through chat.ChatMessageWriter"
	_, _ = q.CreateExternalChatMessage(context.Background(), repo.CreateChatMessageParams{}) // want "do not call CreateExternalChatMessage directly"
	_ = tsrepo.New(nil).WriteChatMessage(context.Background())                               // want "do not call WriteChatMessage directly"
}

func reads() {
	_ = repo.New(nil).GetChat(context.Background())
}
