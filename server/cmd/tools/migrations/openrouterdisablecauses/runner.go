package openrouterdisablecauses

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Mode string

const (
	ModeDryRun         Mode = "dry-run"
	ModeApply          Mode = "apply"
	ModeValidate       Mode = "validate"
	ModeManualOverride Mode = "manual-override"
)

var (
	ErrAmbiguousRows              = errors.New("ambiguous OpenRouter key rows remain unclassified")
	ErrValidationFailed           = errors.New("OpenRouter disable cause validation failed")
	ErrManualOverrideUnauthorized = errors.New("manual override is not authorized")
	ErrManualOverrideConflict     = errors.New("manual override conflicts with an existing classification")
)

const (
	ValidationNull                     = "null"
	ValidationMirrorMismatch           = "mirror_mismatch"
	ValidationUnknownCause             = "unknown_cause"
	ValidationDuplicateCause           = "duplicate_cause"
	ValidationAmbiguous                = "ambiguous"
	ValidationReclassificationMismatch = "reclassification_mismatch"
)

type Options struct {
	BatchSize        int
	LockTimeout      time.Duration
	StatementTimeout time.Duration
	MaxLockRetries   int
}

type Summary struct {
	Mode           Mode             `json:"mode"`
	Scanned        int64            `json:"scanned"`
	Classified     int64            `json:"classified"`
	Updated        int64            `json:"updated"`
	CauseSets      map[string]int64 `json:"cause_sets"`
	Ambiguous      map[string]int64 `json:"ambiguous"`
	Validation     map[string]int64 `json:"validation"`
	SkippedDeleted int64            `json:"skipped_deleted"`
	Batches        int64            `json:"batches"`
	LockRetries    int64            `json:"lock_retries"`
	RemainingNulls int64            `json:"remaining_nulls"`
	Elapsed        time.Duration    `json:"-"`
}

type Runner struct {
	pool         *pgxpool.Pool
	logger       *slog.Logger
	options      Options
	beforeCommit func() error
}

func NewRunner(pool *pgxpool.Pool, logger *slog.Logger, options Options) *Runner {
	if options.BatchSize <= 0 {
		options.BatchSize = 100
	}
	if options.LockTimeout <= 0 {
		options.LockTimeout = 2 * time.Second
	}
	if options.StatementTimeout <= 0 {
		options.StatementTimeout = 30 * time.Second
	}
	if options.MaxLockRetries < 0 {
		options.MaxLockRetries = 0
	}
	return &Runner{pool: pool, logger: logger, options: options}
}

func (r *Runner) Run(ctx context.Context, mode Mode) (Summary, error) {
	started := time.Now()
	summary := Summary{Mode: mode, CauseSets: map[string]int64{}, Ambiguous: map[string]int64{}, Validation: map[string]int64{}}
	if mode == ModeValidate {
		return r.validate(ctx, summary, started)
	}
	if mode != ModeDryRun && mode != ModeApply {
		return summary, fmt.Errorf("unsupported migration mode %q", mode)
	}

	afterOrg, afterKey := "", ""
	emptyPasses := 0
	for {
		rows, err := r.runBatch(ctx, mode, afterOrg, afterKey, &summary)
		if err != nil {
			if isLockTimeout(err) && summary.LockRetries < int64(r.options.MaxLockRetries) {
				summary.LockRetries++
				select {
				case <-ctx.Done():
					return summary, ctx.Err()
				case <-time.After(25 * time.Millisecond):
					continue
				}
			}
			return summary, err
		}
		if len(rows) > 0 {
			r.logger.InfoContext(ctx, "OpenRouter disable cause classification batch complete",
				slog.String("mode", string(mode)),
				slog.Int64("batches", summary.Batches),
				slog.Int64("scanned", summary.Scanned),
				slog.Int64("classified", summary.Classified),
				slog.Int64("updated", summary.Updated),
				slog.Int64("ambiguous", sumCounts(summary.Ambiguous)),
			)
			afterOrg = rows[len(rows)-1].OrganizationID
			afterKey = rows[len(rows)-1].KeyType
			emptyPasses = 0
			continue
		}

		remaining, err := New(r.pool).CountLiveNullClassifications(ctx)
		if err != nil {
			return summary, fmt.Errorf("count remaining NULL classifications: %w", err)
		}
		summary.RemainingNulls = remaining
		if mode == ModeApply && remaining > 0 && len(summary.Ambiguous) == 0 && emptyPasses < r.options.MaxLockRetries {
			emptyPasses++
			summary.LockRetries++
			afterOrg, afterKey = "", ""
			time.Sleep(25 * time.Millisecond)
			continue
		}
		break
	}

	summary.Elapsed = time.Since(started)
	if len(summary.Ambiguous) > 0 {
		return summary, ErrAmbiguousRows
	}
	if mode == ModeApply && summary.RemainingNulls > 0 {
		return summary, fmt.Errorf("%d live rows remain NULL after bounded retries", summary.RemainingNulls)
	}
	return summary, nil
}

func (r *Runner) runBatch(ctx context.Context, mode Mode, afterOrg, afterKey string, summary *Summary) ([]LockClassificationBatchRow, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin classification batch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := New(tx)
	if _, err := q.SetLocalTimeouts(ctx, SetLocalTimeoutsParams{LockTimeout: r.options.LockTimeout.String(), StatementTimeout: r.options.StatementTimeout.String()}); err != nil {
		return nil, fmt.Errorf("set classification timeouts: %w", err)
	}
	rows, err := q.LockClassificationBatch(ctx, LockClassificationBatchParams{AfterOrganizationID: afterOrg, AfterKeyType: afterKey, BatchSize: int32(r.options.BatchSize)})
	if err != nil {
		return nil, fmt.Errorf("lock classification batch: %w", err)
	}
	if len(rows) == 0 {
		return rows, nil
	}

	summary.Batches++
	var batchUpdated int64
	for _, row := range rows {
		summary.Scanned++
		classification := Classify(projectionFromRow(row))
		if !classification.Classified {
			summary.Ambiguous[classification.AmbiguousReason]++
			continue
		}
		summary.Classified++
		summary.CauseSets[causeSetName(classification.Causes)]++
		if mode == ModeDryRun {
			continue
		}

		updated, err := q.CompareAndSetClassification(ctx, CompareAndSetClassificationParams{DisableCauses: classification.Causes, OrganizationID: row.OrganizationID, KeyType: row.KeyType})
		if err != nil {
			return nil, fmt.Errorf("persist classified OpenRouter key: %w", err)
		}
		batchUpdated += updated
	}

	if mode == ModeDryRun {
		return rows, nil
	}
	if r.beforeCommit != nil {
		if err := r.beforeCommit(); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit classification batch: %w", err)
	}
	summary.Updated += batchUpdated
	return rows, nil
}

func projectionFromRow(row LockClassificationBatchRow) Projection {
	return projectionFromFields(row.LegacyDisabled, row.TrialState, row.BillingState, classifyAdmin(row))
}

type adminMetadata struct {
	KeyType string `json:"key_type"`
}
type adminSnapshot struct {
	Disabled      *bool    `json:"disabled"`
	DisableCauses []string `json:"disable_causes"`
}

func classifyAdmin(row LockClassificationBatchRow) AdminState {
	if row.AdminAction == "" {
		return AdminNone
	}
	var metadata adminMetadata
	var before, after adminSnapshot
	if json.Unmarshal(row.AdminMetadata, &metadata) != nil || json.Unmarshal(row.AdminBeforeSnapshot, &before) != nil || json.Unmarshal(row.AdminAfterSnapshot, &after) != nil {
		return AdminMalformed
	}
	if metadata.KeyType != row.KeyType || !validAdminSnapshot(before) || !validAdminSnapshot(after) {
		return AdminMalformed
	}
	hasAdminLock := slices.Contains(after.DisableCauses, CauseAdminLock)
	switch row.AdminAction {
	case "openrouter-key:disable":
		if !hasAdminLock {
			return AdminMalformed
		}
		return AdminDisabled
	case "openrouter-key:enable":
		if hasAdminLock {
			return AdminMalformed
		}
		return AdminEnabled
	default:
		return AdminMalformed
	}
}

func validAdminSnapshot(snapshot adminSnapshot) bool {
	if snapshot.Disabled == nil || snapshot.DisableCauses == nil {
		return false
	}
	causes, err := CanonicalizeCauses(snapshot.DisableCauses)
	return err == nil && *snapshot.Disabled == (len(causes) > 0)
}

func causeSetName(causes []string) string {
	if len(causes) == 0 {
		return "enabled"
	}
	var result strings.Builder
	result.WriteString(causes[0])
	for _, cause := range causes[1:] {
		result.WriteString("+" + cause)
	}
	return result.String()
}

func isLockTimeout(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "55P03" || pgErr.Code == "57014")
}

func sumCounts(counts map[string]int64) int64 {
	var total int64
	for _, count := range counts {
		total += count
	}
	return total
}

func projectionFromFields(legacyDisabled bool, trialState, billingState string, admin AdminState) Projection {
	p := Projection{LegacyDisabled: legacyDisabled, Admin: admin}
	switch trialState {
	case "demoted":
		p.Trial = TrialDemoted
	case "contradictory":
		p.Trial = TrialContradictory
	}
	switch billingState {
	case "active":
		p.Billing = BillingActive
	case "inactive":
		p.Billing = BillingInactive
	case "inconsistent":
		p.Billing = BillingInconsistent
	}
	return p
}

func (r *Runner) validate(ctx context.Context, summary Summary, started time.Time) (Summary, error) {
	afterOrg, afterKey := "", ""
	q := New(r.pool)
	for {
		rows, err := q.ListValidationBatch(ctx, ListValidationBatchParams{AfterOrganizationID: afterOrg, AfterKeyType: afterKey, BatchSize: int32(r.options.BatchSize)})
		if err != nil {
			return summary, fmt.Errorf("list OpenRouter validation batch: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		summary.Batches++
		for _, row := range rows {
			summary.Scanned++
			classification := Classify(projectionFromFields(row.LegacyDisabled, row.TrialState, row.BillingState, classifyValidationAdmin(row)))
			if row.DisableCauses == nil {
				if !classification.Classified {
					summary.Validation[ValidationAmbiguous]++
				}
				summary.Validation[ValidationNull]++
				continue
			}
			if row.LegacyDisabled != (len(row.DisableCauses) > 0) {
				summary.Validation[ValidationMirrorMismatch]++
			}
			seen := make(map[string]bool, len(row.DisableCauses))
			duplicate := false
			for _, cause := range row.DisableCauses {
				if seen[cause] {
					duplicate = true
				}
				seen[cause] = true
			}
			if duplicate {
				summary.Validation[ValidationDuplicateCause]++
			}
			canonical, canonicalErr := CanonicalizeCauses(row.DisableCauses)
			if canonicalErr != nil {
				summary.Validation[ValidationUnknownCause]++
			} else if classification.Classified && !slices.Equal(canonical, classification.Causes) {
				summary.Validation[ValidationReclassificationMismatch]++
			}
		}
		afterOrg = rows[len(rows)-1].OrganizationID
		afterKey = rows[len(rows)-1].KeyType
	}
	deleted, err := q.CountDeletedNullClassifications(ctx)
	if err != nil {
		return summary, fmt.Errorf("count deleted NULL classifications: %w", err)
	}
	summary.SkippedDeleted = deleted
	summary.Elapsed = time.Since(started)
	if len(summary.Validation) > 0 {
		return summary, ErrValidationFailed
	}
	return summary, nil
}

func classifyValidationAdmin(row ListValidationBatchRow) AdminState {
	return classifyAdmin(LockClassificationBatchRow{
		KeyType: row.KeyType, AdminAction: row.AdminAction, AdminMetadata: row.AdminMetadata,
		AdminBeforeSnapshot: row.AdminBeforeSnapshot, AdminAfterSnapshot: row.AdminAfterSnapshot,
	})
}

type ManualOverride struct {
	OrganizationID string   `json:"organization_id"`
	KeyType        string   `json:"key_type"`
	Causes         []string `json:"causes"`
}

func AuthorizeManualOverride(provided, expected string) error {
	if provided == "" || expected == "" || len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		return ErrManualOverrideUnauthorized
	}
	return nil
}

func (r *Runner) ApplyManualOverride(ctx context.Context, override ManualOverride) (bool, error) {
	causes, err := CanonicalizeCauses(override.Causes)
	if err != nil {
		return false, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin manual override: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := New(tx)
	if _, err := q.SetLocalTimeouts(ctx, SetLocalTimeoutsParams{LockTimeout: r.options.LockTimeout.String(), StatementTimeout: r.options.StatementTimeout.String()}); err != nil {
		return false, fmt.Errorf("set manual override timeouts: %w", err)
	}
	target, err := q.GetManualOverrideTarget(ctx, GetManualOverrideTargetParams{OrganizationID: override.OrganizationID, KeyType: override.KeyType})
	if err != nil {
		return false, fmt.Errorf("read manual override target: %w", err)
	}
	if target.DisableCauses != nil {
		if slices.Equal(target.DisableCauses, causes) {
			return false, nil
		}
		return false, ErrManualOverrideConflict
	}
	if target.Disabled != (len(causes) > 0) {
		return false, fmt.Errorf("manual override would change effective access")
	}
	updated, err := q.CompareAndSetClassification(ctx, CompareAndSetClassificationParams{DisableCauses: causes, OrganizationID: override.OrganizationID, KeyType: override.KeyType})
	if err != nil {
		return false, fmt.Errorf("persist manual override: %w", err)
	}
	if updated != 1 {
		return false, ErrManualOverrideConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit manual override: %w", err)
	}
	return true, nil
}
