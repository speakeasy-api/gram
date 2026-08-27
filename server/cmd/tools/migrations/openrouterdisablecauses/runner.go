package openrouterdisablecauses

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
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
	ValidationOverrideMismatch         = "override_mismatch"
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
	pool                 *pgxpool.Pool
	logger               *slog.Logger
	options              Options
	beforeCommit         func() error
	afterValidationBatch func()
	beforeRemainingCount func()
}

func NewRunner(pool *pgxpool.Pool, logger *slog.Logger, options Options) *Runner {
	if options.BatchSize <= 0 {
		options.BatchSize = 100
	} else if options.BatchSize > math.MaxInt32 {
		options.BatchSize = math.MaxInt32
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
	return &Runner{pool: pool, logger: logger, options: options, beforeCommit: nil, afterValidationBatch: nil, beforeRemainingCount: nil}
}

func (r *Runner) Run(ctx context.Context, mode Mode) (Summary, error) {
	started := time.Now()
	summary := Summary{
		Mode: mode, Scanned: 0, Classified: 0, Updated: 0, CauseSets: map[string]int64{},
		Ambiguous: map[string]int64{}, Validation: map[string]int64{}, SkippedDeleted: 0,
		Batches: 0, LockRetries: 0, RemainingNulls: 0, Elapsed: 0,
	}
	if mode == ModeValidate {
		return r.validate(ctx, summary, started, nil)
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
					return summary, fmt.Errorf("wait to retry classification batch: %w", ctx.Err())
				case <-time.After(25 * time.Millisecond):
					continue
				}
			}
			return summary, err
		}
		if len(rows) > 0 {
			r.logger.InfoContext(ctx, "OpenRouter disable cause classification batch complete",
				attr.SlogOpenRouterBackfillMode(string(mode)),
				attr.SlogOpenRouterBackfillBatches(summary.Batches),
				attr.SlogOpenRouterBackfillScanned(summary.Scanned),
				attr.SlogOpenRouterBackfillClassified(summary.Classified),
				attr.SlogOpenRouterBackfillUpdated(summary.Updated),
				attr.SlogOpenRouterBackfillAmbiguous(sumCounts(summary.Ambiguous)),
			)
			afterOrg = rows[len(rows)-1].OrganizationID
			afterKey = rows[len(rows)-1].KeyType
			emptyPasses = 0
			continue
		}

		if r.beforeRemainingCount != nil {
			r.beforeRemainingCount()
		}
		remaining, err := r.countLiveNullClassifications(ctx)
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

func (r *Runner) countLiveNullClassifications(ctx context.Context) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin live NULL count: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := New(tx)
	if _, err := q.SetLocalTimeouts(ctx, SetLocalTimeoutsParams{LockTimeout: r.options.LockTimeout.String(), StatementTimeout: r.options.StatementTimeout.String()}); err != nil {
		return 0, fmt.Errorf("set live NULL count timeouts: %w", err)
	}
	count, err := q.CountLiveNullClassifications(ctx)
	if err != nil {
		return 0, fmt.Errorf("execute live NULL count: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit live NULL count: %w", err)
	}
	return count, nil
}

func (r *Runner) runBatch(ctx context.Context, mode Mode, afterOrg, afterKey string, summary *Summary) ([]LockClassificationBatchRow, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: "", AccessMode: "", DeferrableMode: "", BeginQuery: "", CommitQuery: ""})
	if err != nil {
		return nil, fmt.Errorf("begin classification batch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := New(tx)
	if _, err := q.SetLocalTimeouts(ctx, SetLocalTimeoutsParams{LockTimeout: r.options.LockTimeout.String(), StatementTimeout: r.options.StatementTimeout.String()}); err != nil {
		return nil, fmt.Errorf("set classification timeouts: %w", err)
	}
	rows, err := q.LockClassificationBatch(ctx, LockClassificationBatchParams{AfterOrganizationID: afterOrg, AfterKeyType: afterKey, BatchSize: conv.SafeInt32(r.options.BatchSize)})
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
	if json.Unmarshal(row.AdminMetadata, &metadata) != nil || metadata.KeyType != row.KeyType {
		return AdminMalformed
	}

	legacyNullSnapshots := len(row.AdminBeforeSnapshot) == 0 && len(row.AdminAfterSnapshot) == 0
	if !legacyNullSnapshots {
		var before, after adminSnapshot
		if len(row.AdminBeforeSnapshot) == 0 || len(row.AdminAfterSnapshot) == 0 ||
			json.Unmarshal(row.AdminBeforeSnapshot, &before) != nil || json.Unmarshal(row.AdminAfterSnapshot, &after) != nil ||
			!validAdminSnapshot(before) || !validAdminSnapshot(after) || !validAdminTransition(row.AdminAction, before, after) {
			return AdminMalformed
		}
	}

	switch row.AdminAction {
	case "openrouter-key:disable":
		return AdminDisabled
	case "openrouter-key:enable":
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
	return err == nil && slices.Equal(snapshot.DisableCauses, causes) && *snapshot.Disabled == (len(causes) > 0)
}

func validAdminTransition(action string, before, after adminSnapshot) bool {
	beforeHasLock := slices.Contains(before.DisableCauses, CauseAdminLock)
	afterHasLock := slices.Contains(after.DisableCauses, CauseAdminLock)
	withoutAdminLock := func(causes []string) []string {
		result := make([]string, 0, len(causes))
		for _, cause := range causes {
			if cause != CauseAdminLock {
				result = append(result, cause)
			}
		}
		return result
	}
	beforeOther := withoutAdminLock(before.DisableCauses)
	afterOther := withoutAdminLock(after.DisableCauses)
	if !slices.Equal(beforeOther, afterOther) {
		return false
	}
	switch action {
	case "openrouter-key:disable":
		return !beforeHasLock && afterHasLock
	case "openrouter-key:enable":
		return beforeHasLock && !afterHasLock
	default:
		return false
	}
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
	return errors.As(err, &pgErr) && (pgErr.Code == pgerrcode.LockNotAvailable || pgErr.Code == pgerrcode.QueryCanceled)
}

func sumCounts(counts map[string]int64) int64 {
	var total int64
	for _, count := range counts {
		total += count
	}
	return total
}

func projectionFromFields(legacyDisabled bool, trialState, billingState string, admin AdminState) Projection {
	p := Projection{LegacyDisabled: legacyDisabled, Trial: TrialNone, Billing: BillingIrrelevant, Admin: admin}
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

// Validate proves consistency at one read-only REPEATABLE READ snapshot. Success
// is not a writer fence or contract handoff; callers must validate again after
// normal sweeps, and the later contract migration must enforce its preconditions
// atomically after every provenance writer is cause-aware.
func (r *Runner) Validate(ctx context.Context, overrides []ManualOverride) (Summary, error) {
	started := time.Now()
	summary := Summary{
		Mode: ModeValidate, Scanned: 0, Classified: 0, Updated: 0, CauseSets: map[string]int64{},
		Ambiguous: map[string]int64{}, Validation: map[string]int64{}, SkippedDeleted: 0,
		Batches: 0, LockRetries: 0, RemainingNulls: 0, Elapsed: 0,
	}
	approved := make(map[string][]string, len(overrides))
	for _, override := range overrides {
		causes, err := CanonicalizeCauses(override.Causes)
		if err != nil || override.OrganizationID == "" || override.KeyType == "" {
			return summary, fmt.Errorf("invalid validation override manifest")
		}
		key := override.OrganizationID + "\x00" + override.KeyType
		if _, duplicate := approved[key]; duplicate {
			return summary, fmt.Errorf("duplicate validation override manifest entry")
		}
		approved[key] = causes
	}
	return r.validate(ctx, summary, started, approved)
}

// validate proves the complete population at one PostgreSQL snapshot. It is not
// a writer fence or contract handoff: a later validation must detect later
// mutations, and the contract migration must atomically enforce its own
// preconditions after the cause-aware writer cutover.
func (r *Runner) validate(ctx context.Context, summary Summary, started time.Time, approved map[string][]string) (Summary, error) {
	approvedSeen := make(map[string]bool, len(approved))
	if err := r.validateSnapshot(ctx, &summary, approved, approvedSeen); err != nil {
		return summary, err
	}
	for key := range approved {
		if !approvedSeen[key] {
			summary.Validation[ValidationOverrideMismatch]++
		}
	}
	summary.Elapsed = time.Since(started)
	if len(summary.Validation) > 0 {
		return summary, ErrValidationFailed
	}
	return summary, nil
}

func (r *Runner) validateSnapshot(ctx context.Context, summary *Summary, approved map[string][]string, approvedSeen map[string]bool) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: "", BeginQuery: "", CommitQuery: "",
	})
	if err != nil {
		return fmt.Errorf("begin validation snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := New(tx)
	if _, err := q.SetLocalTimeouts(ctx, SetLocalTimeoutsParams{LockTimeout: r.options.LockTimeout.String(), StatementTimeout: r.options.StatementTimeout.String()}); err != nil {
		return fmt.Errorf("set validation timeouts: %w", err)
	}

	afterOrg, afterKey := "", ""
	for {
		rows, err := q.ListValidationBatch(ctx, ListValidationBatchParams{AfterOrganizationID: afterOrg, AfterKeyType: afterKey, BatchSize: conv.SafeInt32(r.options.BatchSize)})
		if err != nil {
			return fmt.Errorf("list OpenRouter validation batch: %w", err)
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
			if !classification.Classified {
				key := row.OrganizationID + "\x00" + row.KeyType
				if causes, ok := approved[key]; ok && slices.Equal(causes, row.DisableCauses) {
					approvedSeen[key] = true
				} else {
					summary.Validation[ValidationAmbiguous]++
				}
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
		if r.afterValidationBatch != nil {
			r.afterValidationBatch()
		}
	}
	remaining, err := q.CountLiveNullClassifications(ctx)
	if err != nil {
		return fmt.Errorf("count live NULL classifications during validation: %w", err)
	}
	summary.RemainingNulls = remaining
	if remaining > summary.Validation[ValidationNull] {
		summary.Validation[ValidationNull] = remaining
	}
	deleted, err := q.CountDeletedNullClassifications(ctx)
	if err != nil {
		return fmt.Errorf("count deleted NULL classifications: %w", err)
	}
	summary.SkippedDeleted = deleted
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit validation snapshot: %w", err)
	}
	return nil
}

func classifyValidationAdmin(row ListValidationBatchRow) AdminState {
	return classifyAdmin(LockClassificationBatchRow{
		OrganizationID: row.OrganizationID, KeyType: row.KeyType, LegacyDisabled: row.LegacyDisabled,
		TrialState: row.TrialState, BillingState: row.BillingState, AdminAction: row.AdminAction,
		AdminMetadata: row.AdminMetadata, AdminBeforeSnapshot: row.AdminBeforeSnapshot,
		AdminAfterSnapshot: row.AdminAfterSnapshot,
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
