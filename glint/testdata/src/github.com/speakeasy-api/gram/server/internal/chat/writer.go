package chat

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	tsrepo "github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
)

// The owning package may call its own write queries.
func write() {
	_, _ = repo.New(nil).CreateChatMessage(context.Background(), nil)
}

// Seed queries are for tests, even inside the owning package.
func seed() {
	_ = repo.New(nil).SeedChatMessage(context.Background()) // want "do not call SeedChatMessage directly"
}

// The transcript writes belong to transcriptstore, not to the chat writer.
func transcript() {
	_ = tsrepo.New(nil).WriteChatMessage(context.Background()) // want "do not call WriteChatMessage directly"
}
