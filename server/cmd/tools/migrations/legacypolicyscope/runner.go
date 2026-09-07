package legacypolicyscope

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
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

// ErrIncompleteApply reports an apply that finished with candidates left. The
// batch query takes rows FOR UPDATE SKIP LOCKED, so a row another session holds
// is passed over silently; reporting success would let the follow-up contract
// migration run against a half-folded table.
var ErrIncompleteApply = errors.New("legacy policy scopes remain after apply")

// defaultApplyAttempts is how many times apply re-walks the candidate set
// before giving up on rows that stayed locked.
const defaultApplyAttempts = 3

// defaultRetryDelay lets a competing writer's transaction finish before the
// next attempt re-reads the rows it skipped.
const defaultRetryDelay = 2 * time.Second

type Options struct {
	BatchSize        int
	LockTimeout      time.Duration
	StatementTimeout time.Duration
	// ApplyAttempts bounds the retries an apply makes for rows SKIP LOCKED
	// passed over. Zero means defaultApplyAttempts.
	ApplyAttempts int
	// RetryDelay is the pause between those attempts. Zero means
	// defaultRetryDelay; negative means no pause at all.
	RetryDelay time.Duration
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
	Attempts  int64            `json:"attempts"`
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
	// The batch size is cast to int32 for the LIMIT parameter, so a larger
	// value would silently wrap into a negative limit.
	if options.BatchSize > math.MaxInt32 {
		return nil, fmt.Errorf("batch size %d exceeds %d", options.BatchSize, math.MaxInt32)
	}
	// Timeouts are serialized as whole milliseconds, and PostgreSQL reads a
	// zero timeout as "no timeout at all": a sub-millisecond value would
	// truncate to 0ms and disable the very guard it asked for.
	if err := checkTimeout("lock timeout", options.LockTimeout); err != nil {
		return nil, err
	}
	if err := checkTimeout("statement timeout", options.StatementTimeout); err != nil {
		return nil, err
	}
	if options.ApplyAttempts <= 0 {
		options.ApplyAttempts = defaultApplyAttempts
	}
	if options.RetryDelay == 0 {
		options.RetryDelay = defaultRetryDelay
	}
	return &Runner{pool: pool, logger: logger, engine: engine, options: options}, nil
}

// Run folds every policy carrying a legacy scope. Batches are keyset-paginated
// and each batch commits on its own, so an interrupted run resumes by rerunning:
// a folded row no longer matches the candidate predicate.
//
// Apply re-walks the candidate set while rows remain, because SKIP LOCKED
// passes over rows another session holds, and fails if any survive the last
// attempt.
func (r *Runner) Run(ctx context.Context, mode Mode) (Summary, error) {
	start := time.Now()
	summary := Summary{
		Mode: mode, Scanned: 0, Preserved: 0, Cleared: 0, Noop: 0, Updated: 0,
		Batches: 0, Attempts: 0, ByAction: map[string]int64{}, Remaining: 0, Elapsed: 0,
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

	attempts := 1
	if mode == ModeApply {
		attempts = r.options.ApplyAttempts
	}

	var remaining int64
	for attempt := 1; attempt <= attempts; attempt++ {
		summary.Attempts = int64(attempt)
		if err := r.runPass(ctx, mode, &summary); err != nil {
			return summary, err
		}

		var err error
		remaining, err = r.remaining(ctx)
		if err != nil {
			return summary, err
		}
		if remaining == 0 || mode != ModeApply || attempt == attempts {
			break
		}
		r.logger.WarnContext(ctx, "legacy policy scopes remain after apply pass, retrying",
			attr.SlogRiskPolicyScopeFoldRemaining(remaining),
			attr.SlogRiskPolicyScopeFoldAttempt(int64(attempt)),
		)
		if err := sleep(ctx, r.options.RetryDelay); err != nil {
			return summary, err
		}
	}

	summary.Remaining = remaining
	summary.Elapsed = time.Since(start)
	if mode == ModeApply && remaining > 0 {
		return summary, fmt.Errorf("%w: %d (rows held by another session were skipped)", ErrIncompleteApply, remaining)
	}
	return summary, nil
}

// runPass walks the whole candidate set once.
func (r *Runner) runPass(ctx context.Context, mode Mode, summary *Summary) error {
	// Dry-run walks past rows it does not write, so it needs a moving cursor;
	// apply re-reads from zero each batch because folded rows drop out of the
	// candidate set.
	after := uuid.Nil
	for {
		batch, err := r.runBatch(ctx, mode, after, summary)
		if err != nil {
			return err
		}
		if batch == uuid.Nil {
			return nil
		}
		summary.Batches++
		if mode == ModeDryRun {
			after = batch
		}
	}
}

// sleep waits for d, returning early if ctx is done.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("wait before retry: %w", err)
		}
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait before retry: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
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
	if err := r.setLocalTimeouts(ctx, q); err != nil {
		return uuid.Nil, err
	}

	rows, err := q.LockLegacyScopeBatch(ctx, LockLegacyScopeBatchParams{
		AfterID:   after,
		BatchSize: int32(r.options.BatchSize), //nolint:gosec // NewRunner rejects a batch size above math.MaxInt32
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

	config, err := withDetectionScopes(row.AnalyzerConfig, result.DetectionScopes)
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

// setLocalTimeouts applies the configured lock and statement timeouts to the
// transaction q runs in.
func (r *Runner) setLocalTimeouts(ctx context.Context, q *Queries) error {
	if _, err := q.SetLocalTimeouts(ctx, SetLocalTimeoutsParams{
		LockTimeout:      durationSetting(r.options.LockTimeout, "5s"),
		StatementTimeout: durationSetting(r.options.StatementTimeout, "30s"),
	}); err != nil {
		return fmt.Errorf("set local timeouts: %w", err)
	}
	return nil
}

// withTimeouts runs fn inside a transaction carrying those timeouts. The
// counting queries scan every policy row and run on every path, dry-run and
// validate included, so they must be bounded too: an unbounded count against a
// table someone else has locked hangs the run instead of failing it.
func (r *Runner) withTimeouts(ctx context.Context, fn func(*Queries) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin read transaction: %w", err)
	}
	// Read-only work: unwind rather than commit.
	defer func() { _ = tx.Rollback(ctx) }()

	q := New(tx)
	if err := r.setLocalTimeouts(ctx, q); err != nil {
		return err
	}
	return fn(q)
}

func (r *Runner) remaining(ctx context.Context) (int64, error) {
	var remaining int64
	err := r.withTimeouts(ctx, func(q *Queries) error {
		var countErr error
		remaining, countErr = q.CountRemainingLegacyScopes(ctx)
		return countErr
	})
	if err != nil {
		return 0, fmt.Errorf("count remaining legacy scopes: %w", err)
	}
	return remaining, nil
}

// CountByAction reports the pre-run population, which is what decides how much
// of the fleet the preserve path touches.
func (r *Runner) CountByAction(ctx context.Context) (map[string]int64, error) {
	var rows []CountLegacyScopesByActionRow
	err := r.withTimeouts(ctx, func(q *Queries) error {
		var countErr error
		rows, countErr = q.CountLegacyScopesByAction(ctx)
		return countErr
	})
	if err != nil {
		return nil, fmt.Errorf("count legacy scopes by action: %w", err)
	}
	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		out[row.Action] = row.Total
	}
	return out, nil
}

// checkTimeout rejects a positive timeout that would serialize to 0ms. Zero
// itself is allowed: it selects the built-in fallback rather than disabling
// the timeout.
func checkTimeout(name string, d time.Duration) error {
	if d != 0 && d < time.Millisecond {
		return fmt.Errorf("%s must be at least 1ms, got %s", name, d)
	}
	return nil
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
