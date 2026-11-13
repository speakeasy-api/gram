package evalrun_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/evalrun"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/evalrun/evalruntest"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/evalrun/repo"
)

func TestQueueNeedsTheSeededCorpus(t *testing.T) {
	t.Parallel()

	db, err := testInfra(t).CloneTestDatabase(t, "evalrun")
	require.NoError(t, err)
	_, err = evalrun.Queue(t.Context(), db, evalrun.DefaultConfig(), "staff@example.com")
	require.ErrorIs(t, err, evalrun.ErrNoCorpus)

	// A partial corpus would fail the run after it spent a slot.
	evalruntest.SeedCorpusProjects(t, db, memoryeval.ProjectMain)
	_, err = evalrun.Queue(t.Context(), db, evalrun.DefaultConfig(), "staff@example.com")
	require.ErrorIs(t, err, evalrun.ErrNoCorpus)
}

// Runs share the eval projects' memory, so only one may be active, and the
// daily limit counts failed runs a workflow claimed, since they spent.
func TestQueueAllowsOneActiveRunAndADailyLimit(t *testing.T) {
	t.Parallel()

	db, err := testInfra(t).CloneTestDatabase(t, "evalrun")
	require.NoError(t, err)
	orgID := evalruntest.SeedCorpus(t, db)
	q := repo.New(db)

	first, err := evalrun.Queue(t.Context(), db, evalrun.DefaultConfig(), "staff@example.com")
	require.NoError(t, err)
	require.Equal(t, orgID, first.OrganizationID)
	require.Equal(t, "queued", first.Status)

	_, err = evalrun.Queue(t.Context(), db, evalrun.DefaultConfig(), "staff@example.com")
	require.ErrorIs(t, err, evalrun.ErrRunActive)

	fail := func(run repo.MemoryEvalRun) {
		_, err := q.MarkRunRunning(t.Context(), run.ID)
		require.NoError(t, err)
		_, err = q.FailRun(t.Context(), repo.FailRunParams{Error: pgtype.Text{String: "test", Valid: true}, CostUsd: pgtype.Numeric{}, ID: run.ID})
		require.NoError(t, err)
	}
	fail(first)
	// Each run comes from a different staff member: the limit is shared.
	for i := range evalrun.DailyRunLimit - 1 {
		run, err := evalrun.Queue(t.Context(), db, evalrun.DefaultConfig(), fmt.Sprintf("staff-%d@example.com", i))
		require.NoError(t, err)
		fail(run)
	}
	_, err = evalrun.Queue(t.Context(), db, evalrun.DefaultConfig(), "another-staff@example.com")
	require.ErrorIs(t, err, evalrun.ErrDailyLimit)
}

// A run that failed before any workflow claimed it spent nothing, so it does
// not use up the daily limit.
func TestQueueDoesNotCountRunsThatNeverStarted(t *testing.T) {
	t.Parallel()

	db, err := testInfra(t).CloneTestDatabase(t, "evalrun")
	require.NoError(t, err)
	evalruntest.SeedCorpus(t, db)

	for range evalrun.DailyRunLimit + 1 {
		run, err := evalrun.Queue(t.Context(), db, evalrun.DefaultConfig(), "staff@example.com")
		require.NoError(t, err)
		require.NoError(t, evalrun.FailToStart(t.Context(), db, run, "start failed"))
	}
	n, err := evalrun.RunsInWindow(t.Context(), db)
	require.NoError(t, err)
	require.Zero(t, n)
}

// A run left queued or running past any workflow's lifetime has nothing
// behind it to resolve it. A soft-deleted run that is still unresolved blocks
// starts like any other, and the sweep a later start runs is what clears it.
func TestQueueFailsRunsOlderThanAnyWorkflow(t *testing.T) {
	t.Parallel()

	db, err := testInfra(t).CloneTestDatabase(t, "evalrun")
	require.NoError(t, err)
	evalruntest.SeedCorpus(t, db)
	q := repo.New(db)

	orphan, err := evalrun.Queue(t.Context(), db, evalrun.DefaultConfig(), "staff@example.com")
	require.NoError(t, err)
	_, err = q.MarkRunRunning(t.Context(), orphan.ID)
	require.NoError(t, err)
	_, err = q.DeleteRun(t.Context(), orphan.ID)
	require.NoError(t, err)

	_, err = evalrun.Queue(t.Context(), db, evalrun.DefaultConfig(), "staff@example.com")
	require.ErrorIs(t, err, evalrun.ErrRunActive, "a deleted run that is still running keeps writing memory")

	// A start made after any workflow could still be running fails the orphan
	// under the lock and then queues.
	next, err := evalrun.QueueAt(t.Context(), db, evalrun.DefaultConfig(), "staff@example.com", time.Now().Add(evalrun.StaleRunAge+time.Minute))
	require.NoError(t, err)
	require.Equal(t, "queued", next.Status)

	// The orphan is failed, not just skipped: with the new run failed too,
	// a plain start goes through.
	_, err = q.FailRun(t.Context(), repo.FailRunParams{Error: pgtype.Text{String: "test", Valid: true}, CostUsd: pgtype.Numeric{}, ID: next.ID})
	require.NoError(t, err)
	_, err = evalrun.Queue(t.Context(), db, evalrun.DefaultConfig(), "staff@example.com")
	require.NoError(t, err)
}

// The start lock is what makes the one-active-run rule hold when starts race:
// exactly one of several simultaneous starts may succeed.
func TestQueueAcceptsOneOfConcurrentStarts(t *testing.T) {
	t.Parallel()

	db, err := testInfra(t).CloneTestDatabase(t, "evalrun")
	require.NoError(t, err)
	evalruntest.SeedCorpus(t, db)

	const starters = 6
	ready := make(chan struct{})
	errs := make(chan error, starters)
	var wg sync.WaitGroup
	for i := range starters {
		wg.Go(func() {
			<-ready
			_, err := evalrun.Queue(t.Context(), db, evalrun.DefaultConfig(), fmt.Sprintf("staff-%d@example.com", i))
			errs <- err
		})
	}
	close(ready)
	wg.Wait()
	close(errs)

	var accepted int
	for err := range errs {
		if err == nil {
			accepted++
			continue
		}
		require.ErrorIs(t, err, evalrun.ErrRunActive)
	}
	require.Equal(t, 1, accepted)
}

func TestQueueRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	db, err := testInfra(t).CloneTestDatabase(t, "evalrun")
	require.NoError(t, err)
	evalruntest.SeedCorpus(t, db)
	cfg := evalrun.DefaultConfig()
	cfg.Runs = evalrun.MaxRuns + 1
	_, err = evalrun.Queue(t.Context(), db, cfg, "staff@example.com")
	require.Error(t, err)
}

// A failed workflow start fails a queued run but never one a workflow
// already claimed.
func TestFailToStartOnlyFailsQueuedRuns(t *testing.T) {
	t.Parallel()

	db, err := testInfra(t).CloneTestDatabase(t, "evalrun")
	require.NoError(t, err)
	evalruntest.SeedCorpus(t, db)
	q := repo.New(db)

	claimed, err := evalrun.Queue(t.Context(), db, evalrun.DefaultConfig(), "staff@example.com")
	require.NoError(t, err)
	_, err = q.MarkRunRunning(t.Context(), claimed.ID)
	require.NoError(t, err)
	require.NoError(t, evalrun.FailToStart(t.Context(), db, claimed, "start failed"))
	got, err := q.GetRun(t.Context(), claimed.ID)
	require.NoError(t, err)
	require.Equal(t, "running", got.Status)

	interrupted, err := q.FailRun(t.Context(), repo.FailRunParams{Error: pgtype.Text{String: "test", Valid: true}, CostUsd: pgtype.Numeric{}, ID: claimed.ID})
	require.NoError(t, err)
	require.EqualValues(t, 1, interrupted)

	queued, err := evalrun.Queue(t.Context(), db, evalrun.DefaultConfig(), "staff@example.com")
	require.NoError(t, err)
	require.NoError(t, evalrun.FailToStart(t.Context(), db, queued, "start failed"))
	got, err = q.GetRun(t.Context(), queued.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	require.Equal(t, "start failed", got.Error.String)
}
