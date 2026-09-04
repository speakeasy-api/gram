package legacypolicyscope

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
)

// Mode selects what a run does. Dry-run reads and reports without writing.
type Mode string

const (
	ModeDryRun   Mode = "dry-run"
	ModeApply    Mode = "apply"
	ModeValidate Mode = "validate"
)

// ErrValidationFailed reports rows that still carry a legacy scope after apply.
var ErrValidationFailed = errors.New("legacy policy scopes remain after fold")

type Options struct {
	BatchSize        int
	LockTimeout      time.Duration
	StatementTimeout time.Duration
}

// Summary is the per-run report, emitted as JSON by the command.
type Summary struct {
	Mode      Mode             `json:"mode"`
	Scanned   int64            `json:"scanned"`
	Preserved int64            `json:"preserved"`
	Cleared   int64            `json:"cleared"`
	Noop      int64            `json:"noop"`
	Updated   int64            `json:"updated"`
	Batches   int64            `json:"batches"`
	ByAction  map[string]int64 `json:"by_action"`
	Remaining int64            `json:"remaining"`
	Elapsed   time.Duration    `json:"-"`
}

type Runner struct {
	pool    *pgxpool.Pool
	logger  *slog.Logger
	engine  *celenv.Engine
	options Options
}

func NewRunner(pool *pgxpool.Pool, logger *slog.Logger, options Options) (*Runner, error) {
	engine, err := celenv.New()
	if err != nil {
		return nil, fmt.Errorf("build cel engine: %w", err)
	}
	if options.BatchSize <= 0 {
		options.BatchSize = 100
	}
	return &Runner{pool: pool, logger: logger, engine: engine, options: options}, nil
}

// Run folds every policy carrying a legacy scope. Batches are keyset-paginated
// and each batch commits on its own, so an interrupted run resumes by rerunning:
// a folded row no longer matches the candidate predicate.
func (r *Runner) Run(ctx context.Context, mode Mode) (Summary, error) {
	start := time.Now()
	summary := Summary{
		Mode: mode, Scanned: 0, Preserved: 0, Cleared: 0, Noop: 0, Updated: 0,
		Batches: 0, ByAction: map[string]int64{}, Remaining: 0, Elapsed: 0,
	}

	if mode == ModeValidate {
		remaining, err := r.remaining(ctx)
		if err != nil {
			return summary, err
		}
		summary.Remaining = remaining
		summary.Elapsed = time.Since(start)
		if remaining > 0 {
			return summary, fmt.Errorf("%w: %d", ErrValidationFailed, remaining)
		}
		return summary, nil
	}

	// Dry-run walks past rows it does not write, so it needs a moving cursor;
	// apply re-reads from zero each batch because folded rows drop out of the
	// candidate set.
	after := uuid.Nil
	for {
		batch, err := r.runBatch(ctx, mode, after, &summary)
		if err != nil {
			return summary, err
		}
		if batch == uuid.Nil {
			break
		}
		summary.Batches++
		if mode == ModeDryRun {
			after = batch
		}
	}

	remaining, err := r.remaining(ctx)
	if err != nil {
		return summary, err
	}
	summary.Remaining = remaining
	summary.Elapsed = time.Since(start)
	return summary, nil
}

// runBatch folds one locked batch inside a transaction and returns the last id
// it saw, or uuid.Nil when the batch was empty.
func (r *Runner) runBatch(ctx context.Context, mode Mode, after uuid.UUID, summary *Summary) (uuid.UUID, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("begin batch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := New(tx)
	if _, err := q.SetLocalTimeouts(ctx, SetLocalTimeoutsParams{
		LockTimeout:      durationSetting(r.options.LockTimeout, "5s"),
		StatementTimeout: durationSetting(r.options.StatementTimeout, "30s"),
	}); err != nil {
		return uuid.Nil, fmt.Errorf("set local timeouts: %w", err)
	}

	rows, err := q.LockLegacyScopeBatch(ctx, LockLegacyScopeBatchParams{
		AfterID:   after,
		BatchSize: int32(r.options.BatchSize), //nolint:gosec // batch size is operator-supplied and small
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("lock batch: %w", err)
	}
	if len(rows) == 0 {
		return uuid.Nil, nil
	}

	last := uuid.Nil
	for _, row := range rows {
		last = row.ID
		summary.Scanned++
		summary.ByAction[row.Action]++

		if err := r.foldRow(ctx, q, mode, row, summary); err != nil {
			return uuid.Nil, fmt.Errorf("fold policy %s: %w", row.ID, err)
		}
	}

	if mode == ModeApply {
		if err := tx.Commit(ctx); err != nil {
			return uuid.Nil, fmt.Errorf("commit batch: %w", err)
		}
	}
	return last, nil
}

func (r *Runner) foldRow(ctx context.Context, q *Queries, mode Mode, row LockLegacyScopeBatchRow, summary *Summary) error {
	result, err := Fold(Policy{
		Action:          row.Action,
		PolicyType:      row.PolicyType,
		Sources:         row.Sources,
		CustomRuleIDs:   row.CustomRuleIds,
		MessageTypes:    row.MessageTypes,
		ScopeInclude:    row.ScopeInclude.String,
		ScopeExempt:     row.ScopeExempt.String,
		DetectionScopes: ra.DetectionScopesFromConfig(row.AnalyzerConfig),
	})
	if err != nil {
		return err
	}

	switch result.Disposition {
	case DispositionPreserved:
		summary.Preserved++
	case DispositionCleared:
		summary.Cleared++
	case DispositionNoop:
		summary.Noop++
	}

	// A scope the engine rejects would fail the policy closed at scan time, so
	// refuse the whole run rather than write it.
	for _, scope := range result.DetectionScopes {
		if _, err := ra.CompileScope(r.engine, scope.ScopeInclude, scope.ScopeExempt); err != nil {
			return fmt.Errorf("compile folded scope %s: %w", scope.Category, err)
		}
	}

	config, err := ra.WithDetectionScopes(row.AnalyzerConfig, result.DetectionScopes)
	if err != nil {
		return fmt.Errorf("encode analyzer config: %w", err)
	}

	r.logger.InfoContext(ctx, "fold legacy policy scope",
		attr.SlogRiskPolicyID(row.ID.String()),
		attr.SlogRiskPolicyScopeFold(string(result.Disposition)),
		attr.SlogRiskPolicyAction(row.Action),
		attr.SlogRiskPolicyDetectionScopes(string(mustJSON(result.DetectionScopes))),
	)

	if mode != ModeApply {
		return nil
	}

	updated, err := q.ApplyFold(ctx, ApplyFoldParams{
		AnalyzerConfig: config,
		// Only a cleared fold changes what the policy scans.
		BumpVersion: result.Disposition == DispositionCleared,
		ID:          row.ID,
	})
	if err != nil {
		return fmt.Errorf("apply fold: %w", err)
	}
	summary.Updated += updated
	return nil
}

func (r *Runner) remaining(ctx context.Context) (int64, error) {
	remaining, err := New(r.pool).CountRemainingLegacyScopes(ctx)
	if err != nil {
		return 0, fmt.Errorf("count remaining legacy scopes: %w", err)
	}
	return remaining, nil
}

// CountByAction reports the pre-run population, which is what decides how much
// of the fleet the preserve path touches.
func (r *Runner) CountByAction(ctx context.Context) (map[string]int64, error) {
	rows, err := New(r.pool).CountLegacyScopesByAction(ctx)
	if err != nil {
		return nil, fmt.Errorf("count legacy scopes by action: %w", err)
	}
	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		out[row.Action] = row.Total
	}
	return out, nil
}

func durationSetting(d time.Duration, fallback string) string {
	if d <= 0 {
		return fallback
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

func mustJSON(v any) []byte {
	out, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return out
}
