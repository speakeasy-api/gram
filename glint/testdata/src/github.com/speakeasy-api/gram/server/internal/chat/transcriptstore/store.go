package transcriptstore

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	tsrepo "github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
)

// transcriptstore owns the transcript writes.
func seal() {
	_ = tsrepo.New(nil).WriteChatMessage(context.Background())
}

// Message inserts belong to the chat writer.
func insert() {
	_, _ = repo.New(nil).CreateChatMessage(context.Background(), nil) // want "do not call CreateChatMessage directly"
}
