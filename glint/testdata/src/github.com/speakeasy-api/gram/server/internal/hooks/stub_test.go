package hooks

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
)

// Tests may seed rows directly.
func seed() {
	_, _ = repo.New(nil).CreateChatMessage(context.Background(), nil)
}
