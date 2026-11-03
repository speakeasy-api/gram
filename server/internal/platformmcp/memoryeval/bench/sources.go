package bench

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/bench/repo"
)

// SourcesFunc returns the chats a memo or digest citation stands for, so the
// citation can be checked against a question's sources.
type SourcesFunc func(ctx context.Context, kind string, id uuid.UUID) ([]uuid.UUID, error)

// MemorySources resolves memo citations to the chat they were saved from and
// digest citations to every chat they were built from, walking week and
// month digests down to their day digests.
func MemorySources(target Target) SourcesFunc {
	queries := repo.New(target.DB)
	return func(ctx context.Context, kind string, id uuid.UUID) ([]uuid.UUID, error) {
		var (
			chats []uuid.UUID
			err   error
		)
		switch kind {
		case "memo":
			chats, err = queries.MemoSourceChats(ctx, repo.MemoSourceChatsParams{MemoID: id, OrganizationID: target.OrganizationID})
		case "digest":
			chats, err = queries.DigestSourceChats(ctx, repo.DigestSourceChatsParams{DigestID: id, OrganizationID: target.OrganizationID})
		default:
			return nil, fmt.Errorf("unknown citation kind %q", kind)
		}
		if err != nil {
			return nil, fmt.Errorf("resolve %s %s: %w", kind, id, err)
		}
		return chats, nil
	}
}
