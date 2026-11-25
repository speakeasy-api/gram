package reader

import (
	"context"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/plaintextread/repo"
)

func TestReadsPlaintext(t *testing.T) {
	var q repo.Queries
	_, _ = q.ListMessages(context.Background(), "chat")
}
