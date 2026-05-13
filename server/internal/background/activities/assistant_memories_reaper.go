package activities

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	memrepo "github.com/speakeasy-api/gram/server/internal/memory/repo"
)

type ReapSoftDeletedAssistantMemories struct {
	logger *slog.Logger
	db     *pgxpool.Pool
}

func NewReapSoftDeletedAssistantMemories(logger *slog.Logger, db *pgxpool.Pool) *ReapSoftDeletedAssistantMemories {
	return &ReapSoftDeletedAssistantMemories{
		logger: logger.With(attr.SlogComponent("assistant_memories_reaper")),
		db:     db,
	}
}

func (r *ReapSoftDeletedAssistantMemories) Do(ctx context.Context, cutoff time.Time) (int64, error) {
	rows, err := memrepo.New(r.db).ReapSoftDeletedAssistantMemoriesOlderThan(ctx, conv.ToPGTimestamptz(cutoff))
	if err != nil {
		return 0, fmt.Errorf("reap soft-deleted assistant memories: %w", err)
	}

	return rows, nil
}
