// Package evalrun runs one stored team memory eval run against the corpus the
// demo seed wrote: it rebuilds the corpus, writes team memory with the run's
// summary and digest models, scores every answer model in every arm, and
// stores the report on the run's row. Platform admins start runs from the
// dashboard; a Temporal activity calls Runner.Run.
package evalrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/demoseed"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/bench"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/evalrun/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// concurrency bounds in-flight model calls per run.
const concurrency = 8

// maxErrorRunes keeps a stored failure reason readable.
const maxErrorRunes = 2000

// Config is what a run varies; it is stored as the run's config column.
type Config struct {
	AnswerModels []string `json:"answer_models"`
	JudgeModel   string   `json:"judge_model"`
	SummaryModel string   `json:"summary_model"`
	DigestModel  string   `json:"digest_model"`
	Runs         int      `json:"runs"`
}

// DefaultConfig is one run of the live models, judged by a different family.
func DefaultConfig() Config {
	return Config{
		AnswerModels: []string{"anthropic/claude-sonnet-5.5"},
		JudgeModel:   "openai/gpt-5.5",
		SummaryModel: bench.DefaultMemoryModels.Summary,
		DigestModel:  bench.DefaultMemoryModels.Digest,
		Runs:         1,
	}
}

// MaxRuns bounds repeats per model and arm so one run cannot spend without
// limit.
const MaxRuns = 3

// MaxAnswerModels bounds the models one run compares.
const MaxAnswerModels = 4

// Validate rejects a config the bench would reject, plus one larger than a
// single run may spend.
func (c Config) Validate() error {
	if c.Runs > MaxRuns {
		return fmt.Errorf("runs must be at most %d", MaxRuns)
	}
	if len(c.AnswerModels) > MaxAnswerModels {
		return fmt.Errorf("at most %d answer models per run", MaxAnswerModels)
	}
	if !openrouter.IsModelAllowed(c.SummaryModel) || !openrouter.IsModelAllowed(c.DigestModel) {
		return errors.New("summary and digest models must be allowed models")
	}
	if err := c.benchConfig().Validate(); err != nil {
		return fmt.Errorf("invalid bench config: %w", err)
	}
	return nil
}

func (c Config) benchConfig() bench.Config {
	return bench.Config{AnswerModels: c.AnswerModels, JudgeModel: c.JudgeModel, Runs: c.Runs, Concurrency: concurrency}
}

// Runner executes stored runs.
type Runner struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	client openrouter.CompletionClient
}

// NewRunner builds a runner that spends completions through client.
func NewRunner(logger *slog.Logger, db *pgxpool.Pool, client openrouter.CompletionClient) *Runner {
	return &Runner{logger: logger.With(attr.SlogComponent("memory_eval_runs")), db: db, repo: repo.New(db), client: client}
}

// Run claims a queued run, executes it and stores the outcome. A run that is
// not queued (already claimed, finished or deleted) is left alone, so a
// duplicate attempt never spends twice. Failures land on the row; the error
// is returned only when the row itself could not be updated.
func (r *Runner) Run(ctx context.Context, runID uuid.UUID) error {
	run, err := r.repo.MarkRunRunning(ctx, runID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		r.logger.InfoContext(ctx, "memory eval run is not queued; skipping", attr.SlogMemoryEvalRunID(runID.String()))
		return nil
	case err != nil:
		return fmt.Errorf("claim memory eval run: %w", err)
	}

	report, runErr := r.execute(ctx, run)
	if runErr != nil {
		r.logger.ErrorContext(ctx, "memory eval run failed", attr.SlogMemoryEvalRunID(runID.String()), attr.SlogError(runErr))
		return r.fail(ctx, runID, runErr.Error())
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return r.fail(ctx, runID, "encode report: "+err.Error())
	}
	if _, err := r.repo.CompleteRun(ctx, repo.CompleteRunParams{Report: encoded, CostUsd: costNumeric(TotalCost(report)), ID: runID}); err != nil {
		return fmt.Errorf("store memory eval report: %w", err)
	}
	return nil
}

// MarkInterrupted fails a run whose activity died before it could record the
// outcome. A no-op for runs that already resolved.
func (r *Runner) MarkInterrupted(ctx context.Context, runID uuid.UUID) error {
	return r.fail(ctx, runID, "the run stopped before it finished; start it again")
}

func (r *Runner) fail(ctx context.Context, runID uuid.UUID, reason string) error {
	if runes := []rune(reason); len(runes) > maxErrorRunes {
		reason = string(runes[:maxErrorRunes])
	}
	if _, err := r.repo.FailRun(ctx, repo.FailRunParams{Error: pgtype.Text{String: reason, Valid: true}, CostUsd: pgtype.Numeric{Int: nil, Exp: 0, NaN: false, InfinityModifier: pgtype.Finite, Valid: false}, ID: runID}); err != nil {
		return fmt.Errorf("mark memory eval run failed: %w", err)
	}
	return nil
}

func (r *Runner) execute(ctx context.Context, run repo.MemoryEvalRun) (*bench.Report, error) {
	var cfg Config
	if err := json.Unmarshal(run.Config, &cfg); err != nil {
		return nil, fmt.Errorf("decode run config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid run config: %w", err)
	}
	target, err := r.target(ctx, run.OrganizationID)
	if err != nil {
		return nil, err
	}

	prep, err := bench.Prepare(ctx, r.logger, r.client, target, bench.MemoryModels{Summary: cfg.SummaryModel, Digest: cfg.DigestModel}, concurrency)
	if err != nil {
		return nil, fmt.Errorf("write team memory: %w", err)
	}
	searchArm, err := bench.SearchChatsArm(r.logger, target)
	if err != nil {
		return nil, fmt.Errorf("build search_chats arm: %w", err)
	}
	memoryArm, err := bench.SearchTeamMemoryArm(r.logger, target)
	if err != nil {
		return nil, fmt.Errorf("build search_team_memory arm: %w", err)
	}
	report, err := bench.Run(ctx, r.client, target.Corpus, []bench.Arm{bench.NoMemoryArm(), searchArm, memoryArm}, cfg.benchConfig())
	if err != nil {
		return nil, fmt.Errorf("score arms: %w", err)
	}
	report.Preparation = &prep
	return report, nil
}

// target finds the corpus the demo seed wrote into the organization: the
// seed's two eval projects, and the corpus rebuilt at the seed's anchor so
// answer keys and chat IDs match what is stored.
func (r *Runner) target(ctx context.Context, organizationID string) (bench.Target, error) {
	spec, ok := seededSpec(organizationID)
	if !ok {
		return bench.Target{}, fmt.Errorf("organization %s has no seeded memory eval corpus", organizationID)
	}
	projects := make(map[memoryeval.ProjectKey]bench.Project, 2)
	var anchorProject projectsrepo.Project
	for _, key := range []memoryeval.ProjectKey{memoryeval.ProjectMain, memoryeval.ProjectOther} {
		id, _ := demoseed.MemoryEvalProjectID(spec, key)
		project, err := projectsrepo.New(r.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: id, OrganizationID: organizationID})
		if err != nil {
			return bench.Target{}, fmt.Errorf("load memory eval project %s (reseed the demo org if it is missing): %w", key, err)
		}
		projects[key] = bench.Project{ID: project.ID, Slug: project.Slug}
		if key == memoryeval.ProjectMain {
			anchorProject = project
		}
	}
	corpus := demoseed.MemoryEvalCorpus(spec, demoseed.MemoryEvalAnchor(anchorProject.CreatedAt.Time))
	return bench.Target{
		DB:             r.db,
		Corpus:         corpus,
		OrganizationID: organizationID,
		Projects:       projects,
		UserID:         func(handle string) string { return spec.UserPrefix + handle },
	}, nil
}

// seededSpec is the seed tenant for an organization: the shared demo org in
// deployed environments, the local org in development.
func seededSpec(organizationID string) (demoseed.Spec, bool) {
	for _, spec := range []demoseed.Spec{demoseed.DefaultSpec(), demoseed.LocalSpec()} {
		if spec.OrgID == organizationID {
			return spec, true
		}
	}
	var none demoseed.Spec
	return none, false
}

// TotalCost is what a run reported spending across preparation, answers and
// grading.
func TotalCost(report *bench.Report) float64 {
	total := report.JudgeCostUSD
	for _, s := range report.Scores {
		total += s.CostUSD
	}
	if p := report.Preparation; p != nil {
		total += p.SummarySpend.CostUSD + p.DigestSpend.CostUSD
	}
	return total
}

func costNumeric(cost float64) pgtype.Numeric {
	var n pgtype.Numeric
	if err := n.Scan(fmt.Sprintf("%.4f", cost)); err != nil {
		return pgtype.Numeric{Int: nil, Exp: 0, NaN: false, InfinityModifier: pgtype.Finite, Valid: false}
	}
	return n
}
