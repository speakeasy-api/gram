package evalrun

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/evalrun/repo"
)

// QueueAt runs Queue as if it were now, so a test can age unresolved runs
// past the stale-run cutoff without rewriting their timestamps.
func QueueAt(ctx context.Context, db *pgxpool.Pool, cfg Config, requestedBy string, now time.Time) (repo.MemoryEvalRun, error) {
	return queue(ctx, db, cfg, requestedBy, now.Add(-staleRunAge))
}

// StaleRunAge is how old an unresolved run must be before a start fails it.
const StaleRunAge = staleRunAge
