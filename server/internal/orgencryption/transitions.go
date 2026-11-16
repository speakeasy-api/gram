package orgencryption

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// TransitionState is where an organization is in moving one use case's values
// from the platform key to its own key. Stored in
// organization_encryption_transitions.state.
type TransitionState string

const (
	// TransitionStatePlatformKey writes and reads only the platform-key copy.
	// An organization with no transition row is in this state.
	TransitionStatePlatformKey TransitionState = "platform_key"

	// TransitionStateDualWrite writes both copies and reads the
	// organization-key copy when it exists.
	TransitionStateDualWrite TransitionState = "dual_write"

	// TransitionStateOrgKMS writes and reads only the organization-key copy.
	// No transition leaves it.
	TransitionStateOrgKMS TransitionState = "org_kms"
)

func parseTransitionState(value string) (TransitionState, error) {
	switch state := TransitionState(value); state {
	case TransitionStatePlatformKey, TransitionStateDualWrite, TransitionStateOrgKMS:
		return state, nil
	default:
		return "", fmt.Errorf("%w: unknown transition state %q", ErrInvalidState, value)
	}
}

const (
	// transitionCacheTTL is how long a process trusts a transition state read.
	// It matches configCacheTTL. Fencing, not this TTL, keeps a stale read
	// from writing the wrong copies.
	transitionCacheTTL = configCacheTTL

	// transitionCacheSize bounds the transition states one process remembers:
	// one per (organization, use case) it serves.
	transitionCacheSize = 4096

	// MaxDecisionHold is the longest a Decision may be held before sealing
	// with it. It bounds how long a writer can act on a state other
	// processes have already left, such as across a provider call.
	MaxDecisionHold = 2 * time.Minute

	// minimumDualWriteSlack covers clock skew and a write already in flight
	// when the floor elapses.
	minimumDualWriteSlack = time.Minute

	// MinimumDualWriteFloor is the shortest time in dual_write that finalize
	// accepts, whatever the caller asks for. After it, no process can still
	// hold a platform_key state read or decision from before the
	// enrollment, so no write that skips the organization-key copy can
	// land after finalize.
	MinimumDualWriteFloor = transitionCacheTTL + MaxDecisionHold + minimumDualWriteSlack

	// DefaultMinimumDualWrite is a suggested soak time for callers that offer
	// finalize. Failing closed in dual_write surfaces key problems while
	// the platform-key copies still exist, and a day covers daily traffic
	// patterns and at least one hourly backfill and verification.
	DefaultMinimumDualWrite = 24 * time.Hour

	// DefaultClearSettleDelay is how long clearing waits after a pass before
	// counting platform-key copies again. A write statement that started
	// before finalize committed can still land after a batch cleared its row,
	// because its fence check reads the state as of its own start.
	DefaultClearSettleDelay = time.Minute

	// maxClearPasses bounds the clear, settle and count cycles in one call.
	// Copies that keep reappearing point to a writer that ignores the fence,
	// so clearing stops and reports it instead of looping.
	maxClearPasses = 5

	// clearBatchSize is how many platform-key copies one clear statement
	// destroys, keeping each statement's row locks short.
	clearBatchSize = 500
)

// Transition is one use case's transition state for an organization.
type Transition struct {
	// ID is the state row's id, or uuid.Nil when the organization has no row
	// and is therefore in platform_key.
	ID uuid.UUID

	// OrganizationID owns the values being moved.
	OrganizationID string

	// UseCase is the use case being moved.
	UseCase UseCase

	// State decides which copies are written and read.
	State TransitionState

	// Generation increases on every state change and is 0 without a row.
	// Writers fence on it.
	Generation int64

	// StateChangedAt is when State was entered; zero without a row.
	StateChangedAt time.Time

	// PlatformCopiesClearedAt is when finalize finished destroying the
	// platform-key copies. Nil until then.
	PlatformCopiesClearedAt *time.Time

	// LastBackfill is the last backfill run.
	LastBackfill BackfillRecord

	// LastVerifiedAt is when the last clean verification pass started.
	LastVerifiedAt *time.Time
}

// BackfillRecord is the last backfill run recorded on a transition row.
type BackfillRecord struct {
	// StartedAt is when the run started; nil if none ran.
	StartedAt *time.Time

	// CompletedAt is when the run ended; nil while it runs or if none ran.
	CompletedAt *time.Time

	// Converted is how many values gained an organization-key copy.
	Converted *int64

	// Remaining is how many values still wait for an organization-key copy.
	Remaining *int64

	// Unreadable is how many values are recorded as unreadable.
	Unreadable *int64

	// Error is a short summary of why the run stopped early. It never
	// carries values, ciphertext or key material.
	Error *string
}

type transitionKey struct {
	organizationID string
	useCase        UseCase
}

// TransitionsConfig configures transition state changes.
type TransitionsConfig struct {
	// ClearSettleDelay is how long clearing waits before counting again.
	// Zero means DefaultClearSettleDelay.
	ClearSettleDelay time.Duration
}

// Transitions reads and changes transition state. Every process that seals
// or opens values reads it; only processes with a Provisioner and an audit
// logger change it.
type Transitions struct {
	logger      *slog.Logger
	db          *pgxpool.Pool
	audit       *audit.Logger
	provisioner *Provisioner
	cache       *expirable.LRU[transitionKey, Transition]
	settleDelay time.Duration
}

// NewTransitions wires transition state. auditLogger and provisioner may be
// nil in a process that only reads state; changes then fail.
func NewTransitions(logger *slog.Logger, db *pgxpool.Pool, auditLogger *audit.Logger, provisioner *Provisioner, cfg TransitionsConfig) *Transitions {
	settleDelay := cfg.ClearSettleDelay
	if settleDelay <= 0 {
		settleDelay = DefaultClearSettleDelay
	}

	return &Transitions{
		logger:      logger.With(attr.SlogComponent("orgencryption.transitions")),
		db:          db,
		audit:       auditLogger,
		provisioner: provisioner,
		cache:       expirable.NewLRU[transitionKey, Transition](transitionCacheSize, nil, transitionCacheTTL),
		settleDelay: settleDelay,
	}
}

// Get returns a use case's transition state, cached for transitionCacheTTL.
func (t *Transitions) Get(ctx context.Context, organizationID string, useCase UseCase) (Transition, error) {
	if err := useCase.validate(); err != nil {
		return Transition{}, err
	}

	if cached, ok := t.cache.Get(transitionKey{organizationID: organizationID, useCase: useCase}); ok {
		return cached, nil
	}

	return t.GetFresh(ctx, organizationID, useCase)
}

// GetFresh reads a use case's transition state from the database and
// refreshes the cache.
func (t *Transitions) GetFresh(ctx context.Context, organizationID string, useCase UseCase) (Transition, error) {
	if err := useCase.validate(); err != nil {
		return Transition{}, err
	}

	row, err := repo.New(t.db).GetEncryptionTransition(ctx, repo.GetEncryptionTransitionParams{OrganizationID: organizationID, UseCase: string(useCase)})
	var transition Transition
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		transition = noTransition(organizationID, useCase)
	case err != nil:
		return Transition{}, fmt.Errorf("load encryption transition: %w", err)
	default:
		transition, err = toTransition(row)
		if err != nil {
			return Transition{}, err
		}
	}

	t.cache.Add(transitionKey{organizationID: organizationID, useCase: useCase}, transition)

	return transition, nil
}

// List returns the organization's transition state for every registered use
// case, uncached, for the admin API.
func (t *Transitions) List(ctx context.Context, organizationID string) ([]Transition, error) {
	rows, err := repo.New(t.db).ListEncryptionTransitions(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list encryption transitions: %w", err)
	}

	byUseCase := make(map[UseCase]repo.OrganizationEncryptionTransition, len(rows))
	for _, row := range rows {
		byUseCase[UseCase(row.UseCase)] = row
	}

	transitions := make([]Transition, 0, len(registeredUseCases))
	for _, useCase := range registeredUseCases {
		row, ok := byUseCase[useCase]
		if !ok {
			transitions = append(transitions, noTransition(organizationID, useCase))
			continue
		}

		transition, err := toTransition(row)
		if err != nil {
			return nil, err
		}
		transitions = append(transitions, transition)
	}

	return transitions, nil
}

// Forget drops the cached state for an organization's use case.
func (t *Transitions) Forget(organizationID string, useCase UseCase) {
	t.cache.Remove(transitionKey{organizationID: organizationID, useCase: useCase})
}

// Enroll moves a use case from platform_key to dual_write. It provisions the
// organization's key and the use case's data key first and changes nothing
// if that fails. Each enroll can mint a KMS key, so bulk callers rate-limit.
func (t *Transitions) Enroll(ctx context.Context, organizationID string, useCase UseCase, actor Actor) (Transition, error) {
	if err := t.requireWritable(useCase); err != nil {
		return Transition{}, err
	}

	if err := t.provisioner.EnsureUseCase(ctx, organizationID, useCase, actor); err != nil {
		return Transition{}, fmt.Errorf("provision organization encryption: %w", err)
	}

	return t.change(ctx, organizationID, useCase, actor, t.audit.LogOrganizationEncryptionTransitionEnroll,
		func(ctx context.Context, q *repo.Queries, before *repo.OrganizationEncryptionTransition) (repo.OrganizationEncryptionTransition, *audit.OrganizationEncryptionTransitionMetadata, error) {
			switch {
			case before == nil:
				row, err := q.CreateEncryptionTransition(ctx, repo.CreateEncryptionTransitionParams{
					OrganizationID:          organizationID,
					UseCase:                 string(useCase),
					State:                   string(TransitionStateDualWrite),
					PlatformCopiesClearedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
				})
				if err != nil {
					return repo.OrganizationEncryptionTransition{}, nil, fmt.Errorf("create encryption transition: %w", err)
				}
				return row, nil, nil
			case TransitionState(before.State) == TransitionStatePlatformKey:
				row, err := updateTransitionState(ctx, q, *before, TransitionStateDualWrite)
				return row, nil, err
			default:
				return repo.OrganizationEncryptionTransition{}, nil, fmt.Errorf("%w: enroll from %s", ErrTransitionNotAllowed, before.State)
			}
		})
}

// Rollback moves a use case from dual_write back to platform_key. Both copies
// still exist in dual_write, so nothing is lost.
func (t *Transitions) Rollback(ctx context.Context, organizationID string, useCase UseCase, actor Actor) (Transition, error) {
	if err := t.requireWritable(useCase); err != nil {
		return Transition{}, err
	}

	return t.change(ctx, organizationID, useCase, actor, t.audit.LogOrganizationEncryptionTransitionRollback,
		func(ctx context.Context, q *repo.Queries, before *repo.OrganizationEncryptionTransition) (repo.OrganizationEncryptionTransition, *audit.OrganizationEncryptionTransitionMetadata, error) {
			if before == nil || TransitionState(before.State) != TransitionStateDualWrite {
				return repo.OrganizationEncryptionTransition{}, nil, fmt.Errorf("%w: roll back from %s", ErrTransitionNotAllowed, stateOf(before))
			}

			row, err := updateTransitionState(ctx, q, *before, TransitionStatePlatformKey)
			return row, nil, err
		})
}

// StartInOrgKMS records a new organization's use case directly in org_kms, with
// its platform-key copies marked cleared because there never were any. It
// provisions first and writes nothing if that fails, leaving the organization
// in platform_key.
func (t *Transitions) StartInOrgKMS(ctx context.Context, organizationID string, useCase UseCase, actor Actor) (Transition, error) {
	if err := t.requireWritable(useCase); err != nil {
		return Transition{}, err
	}

	if err := t.provisioner.EnsureUseCase(ctx, organizationID, useCase, actor); err != nil {
		return Transition{}, fmt.Errorf("provision organization encryption: %w", err)
	}

	return t.change(ctx, organizationID, useCase, actor, t.audit.LogOrganizationEncryptionTransitionStartOrgKMS,
		func(ctx context.Context, q *repo.Queries, before *repo.OrganizationEncryptionTransition) (repo.OrganizationEncryptionTransition, *audit.OrganizationEncryptionTransitionMetadata, error) {
			if before != nil {
				return repo.OrganizationEncryptionTransition{}, nil, fmt.Errorf("%w: start in org_kms from %s", ErrTransitionNotAllowed, before.State)
			}

			now, err := q.GetEncryptionClock(ctx)
			if err != nil {
				return repo.OrganizationEncryptionTransition{}, nil, fmt.Errorf("read database clock: %w", err)
			}

			row, err := q.CreateEncryptionTransition(ctx, repo.CreateEncryptionTransitionParams{
				OrganizationID:          organizationID,
				UseCase:                 string(useCase),
				State:                   string(TransitionStateOrgKMS),
				PlatformCopiesClearedAt: now,
			})
			if err != nil {
				return repo.OrganizationEncryptionTransition{}, nil, fmt.Errorf("create encryption transition: %w", err)
			}

			return row, nil, nil
		})
}

// FinalizeRequest is what finalize checks before moving to org_kms.
type FinalizeRequest struct {
	// MinimumDualWrite is how long the use case must have been in
	// dual_write. Values below MinimumDualWriteFloor are raised to it.
	MinimumDualWrite time.Duration

	// Stores are the copy stores of every column pair that holds values for
	// the use case.
	Stores []CopyStore
}

// Finalize moves a use case from dual_write to org_kms. Under the state row
// lock it checks that no value waits for an organization-key copy, that no
// value is stored without an organization it can be attributed to, that a
// clean verification pass ran after the last backfill in this dual_write
// period, and that the use case has spent the minimum time in dual_write.
// Values recorded as unreadable do not block it; ClearPlatformCopies destroys
// them with the rest. Finalize clears nothing itself: call ClearPlatformCopies
// next, and again after a crash until it succeeds.
func (t *Transitions) Finalize(ctx context.Context, organizationID string, useCase UseCase, req FinalizeRequest, actor Actor) (Transition, error) {
	if err := t.requireWritable(useCase); err != nil {
		return Transition{}, err
	}
	if err := requireStores(useCase, req.Stores); err != nil {
		return Transition{}, err
	}
	minimum := max(req.MinimumDualWrite, MinimumDualWriteFloor)

	return t.change(ctx, organizationID, useCase, actor, t.audit.LogOrganizationEncryptionTransitionFinalize,
		func(ctx context.Context, q *repo.Queries, before *repo.OrganizationEncryptionTransition) (repo.OrganizationEncryptionTransition, *audit.OrganizationEncryptionTransitionMetadata, error) {
			if before == nil || TransitionState(before.State) != TransitionStateDualWrite {
				return repo.OrganizationEncryptionTransition{}, nil, fmt.Errorf("%w: finalize from %s", ErrTransitionNotAllowed, stateOf(before))
			}

			now, err := q.GetEncryptionClock(ctx)
			if err != nil {
				return repo.OrganizationEncryptionTransition{}, nil, fmt.Errorf("read database clock: %w", err)
			}
			if err := checkFinalizeTiming(*before, now.Time, minimum); err != nil {
				return repo.OrganizationEncryptionTransition{}, nil, err
			}

			counts, err := countStores(ctx, organizationID, req.Stores)
			if err != nil {
				return repo.OrganizationEncryptionTransition{}, nil, err
			}
			switch {
			case counts.Waiting > 0:
				return repo.OrganizationEncryptionTransition{}, nil, fmt.Errorf("%w: %d values still wait for an organization-key copy", ErrFinalizeBlocked, counts.Waiting)
			case counts.Unattributed > 0:
				return repo.OrganizationEncryptionTransition{}, nil, fmt.Errorf("%w: %d values are stored without an organization", ErrFinalizeBlocked, counts.Unattributed)
			}

			row, err := updateTransitionState(ctx, q, *before, TransitionStateOrgKMS)
			if err != nil {
				return repo.OrganizationEncryptionTransition{}, nil, err
			}

			return row, &audit.OrganizationEncryptionTransitionMetadata{
				MinimumDualWriteSeconds: int64(minimum / time.Second),
				UnreadableValues:        counts.Unreadable,
				ClearedValues:           0,
			}, nil
		})
}

// checkFinalizeTiming checks the minimum time in dual_write and that a clean
// verification pass started after both the move to dual_write and the last
// backfill, all on the database clock.
func checkFinalizeTiming(row repo.OrganizationEncryptionTransition, now time.Time, minimum time.Duration) error {
	if elapsed := now.Sub(row.StateChangedAt.Time); elapsed < minimum {
		return fmt.Errorf("%w: in dual_write for %s, needs %s", ErrFinalizeBlocked, elapsed.Truncate(time.Second), minimum)
	}

	if !row.LastVerifiedAt.Valid || row.LastVerifiedAt.Time.Before(row.StateChangedAt.Time) {
		return fmt.Errorf("%w: no clean verification pass since entering dual_write", ErrFinalizeBlocked)
	}

	if row.LastBackfillStartedAt.Valid && !row.LastBackfillCompletedAt.Valid {
		return fmt.Errorf("%w: a backfill is running or did not complete", ErrFinalizeBlocked)
	}

	if row.LastBackfillCompletedAt.Valid && row.LastVerifiedAt.Time.Before(row.LastBackfillCompletedAt.Time) {
		return fmt.Errorf("%w: no clean verification pass since the last backfill", ErrFinalizeBlocked)
	}

	return nil
}

// ClearPlatformCopies destroys the platform-key copies of a finalized use case
// in batches, then records that clearing finished. It is safe to repeat: a
// call after completion returns at once, and an interrupted call resumes.
// After each pass it waits ClearSettleDelay and counts again, because a write
// that started before finalize committed can land after its row was cleared.
func (t *Transitions) ClearPlatformCopies(ctx context.Context, organizationID string, useCase UseCase, stores []CopyStore, actor Actor) (Transition, error) {
	if err := t.requireWritable(useCase); err != nil {
		return Transition{}, err
	}
	if err := requireStores(useCase, stores); err != nil {
		return Transition{}, err
	}

	current, err := t.GetFresh(ctx, organizationID, useCase)
	if err != nil {
		return Transition{}, err
	}
	switch {
	case current.State != TransitionStateOrgKMS:
		return Transition{}, fmt.Errorf("%w: clear platform-key copies in %s", ErrTransitionNotAllowed, current.State)
	case current.PlatformCopiesClearedAt != nil:
		return current, nil
	}

	logger := t.logger.With(attr.SlogOrganizationID(organizationID), attr.SlogOrgEncryptionUseCase(useCase))

	var cleared int64
	for pass := range maxClearPasses {
		n, err := clearStores(ctx, organizationID, current.Generation, stores)
		cleared += n
		if err != nil {
			return Transition{}, err
		}

		if err := sleep(ctx, t.settleDelay); err != nil {
			return Transition{}, err
		}

		counts, err := countStores(ctx, organizationID, stores)
		if err != nil {
			return Transition{}, err
		}
		if counts.PlatformCopies == 0 {
			break
		}

		logger.WarnContext(ctx, "platform-key copies reappeared after clearing", attr.SlogRetryAttempt(pass+1))
		if pass == maxClearPasses-1 {
			return Transition{}, fmt.Errorf("orgencryption: %d platform-key copies remain after %d clear passes", counts.PlatformCopies, maxClearPasses)
		}
	}

	return t.change(ctx, organizationID, useCase, actor, t.audit.LogOrganizationEncryptionTransitionClearPlatformCopies,
		func(ctx context.Context, q *repo.Queries, before *repo.OrganizationEncryptionTransition) (repo.OrganizationEncryptionTransition, *audit.OrganizationEncryptionTransitionMetadata, error) {
			if before == nil || TransitionState(before.State) != TransitionStateOrgKMS || before.Generation != current.Generation {
				return repo.OrganizationEncryptionTransition{}, nil, ErrConcurrentChange
			}

			row, err := q.MarkEncryptionTransitionPlatformCopiesCleared(ctx, repo.MarkEncryptionTransitionPlatformCopiesClearedParams{
				OrganizationID: organizationID,
				UseCase:        string(useCase),
				Generation:     before.Generation,
			})
			if err != nil {
				return repo.OrganizationEncryptionTransition{}, nil, fmt.Errorf("mark platform-key copies cleared: %w", err)
			}

			return row, &audit.OrganizationEncryptionTransitionMetadata{MinimumDualWriteSeconds: 0, UnreadableValues: 0, ClearedValues: cleared}, nil
		})
}

func clearStores(ctx context.Context, organizationID string, generation int64, stores []CopyStore) (int64, error) {
	var cleared int64
	for _, store := range stores {
		for {
			n, err := store.ClearPlatformCopies(ctx, organizationID, generation, clearBatchSize)
			cleared += n
			if err != nil {
				return cleared, fmt.Errorf("clear platform-key copies in %s: %w", store.Columns().PlatformColumn, err)
			}
			if n < clearBatchSize {
				break
			}
		}
	}

	return cleared, nil
}

func countStores(ctx context.Context, organizationID string, stores []CopyStore) (CopyCounts, error) {
	total := CopyCounts{Waiting: 0, Unreadable: 0, Unattributed: 0, PlatformCopies: 0}
	for _, store := range stores {
		counts, err := store.Count(ctx, organizationID)
		if err != nil {
			return CopyCounts{}, fmt.Errorf("count values in %s: %w", store.Columns().PlatformColumn, err)
		}
		total.Waiting += counts.Waiting
		total.Unreadable += counts.Unreadable
		total.Unattributed += counts.Unattributed
		total.PlatformCopies += counts.PlatformCopies
	}

	return total, nil
}

// sleep waits for d or until ctx ends.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return fmt.Errorf("wait to count platform-key copies: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

type transitionAuditFunc func(ctx context.Context, dbtx auditrepo.DBTX, event audit.LogOrganizationEncryptionTransitionEvent) error

type transitionApplyFunc func(ctx context.Context, q *repo.Queries, before *repo.OrganizationEncryptionTransition) (repo.OrganizationEncryptionTransition, *audit.OrganizationEncryptionTransitionMetadata, error)

// change applies a transition under the organization lock and the state row
// lock, audits it in the same transaction, and drops this process's cache.
func (t *Transitions) change(ctx context.Context, organizationID string, useCase UseCase, actor Actor, log transitionAuditFunc, apply transitionApplyFunc) (Transition, error) {
	tx, err := t.db.Begin(ctx)
	if err != nil {
		return Transition{}, fmt.Errorf("begin encryption transition: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := repo.New(tx)
	if err := lockOrganization(ctx, q, organizationID); err != nil {
		return Transition{}, err
	}

	var before *repo.OrganizationEncryptionTransition
	locked, err := q.LockEncryptionTransition(ctx, repo.LockEncryptionTransitionParams{OrganizationID: organizationID, UseCase: string(useCase)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return Transition{}, fmt.Errorf("lock encryption transition: %w", err)
	default:
		before = &locked
	}

	after, metadata, err := apply(ctx, q, before)
	if err != nil {
		return Transition{}, err
	}

	if err := log(ctx, tx, transitionEvent(actor, organizationID, useCase, before, after, metadata)); err != nil {
		return Transition{}, fmt.Errorf("audit encryption transition: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Transition{}, &commitError{err: err}
	}
	t.Forget(organizationID, useCase)

	return toTransition(after)
}

func (t *Transitions) requireWritable(useCase UseCase) error {
	if err := useCase.validate(); err != nil {
		return err
	}
	if t.audit == nil || t.provisioner == nil {
		return errors.New("orgencryption: transition changes need a provisioner and an audit logger in this process")
	}

	return nil
}

func requireStores(useCase UseCase, stores []CopyStore) error {
	if len(stores) == 0 {
		return errors.New("orgencryption: at least one copy store is required")
	}
	for _, store := range stores {
		if columns := store.Columns(); columns.UseCase != useCase {
			return fmt.Errorf("orgencryption: copy store for %s belongs to use case %q, not %q", columns.OrgColumn, columns.UseCase, useCase)
		}
	}

	return nil
}

// updateTransitionState moves a locked row to state, increasing generation.
func updateTransitionState(ctx context.Context, q *repo.Queries, row repo.OrganizationEncryptionTransition, state TransitionState) (repo.OrganizationEncryptionTransition, error) {
	updated, err := q.UpdateEncryptionTransitionState(ctx, repo.UpdateEncryptionTransitionStateParams{
		State:          string(state),
		OrganizationID: row.OrganizationID,
		UseCase:        row.UseCase,
		Generation:     row.Generation,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return repo.OrganizationEncryptionTransition{}, ErrConcurrentChange
	case err != nil:
		return repo.OrganizationEncryptionTransition{}, fmt.Errorf("update encryption transition state: %w", err)
	}

	return updated, nil
}

func stateOf(row *repo.OrganizationEncryptionTransition) TransitionState {
	if row == nil {
		return TransitionStatePlatformKey
	}

	return TransitionState(row.State)
}

func noTransition(organizationID string, useCase UseCase) Transition {
	return Transition{
		ID:                      uuid.Nil,
		OrganizationID:          organizationID,
		UseCase:                 useCase,
		State:                   TransitionStatePlatformKey,
		Generation:              0,
		StateChangedAt:          time.Time{},
		PlatformCopiesClearedAt: nil,
		LastBackfill:            BackfillRecord{StartedAt: nil, CompletedAt: nil, Converted: nil, Remaining: nil, Unreadable: nil, Error: nil},
		LastVerifiedAt:          nil,
	}
}

func toTransition(row repo.OrganizationEncryptionTransition) (Transition, error) {
	state, err := parseTransitionState(row.State)
	if err != nil {
		return Transition{}, err
	}

	return Transition{
		ID:                      row.ID,
		OrganizationID:          row.OrganizationID,
		UseCase:                 UseCase(row.UseCase),
		State:                   state,
		Generation:              row.Generation,
		StateChangedAt:          row.StateChangedAt.Time,
		PlatformCopiesClearedAt: optionalTime(row.PlatformCopiesClearedAt),
		LastBackfill: BackfillRecord{
			StartedAt:   optionalTime(row.LastBackfillStartedAt),
			CompletedAt: optionalTime(row.LastBackfillCompletedAt),
			Converted:   optionalInt(row.LastBackfillConvertedCount),
			Remaining:   optionalInt(row.LastBackfillRemainingCount),
			Unreadable:  optionalInt(row.LastBackfillUnreadableCount),
			Error:       optionalText(row.LastBackfillError),
		},
		LastVerifiedAt: optionalTime(row.LastVerifiedAt),
	}, nil
}

func optionalTime(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}

	return new(ts.Time)
}

func optionalInt(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}

	return new(v.Int64)
}

func optionalText(v pgtype.Text) *string {
	if !v.Valid {
		return nil
	}

	return new(v.String)
}

func transitionEvent(actor Actor, organizationID string, useCase UseCase, before *repo.OrganizationEncryptionTransition, after repo.OrganizationEncryptionTransition, metadata *audit.OrganizationEncryptionTransitionMetadata) audit.LogOrganizationEncryptionTransitionEvent {
	beforeSnapshot := &audit.OrganizationEncryptionTransitionSnapshot{
		UseCase:               string(useCase),
		State:                 string(TransitionStatePlatformKey),
		Generation:            0,
		PlatformCopiesCleared: false,
	}
	if before != nil {
		beforeSnapshot = transitionSnapshot(*before)
	}

	return audit.LogOrganizationEncryptionTransitionEvent{
		OrganizationID:           organizationID,
		Actor:                    actor.Principal,
		ActorDisplayName:         actor.DisplayName,
		ActorSlug:                nil,
		TransitionURN:            urn.NewOrganizationEncryptionTransition(after.ID),
		TransitionSnapshotBefore: beforeSnapshot,
		TransitionSnapshotAfter:  transitionSnapshot(after),
		Metadata:                 metadata,
	}
}

func transitionSnapshot(row repo.OrganizationEncryptionTransition) *audit.OrganizationEncryptionTransitionSnapshot {
	return &audit.OrganizationEncryptionTransitionSnapshot{
		UseCase:               row.UseCase,
		State:                 row.State,
		Generation:            row.Generation,
		PlatformCopiesCleared: row.PlatformCopiesClearedAt.Valid,
	}
}
