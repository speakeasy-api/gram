package analysis

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
)

// MaxSweepProjectPage is the widest page of projects one discovery call reads.
// A sweep reaches the rest of the estate by chaining the last project id it
// saw, which keeps a single call's cost independent of how many projects hold
// work.
const MaxSweepProjectPage int32 = 100

// PendingWorkProject is a project the sweep has to visit, and whether it holds
// a reservation to recover.
type PendingWorkProject struct {
	ProjectID uuid.UUID `json:"project_id"`
	HasStale  bool      `json:"has_stale"`
}

// PendingWorkProjects returns the next page of projects that hold analysis
// work the pipeline has not finished, ordered by project id and starting
// strictly after the given one. The zero uuid starts at the head of the estate.
//
// Two things count as unfinished work: a live pending evaluation, and a
// reservation whose owner has been gone for staleAfter. The second source is
// what makes this a recovery pass rather than a discovery one — a project whose
// only work is a crashed reservation would otherwise never be visited again.
// Sessions no signal ever enqueued are not discovered here: enqueue coverage
// comes from the chat-writer observer, which wakes the coordinator on every
// durable message write.
//
// A page shorter than pageLimit is the end of the estate.
func PendingWorkProjects(ctx context.Context, db *pgxpool.Pool, after uuid.UUID, staleAfter time.Duration, pageLimit int32) ([]PendingWorkProject, error) {
	if pageLimit <= 0 || pageLimit > MaxSweepProjectPage {
		return nil, fmt.Errorf("list projects with pending chat analysis work: page limit must be between 1 and %d, got %d", MaxSweepProjectPage, pageLimit)
	}
	if staleAfter.Microseconds() <= 0 {
		return nil, fmt.Errorf("list projects with pending chat analysis work: stale duration must be positive")
	}

	rows, err := repo.New(db).ListProjectsWithPendingChatAnalysisWork(ctx, repo.ListProjectsWithPendingChatAnalysisWorkParams{
		ProjectCursor: after,
		StaleAfter:    pgtype.Interval{Microseconds: staleAfter.Microseconds(), Days: 0, Months: 0, Valid: true},
		PageLimit:     pageLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list projects with pending chat analysis work: %w", err)
	}

	projects := make([]PendingWorkProject, 0, len(rows))
	for _, row := range rows {
		projects = append(projects, PendingWorkProject{ProjectID: row.ProjectID, HasStale: row.HasStale})
	}

	return projects, nil
}
