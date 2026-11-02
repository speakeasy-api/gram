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

	"github.com/speakeasy-api/gram/server/internal/demoseed"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/evalrun/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// DailyRunLimit bounds runs per rolling 24 hours across all staff. Failed
// runs count: they spent before failing.
const DailyRunLimit = 10

var (
	// ErrNoCorpus means no organization in this database holds the corpus
	// the demo seed writes; reseed the demo organization.
	ErrNoCorpus = errors.New("no organization holds the seeded memory eval corpus")
	// ErrRunActive means another run is queued or running. Runs share the
	// eval projects' summaries and digests, so they go one at a time.
	ErrRunActive = errors.New("another memory eval run is queued or running")
	// ErrDailyLimit means DailyRunLimit runs started in the last 24 hours.
	ErrDailyLimit = errors.New("daily memory eval run limit reached")
)

// CorpusOrganization is the organization holding the seeded corpus: the
// shared demo organization in deployed environments, the local one in
// development.
func CorpusOrganization(ctx context.Context, db *pgxpool.Pool) (string, error) {
	projects := projectsrepo.New(db)
	for _, spec := range []demoseed.Spec{demoseed.DefaultSpec(), demoseed.LocalSpec()} {
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
	organizationID, err := CorpusOrganization(ctx, db)
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
	active, err := q.CountActiveRuns(ctx)
	if err != nil {
		return none, fmt.Errorf("count active runs: %w", err)
	}
	if active > 0 {
		return none, ErrRunActive
	}
	today, err := q.CountRunsSince(ctx, pgtype.Timestamptz{Time: time.Now().Add(-24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true})
	if err != nil {
		return none, fmt.Errorf("count runs today: %w", err)
	}
	if today >= DailyRunLimit {
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

// FailToStart fails a queued run whose workflow could not be started.
func FailToStart(ctx context.Context, db *pgxpool.Pool, run repo.MemoryEvalRun, reason string) error {
	r := Runner{logger: nil, db: db, repo: repo.New(db), client: nil}
	return r.fail(ctx, run.ID, reason)
}
