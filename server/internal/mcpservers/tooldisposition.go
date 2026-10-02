package mcpservers

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/tooldisposition"
)

// ToolDispositionCache resolves effective tool annotations and dispositions.
type ToolDispositionCache = tooldisposition.Cache

// NewToolDispositionCache creates a shared tool annotation cache.
func NewToolDispositionCache(logger *slog.Logger, db *pgxpool.Pool, c cache.Cache) *ToolDispositionCache {
	return tooldisposition.New(logger, db, c)
}
