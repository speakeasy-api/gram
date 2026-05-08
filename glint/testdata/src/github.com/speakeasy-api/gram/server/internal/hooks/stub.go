package hooks

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
)

func bad() {
	q := repo.New(nil)
	q.CreateChatMessage(context.Background(), nil) // want "do not call CreateChatMessage directly"
}
