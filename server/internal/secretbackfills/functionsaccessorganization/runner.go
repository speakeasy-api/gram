// Package functionsaccessorganization sets functions_access.organization_id
// on function runner keys written before the deploy path set it. The
// migrations CLI and the admin dashboard's temporary Secret backfills page run
// it; see
// server/cmd/tools/migrations/FUNCTIONS_ACCESS_ORGANIZATION_BACKFILL.md.
package functionsaccessorganization

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// Options bound one run.
type Options struct {
	// Apply writes the backfill. Otherwise the run only counts.
	Apply bool

	// BatchSize is the most rows one statement looks at.
	BatchSize int32
}

// Counts classifies function runner keys, soft-deleted ones included, without
// naming organizations.
type Counts struct {
	// Pending is the number of runner keys the backfill fills: no
	// organization_id yet, and a project whose organization exists.
	Pending int64

	// MissingOrganization is the number of runner keys without an
	// organization_id whose project is gone or whose project's organization
	// has no organization_metadata row. They stay NULL.
	MissingOrganization int64
}

// Report summarizes one run.
type Report struct {
	// Filled is the number of runner keys this run updated. Always zero in a
	// dry run.
	Filled int64

	// Counts is the state after the run: what remains in an apply, what an
	// apply would act on in a dry run.
	Counts Counts
}

// Run fills organization_id in id order, each batch committing on its own,
// so an interrupted run continues where it stopped when run again. A dry run
// only counts.
func Run(ctx context.Context, db DBTX, opts Options) (Report, error) {
	q := New(db)
	report := Report{Filled: 0, Counts: Counts{Pending: 0, MissingOrganization: 0}}

	if opts.Apply {
		after := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		for {
			if err := ctx.Err(); err != nil {
				return report, fmt.Errorf("fill runner key organizations: %w", err)
			}

			batch, err := q.FillBatch(ctx, FillBatchParams{After: after, BatchSize: opts.BatchSize})
			if err != nil {
				return report, fmt.Errorf("fill runner key organizations: %w", err)
			}
			report.Filled += batch.Filled
			// An empty batch has no last id: every row after the previous
			// batch already has an organization_id.
			if batch.LastID == uuid.Nil {
				break
			}
			after = uuid.NullUUID{UUID: batch.LastID, Valid: true}
		}
	}

	counts, err := q.CountRunnerKeys(ctx)
	if err != nil {
		return report, fmt.Errorf("count runner keys: %w", err)
	}
	report.Counts = Counts(counts)
	return report, nil
}
