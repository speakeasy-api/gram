package chat

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
)

// The owning package may call its own write queries.
func write() {
	_, _ = repo.New(nil).CreateChatMessage(context.Background(), nil)
}
