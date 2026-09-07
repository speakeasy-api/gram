package killswitches

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/killswitches/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// maintenanceActorUserID is the synthetic actor recorded on background-emitted
// audit entries, matching the fleet's existing system-job convention.
const (
	maintenanceActorUserID      = "system"
	maintenanceActorDisplayName = "System"
)

// MaintenanceService owns the privileged cross-organization killswitch
// maintenance transactions: version-specific expiry history and operation
// receipt retention. It records history only; query-time database state stays
// authoritative for enforcement, and no method here mutates prescription
// headers or versions.
type MaintenanceService struct {
	db          *pgxpool.Pool
	auditLogger *audit.Logger

	// beforeExpiryCommit runs after the marker and audit writes but before the
	// expiry transaction commits. Test seam for retry-recovery coverage.
	beforeExpiryCommit func(context.Context) error
}

func NewMaintenanceService(db *pgxpool.Pool, auditLogger *audit.Logger) *MaintenanceService {
	return &MaintenanceService{db: db, auditLogger: auditLogger, beforeExpiryCommit: nil}
}

// ExpiryBatchResult summarizes one bounded expiry batch as aggregate counts
// only: how many due candidates discovery returned and how many of them were
// recorded. Candidates that raced with a concurrent sweep or became ineligible
// under the row lock are counted but not recorded, so drain decisions must
// follow Candidates, not Recorded.
type ExpiryBatchResult struct {
	Candidates int64 `json:"candidates"`
	Recorded   int64 `json:"recorded"`
}

// RecordDueExpiries records expiry history for one bounded batch of versions
// that reached their deadline while still current: active state, a due
// expires_at, and no supersession before the deadline. Each candidate commits
// its idempotency marker, typed audit entry, and outbox row in one
// transaction; only the transaction whose marker insert succeeds audits.
func (s *MaintenanceService) RecordDueExpiries(ctx context.Context, batchSize int32) (ExpiryBatchResult, error) {
	var result ExpiryBatchResult
	if batchSize < 1 || batchSize > maxCleanupBatchSize {
		return result, fmt.Errorf("%w: expiry batch size must be between 1 and %d", ErrInvalidArgument, maxCleanupBatchSize)
	}

	candidates, err := repo.New(s.db).ListDueKillswitchExpiries(ctx, batchSize)
	if err != nil {
		return result, fmt.Errorf("list due killswitch expiries: %w", err)
	}
	result.Candidates = int64(len(candidates))

	for _, candidate := range candidates {
		count, err := s.recordExpiry(ctx, candidate)
		if err != nil {
			return result, err
		}
		result.Recorded += count
	}
	return result, nil
}

func (s *MaintenanceService) recordExpiry(ctx context.Context, candidate repo.ListDueKillswitchExpiriesRow) (int64, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: "", DeferrableMode: "", BeginQuery: "", CommitQuery: ""})
	if err != nil {
		return 0, fmt.Errorf("begin killswitch expiry transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := repo.New(tx)

	_, err = queries.LockKillswitchPrescriptionCurrent(ctx, repo.LockKillswitchPrescriptionCurrentParams{OrganizationID: candidate.OrganizationID, PrescriptionID: candidate.PrescriptionID})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("lock killswitch prescription for expiry: %w", err)
	}

	locked, err := queries.LockKillswitchVersionForExpiry(ctx, repo.LockKillswitchVersionForExpiryParams(candidate))
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("lock killswitch version for expiry: %w", err)
	}
	if !expiryEligible(locked) {
		return 0, nil
	}

	rows, err := queries.RecordKillswitchExpiryEvent(ctx, repo.RecordKillswitchExpiryEventParams(candidate))
	if err != nil {
		return 0, fmt.Errorf("record killswitch expiry event: %w", err)
	}
	if rows != 1 {
		return 0, nil
	}

	actorDisplayName := maintenanceActorDisplayName
	if err := s.auditLogger.LogKillswitchExpire(ctx, tx, audit.LogKillswitchExpireEvent{
		OrganizationID:   candidate.OrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, maintenanceActorUserID),
		ActorDisplayName: &actorDisplayName,
		PrescriptionURN:  urn.NewKillswitchPrescription(candidate.PrescriptionID),
		Version:          candidate.Version,
		ExpiredAt:        locked.ExpiresAt.Time.UTC(),
	}); err != nil {
		return 0, fmt.Errorf("audit killswitch expiry: %w", err)
	}

	if s.beforeExpiryCommit != nil {
		if err := s.beforeExpiryCommit(ctx); err != nil {
			return 0, fmt.Errorf("before killswitch expiry commit: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit killswitch expiry transaction: %w", err)
	}
	return 1, nil
}

func expiryEligible(locked repo.LockKillswitchVersionForExpiryRow) bool {
	if PrescriptionState(locked.State) != PrescriptionStateActive {
		return false
	}
	if !locked.ExpiresAt.Valid || !locked.DatabaseNow.Valid || locked.ExpiresAt.Time.After(locked.DatabaseNow.Time) {
		return false
	}
	// A version replaced at or before its deadline never expired; only expiry
	// strictly before supersession is durable history.
	if locked.SupersededAt.Valid && !locked.ExpiresAt.Time.Before(locked.SupersededAt.Time) {
		return false
	}
	return true
}

// CleanupExpiredOperationsGlobal deletes one bounded, ordered batch of expired
// operation receipts across all organizations. Cleanup is physical retention
// only: replay eligibility ends at each receipt's expires_at regardless of
// when cleanup runs. The returned value is a row count only.
func (s *MaintenanceService) CleanupExpiredOperationsGlobal(ctx context.Context, batchSize int32) (int64, error) {
	if batchSize < 1 || batchSize > maxCleanupBatchSize {
		return 0, fmt.Errorf("%w: cleanup batch size must be between 1 and %d", ErrInvalidArgument, maxCleanupBatchSize)
	}
	rows, err := repo.New(s.db).DeleteExpiredKillswitchOperationsGlobal(ctx, batchSize)
	if err != nil {
		return 0, fmt.Errorf("delete expired killswitch operations: %w", err)
	}
	return rows, nil
}
