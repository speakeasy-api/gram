// Package remotesessionsorganization sets remote_sessions.organization_id on
// rows written before every writer set it; see
// REMOTE_SESSIONS_ORGANIZATION_BACKFILL.md.
package remotesessionsorganization

import (
	"context"
	"fmt"
)

// Options bound one run.
type Options struct {
	// Apply writes the backfill. Otherwise the run only counts.
	Apply bool

	// BatchSize is the most rows one statement updates.
	BatchSize int32
}

// Counts classifies remote sessions, soft-deleted ones included, without
// naming organizations.
type Counts struct {
	// Pending is the number of sessions the backfill fills: no
	// organization_id yet, and a client whose organization exists.
	Pending int64

	// GlobalClient is the number of sessions under a client with no project
	// and no organization. They have no organization and stay NULL.
	GlobalClient int64

	// MissingOrganization is the number of sessions whose client names an
	// organization, directly or through its project, that has no
	// organization_metadata row. They stay NULL.
	MissingOrganization int64

	// Mismatched is the number of sessions whose organization_id differs from
	// their client's organization. No writer produces these; the backfill
	// leaves them for investigation.
	Mismatched int64
}

// Report summarizes one run.
type Report struct {
	// Filled is the number of sessions this run updated. Always zero in a dry
	// run.
	Filled int64

	// Counts is the state after the run: what remains in an apply, what an
	// apply would act on in a dry run.
	Counts Counts
}

// Run fills organization_id one organization at a time, each batch
// committing on its own, so an interrupted run continues where it stopped
// when run again. A dry run only counts.
func Run(ctx context.Context, db DBTX, opts Options) (Report, error) {
	q := New(db)
	report := Report{Filled: 0, Counts: Counts{Pending: 0, GlobalClient: 0, MissingOrganization: 0, Mismatched: 0}}

	if opts.Apply {
		// One scan finds the organizations; each batch then reaches that
		// organization's sessions through its clients.
		organizations, err := q.ListPendingOrganizations(ctx)
		if err != nil {
			return report, fmt.Errorf("list organizations with pending sessions: %w", err)
		}

		for _, organizationID := range organizations {
			for {
				if err := ctx.Err(); err != nil {
					return report, fmt.Errorf("fill session organizations: %w", err)
				}

				n, err := q.FillOrganizationBatch(ctx, FillOrganizationBatchParams{OrganizationID: organizationID, BatchSize: opts.BatchSize})
				if err != nil {
					return report, fmt.Errorf("fill session organizations: %w", err)
				}
				report.Filled += n
				if n == 0 {
					break
				}
			}
		}
	}

	counts, err := q.CountSessions(ctx)
	if err != nil {
		return report, fmt.Errorf("count sessions: %w", err)
	}
	report.Counts = Counts(counts)
	return report, nil
}
