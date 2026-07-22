package efficacy

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

// MaxSweepProjectPage is the widest page of projects one discovery call reads.
// A sweep reaches the rest of the estate by chaining the last project id it saw,
// which is what keeps a single call's cost independent of how many projects hold
// work.
const MaxSweepProjectPage int32 = 100

// PendingWorkProject is a project the sweep has to visit, and whether it holds
// a reservation to recover. HasStale is what lets the sweep skip the reset for
// the projects named by the other two sources, which is most of them.
type PendingWorkProject struct {
	ProjectID uuid.UUID `json:"project_id"`
	HasStale  bool      `json:"has_stale"`
}

// PendingWorkProjects returns the next page of projects that hold efficacy work
// the pipeline has not finished, ordered by project id and starting strictly
// after the given one. The zero uuid starts at the head of the estate.
//
// Three things count as unfinished work, and a project holding any of them is
// returned: an activation that was reconciled but never enqueued, an evaluation
// still pending, and a reservation whose owner has been gone for staleAfter. The
// last is what makes this a recovery pass rather than a discovery one — a
// project whose only work is a crashed reservation would otherwise never be
// visited again, since nothing left to enqueue or reserve would name it, and it
// is the one the returned HasStale reports.
//
// A page shorter than pageLimit is the end of the estate.
func PendingWorkProjects(ctx context.Context, db *pgxpool.Pool, after uuid.UUID, staleAfter time.Duration, pageLimit int32) ([]PendingWorkProject, error) {
	if pageLimit <= 0 || pageLimit > MaxSweepProjectPage {
		return nil, fmt.Errorf("list projects with pending skill efficacy work: page limit must be between 1 and %d, got %d", MaxSweepProjectPage, pageLimit)
	}

	rows, err := repo.New(db).ListProjectsWithPendingSkillEfficacyWork(ctx, repo.ListProjectsWithPendingSkillEfficacyWorkParams{
		ProjectCursor: after,
		StaleAfter:    pgtype.Interval{Microseconds: staleAfter.Microseconds(), Days: 0, Months: 0, Valid: true},
		PageLimit:     pageLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list projects with pending skill efficacy work: %w", err)
	}

	projects := make([]PendingWorkProject, 0, len(rows))
	for _, row := range rows {
		projects = append(projects, PendingWorkProject{ProjectID: row.ProjectID, HasStale: row.HasStale})
	}

	return projects, nil
}
