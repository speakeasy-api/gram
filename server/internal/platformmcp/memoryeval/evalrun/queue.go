package evalrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/demoseed"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/evalrun/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// DailyRunLimit bounds runs per RunWindow across all staff. Failed runs
// count, since they spent before failing, unless no workflow ever claimed them.
const DailyRunLimit = 10

// RunWindow is the rolling window DailyRunLimit applies to.
const RunWindow = 24 * time.Hour

// staleRunAge is how old an unresolved run must be before a start fails it.
// It outlives the workflow's run timeout, so a run still inside a live
// workflow is never failed under it.
const staleRunAge = background.MemoryEvalRunWorkflowRunTimeout + 10*time.Minute

// staleRunError is stored on runs a start failed because they outlived any
// workflow that could still resolve them.
const staleRunError = "the run stopped without finishing and was failed so a new run could start"

// RunsInWindow counts the runs that use up DailyRunLimit right now. Queue
// calls it on the transaction holding the start lock; the admin options read
// calls it on the pool.
func RunsInWindow(ctx context.Context, db repo.DBTX) (int64, error) {
	n, err := repo.New(db).CountRunsSince(ctx, pgtype.Timestamptz{Time: time.Now().Add(-RunWindow), InfinityModifier: pgtype.Finite, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("count memory eval runs in window: %w", err)
	}
	return n, nil
}

var (
	// ErrNoCorpus means no organization in this database holds the corpus
	// the demo seed writes; reseed the demo organization.
	ErrNoCorpus = errors.New("no organization holds the seeded memory eval corpus")
	// ErrRunActive means another run is queued or running. Runs share the
	// eval projects' summaries and digests, so they go one at a time.
	ErrRunActive = errors.New("another memory eval run is queued or running")
	// ErrDailyLimit means DailyRunLimit runs started within RunWindow.
	ErrDailyLimit = errors.New("daily memory eval run limit reached")
)

// corpusOrganization is the organization holding the seeded corpus: the
// shared demo organization in deployed environments, the local one in
// development.
func corpusOrganization(ctx context.Context, db *pgxpool.Pool) (string, error) {
	projects := projectsrepo.New(db)
	for _, spec := range seedSpecs() {
		id, ok := demoseed.MemoryEvalProjectID(spec, memoryeval.ProjectMain)
		if !ok {
			continue
		}
		_, err := projects.GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: id, OrganizationID: spec.OrgID})
		switch {
		case err == nil:
			return spec.OrgID, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return "", fmt.Errorf("look up memory eval project: %w", err)
		}
	}
	return "", ErrNoCorpus
}

// Queue validates cfg and inserts a queued run for the corpus organization,
// enforcing one active run and DailyRunLimit. The caller starts the workflow.
func Queue(ctx context.Context, db *pgxpool.Pool, cfg Config, requestedBy string) (repo.MemoryEvalRun, error) {
	var none repo.MemoryEvalRun
	if err := cfg.Validate(); err != nil {
		return none, err
	}
	organizationID, err := corpusOrganization(ctx, db)
	if err != nil {
		return none, err
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		return none, fmt.Errorf("encode run config: %w", err)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return none, fmt.Errorf("begin queue run: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := repo.New(tx)

	if err := q.LockRunStarts(ctx); err != nil {
		return none, fmt.Errorf("lock run starts: %w", err)
	}
	if _, err := q.FailStaleActiveRuns(ctx, repo.FailStaleActiveRunsParams{
		Error:         pgtype.Text{String: staleRunError, Valid: true},
		CreatedBefore: pgtype.Timestamptz{Time: time.Now().Add(-staleRunAge), InfinityModifier: pgtype.Finite, Valid: true},
	}); err != nil {
		return none, fmt.Errorf("fail stale runs: %w", err)
	}
	active, err := q.CountActiveRuns(ctx)
	if err != nil {
		return none, fmt.Errorf("count active runs: %w", err)
	}
	if active > 0 {
		return none, ErrRunActive
	}
	inWindow, err := RunsInWindow(ctx, tx)
	if err != nil {
		return none, err
	}
	if inWindow >= DailyRunLimit {
		return none, ErrDailyLimit
	}
	run, err := q.CreateRun(ctx, repo.CreateRunParams{OrganizationID: organizationID, Config: encoded, RequestedBy: requestedBy})
	if err != nil {
		return none, fmt.Errorf("create run: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return none, fmt.Errorf("commit queue run: %w", err)
	}
	return run, nil
}

// FailToStart fails a run whose workflow could not be started. It only
// touches a still-queued row: if a workflow did start and claimed the run
// before the start call reported an error, the run is left to finish.
func FailToStart(ctx context.Context, db *pgxpool.Pool, run repo.MemoryEvalRun, reason string) error {
	if runes := []rune(reason); len(runes) > maxErrorRunes {
		reason = string(runes[:maxErrorRunes])
	}
	if _, err := repo.New(db).FailQueuedRun(ctx, repo.FailQueuedRunParams{Error: pgtype.Text{String: reason, Valid: true}, ID: run.ID}); err != nil {
		return fmt.Errorf("fail unstarted memory eval run: %w", err)
	}
	return nil
}
