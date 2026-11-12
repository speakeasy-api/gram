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

// CheckSeedUnchangedSince loads the eval projects' stamp, runs between, and
// reports whether a run that loaded them first could still store its report.
func CheckSeedUnchangedSince(ctx context.Context, r *Runner, organizationID string, between func()) error {
	_, stamp, err := r.target(ctx, organizationID)
	if err != nil {
		return err
	}
	between()
	return r.checkSeedUnchanged(ctx, organizationID, stamp)
}

// ErrReseeded is the failure a run that overlapped a reseed stores.
var ErrReseeded = errReseeded
