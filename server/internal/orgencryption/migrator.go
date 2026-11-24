package orgencryption

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
)

const (
	// migratorPageSize bounds the transitions one list query returns. There
	// are tens of organizations per use case to move. ListDueBackfills pages
	// through every dual_write transition; ListFinalized returns one page and
	// leaves the rest for the next run.
	migratorPageSize = 500

	// VerificationRetryInterval is how long an organization with nothing
	// waiting, but no clean verification since its last backfill, waits
	// before ListDueBackfills offers it again. A failed verification needs a
	// person to look, and re-verifying every hour would repeat the same
	// failure and its error logs.
	VerificationRetryInterval = 6 * time.Hour

	// stalledClearGrace is how long after finalize an unfinished clear waits
	// before ListFinalized offers it for resuming, so the sweep does not race
	// the clear that the finalizing operator runs right after finalize.
	stalledClearGrace = time.Hour

	// stragglerWindow is how long after a use case reaches org_kms the sweep
	// keeps looking for platform-key copies. A write decided before the move
	// can only land inside a transaction held open since then, and the
	// server's one-minute statement and idle-in-transaction timeouts end
	// such a transaction unless it keeps issuing statements. A week leaves
	// wide margin while keeping the list to recently moved organizations.
	stragglerWindow = 7 * 24 * time.Hour
)

// StragglerKind says whether a platform-key copy found after clearing stood
// alone or beside an organization-key copy.
type StragglerKind string

const (
	// StragglerKindPlatformOnly is a platform-key copy with no
	// organization-key copy. Clearing it destroys the value.
	StragglerKindPlatformOnly StragglerKind = "platform_only"

	// StragglerKindBesideOrgCopy is a platform-key copy beside an
	// organization-key copy. Clearing it loses nothing.
	StragglerKindBesideOrgCopy StragglerKind = "beside_org_copy"
)

// UseCaseStores names a use case a Migrator moves and the copy stores of
// every value it holds.
type UseCaseStores struct {
	// UseCase is the registered use case.
	UseCase UseCase

	// Stores reach every value the use case holds. A value left out keeps
	// only its platform-key copy, which nothing reads once the organization
	// is finalized.
	Stores []CopyStore
}

// Migrator runs the background side of moving use cases from the platform key
// to organization keys, for the use cases it has copy stores for: it picks
// the dual_write organizations due a backfill and runs them through Runner,
// resumes clears that never finished, and sweeps finalized organizations for
// platform-key copies that appeared after clearing.
type Migrator struct {
	logger      *slog.Logger
	db          *pgxpool.Pool
	runner      *Runner
	transitions *Transitions
	stores      map[UseCase][]CopyStore
	useCases    []string
	stragglers  metric.Int64Counter
}

// NewMigrator builds a Migrator over the use cases it moves, each listed
// once with the copy stores of every value it holds.
func NewMigrator(logger *slog.Logger, db *pgxpool.Pool, runner *Runner, transitions *Transitions, moves []UseCaseStores, meterProvider metric.MeterProvider) (*Migrator, error) {
	if len(moves) == 0 {
		return nil, errors.New("orgencryption: a migrator needs the copy stores of at least one use case")
	}

	stores := make(map[UseCase][]CopyStore, len(moves))
	useCases := make([]string, 0, len(moves))
	for _, move := range moves {
		if err := move.UseCase.validate(); err != nil {
			return nil, err
		}
		if err := requireStores(move.UseCase, move.Stores); err != nil {
			return nil, err
		}
		if _, ok := stores[move.UseCase]; ok {
			return nil, fmt.Errorf("orgencryption: use case %q is listed twice", move.UseCase)
		}
		stores[move.UseCase] = move.Stores
		useCases = append(useCases, string(move.UseCase))
	}
	slices.Sort(useCases)

	logger = logger.With(attr.SlogComponent("orgencryption.migrator"))

	return &Migrator{
		logger:      logger,
		db:          db,
		runner:      runner,
		transitions: transitions,
		stores:      stores,
		useCases:    useCases,
		stragglers:  newStragglerCounter(logger, meterProvider),
	}, nil
}

// ListDueBackfills returns the dual_write transitions due a backfill, least
// recently attempted first. A transition is due while values wait for an
// organization-key copy; while it has no clean verification since it entered
// dual_write or since its last backfill started, once that attempt started at
// least VerificationRetryInterval ago; or when its last run started but never
// recorded its end, once that run's claim expired. A transition whose counts
// fail is logged and left out, so one organization never holds up the rest.
func (m *Migrator) ListDueBackfills(ctx context.Context) ([]Transition, error) {
	q := repo.New(m.db)

	now, err := q.GetEncryptionClock(ctx)
	if err != nil {
		return nil, fmt.Errorf("read database clock: %w", err)
	}

	var due []Transition
	afterOrganizationID, afterUseCase := "", ""
	for {
		rows, err := q.ListEncryptionTransitionsInDualWrite(ctx, repo.ListEncryptionTransitionsInDualWriteParams{
			UseCases:            m.useCases,
			AfterOrganizationID: afterOrganizationID,
			AfterUseCase:        afterUseCase,
			LimitValue:          migratorPageSize,
		})
		if err != nil {
			return nil, fmt.Errorf("list dual_write encryption transitions: %w", err)
		}

		for _, row := range rows {
			if m.backfillDue(ctx, row, now.Time) {
				transition, err := toTransition(row)
				if err != nil {
					m.logger.ErrorContext(ctx, "read encryption transition", attr.SlogOrganizationID(row.OrganizationID), attr.SlogError(err))
					continue
				}
				due = append(due, transition)
			}
		}

		if len(rows) < migratorPageSize {
			break
		}
		afterOrganizationID, afterUseCase = rows[len(rows)-1].OrganizationID, rows[len(rows)-1].UseCase
	}

	slices.SortStableFunc(due, func(a, b Transition) int {
		switch {
		case a.LastBackfill.StartedAt == nil && b.LastBackfill.StartedAt == nil:
			return 0
		case a.LastBackfill.StartedAt == nil:
			return -1
		case b.LastBackfill.StartedAt == nil:
			return 1
		default:
			return a.LastBackfill.StartedAt.Compare(*b.LastBackfill.StartedAt)
		}
	})

	return due, nil
}

// backfillDue reports whether a dual_write transition is due a backfill at
// now, the database clock.
func (m *Migrator) backfillDue(ctx context.Context, row repo.OrganizationEncryptionTransition, now time.Time) bool {
	started, completed, verified := row.LastBackfillStartedAt, row.LastBackfillCompletedAt, row.LastVerifiedAt

	if started.Valid && !completed.Valid {
		// A run holds the claim, or its process died before it recorded its
		// end. Once the claim expires nothing would pick it up otherwise,
		// and finalize refuses until a run completes.
		return now.Sub(started.Time) >= RunLease
	}

	cleanlyVerified := verified.Valid && !verified.Time.Before(row.StateChangedAt.Time) && (!started.Valid || !verified.Time.Before(started.Time))
	if !cleanlyVerified && (!started.Valid || now.Sub(started.Time) >= VerificationRetryInterval) {
		return true
	}

	counts, err := countStores(ctx, row.OrganizationID, m.stores[UseCase(row.UseCase)])
	if err != nil {
		m.logger.ErrorContext(ctx, "count values waiting for an organization-key copy",
			attr.SlogOrganizationID(row.OrganizationID), attr.SlogOrgEncryptionUseCase(row.UseCase), attr.SlogError(err))
		return false
	}

	return counts.Waiting > 0
}

// Backfill runs one organization's use case through Runner. It returns
// ErrBackfillRunning while another run holds the claim.
func (m *Migrator) Backfill(ctx context.Context, organizationID string, useCase UseCase) (RunResult, error) {
	stores, err := m.storesFor(useCase)
	if err != nil {
		return RunResult{Converted: 0, Counted: false, Remaining: 0, Unreadable: 0, Unattributed: 0, Verified: 0, Mismatched: 0, Unopenable: 0, VerificationPassed: false}, err
	}

	return m.runner.Run(ctx, organizationID, useCase, stores)
}

// ListFinalized returns the org_kms transitions the sweep should visit:
// clears that never finished, finalized more than stalledClearGrace ago,
// first; then use cases that reached org_kms within stragglerWindow.
func (m *Migrator) ListFinalized(ctx context.Context) ([]Transition, error) {
	rows, err := repo.New(m.db).ListEncryptionTransitionsInOrgKMSForSweep(ctx, repo.ListEncryptionTransitionsInOrgKMSForSweepParams{
		UseCases:          m.useCases,
		StalledClearGrace: conv.PtrToPGInterval(new(stalledClearGrace)),
		StragglerWindow:   conv.PtrToPGInterval(new(stragglerWindow)),
		LimitValue:        migratorPageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("list org_kms encryption transitions: %w", err)
	}

	transitions := make([]Transition, 0, len(rows))
	for _, row := range rows {
		transition, err := toTransition(row)
		if err != nil {
			return nil, err
		}
		transitions = append(transitions, transition)
	}

	return transitions, nil
}

// ResumeClear finishes destroying the platform-key copies of a finalized use
// case whose clear never completed, as the system actor. It waits the clear
// settle delay after each pass, so a call takes at least that long.
func (m *Migrator) ResumeClear(ctx context.Context, organizationID string, useCase UseCase) error {
	stores, err := m.storesFor(useCase)
	if err != nil {
		return err
	}

	if _, err := m.transitions.ClearPlatformCopies(ctx, organizationID, useCase, stores, SystemActor()); err != nil {
		return fmt.Errorf("resume clearing platform-key copies: %w", err)
	}

	return nil
}

// SweepStragglers clears platform-key copies that appeared after a finalized
// use case's clear finished. Each column that held any is logged as an error
// and counted in gram.orgencryption.transition.stragglers by kind. It returns
// how many copies it cleared.
func (m *Migrator) SweepStragglers(ctx context.Context, organizationID string, useCase UseCase) (int64, error) {
	stores, err := m.storesFor(useCase)
	if err != nil {
		return 0, err
	}

	stragglers, sweepErr := m.transitions.SweepPlatformCopies(ctx, organizationID, useCase, stores)

	var cleared int64
	for _, straggler := range stragglers {
		cleared += straggler.Cleared
		m.logger.ErrorContext(ctx, "platform-key copies found after clearing finished",
			attr.SlogOrganizationID(organizationID),
			attr.SlogOrgEncryptionUseCase(useCase),
			attr.SlogOrgEncryptionSourceColumn(straggler.Columns.PlatformColumn),
			attr.SlogOrgEncryptionPlatformOnlyCount(straggler.PlatformOnly),
			attr.SlogOrgEncryptionBesideOrgCopyCount(straggler.BesideOrgCopy),
			attr.SlogOrgEncryptionClearedCount(straggler.Cleared),
		)
		m.recordStragglers(ctx, useCase, straggler.Columns.PlatformColumn, StragglerKindPlatformOnly, straggler.PlatformOnly)
		m.recordStragglers(ctx, useCase, straggler.Columns.PlatformColumn, StragglerKindBesideOrgCopy, straggler.BesideOrgCopy)
	}

	if sweepErr != nil {
		return cleared, fmt.Errorf("sweep platform-key copies: %w", sweepErr)
	}

	return cleared, nil
}

func (m *Migrator) recordStragglers(ctx context.Context, useCase UseCase, column string, kind StragglerKind, n int64) {
	if m.stragglers == nil || n == 0 {
		return
	}

	m.stragglers.Add(ctx, n, metric.WithAttributes(
		attr.OrgEncryptionUseCase(useCase),
		attr.OrgEncryptionSourceColumn(column),
		attr.OrgEncryptionStragglerKind(kind),
	))
}

func (m *Migrator) storesFor(useCase UseCase) ([]CopyStore, error) {
	stores, ok := m.stores[useCase]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNoCopyStores, useCase)
	}

	return stores, nil
}
