package orgencryption

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
)

const (
	// migratorPageSize bounds the finalized transitions ListFinalized
	// returns. A list that fills up leaves the rest for the next run.
	migratorPageSize = 500

	// backfillCandidatePageSize is how many candidates one
	// NextBackfillCandidates page holds. Callers check and backfill each
	// candidate before reading the next page, so a small page keeps the work
	// between list queries short.
	backfillCandidatePageSize = 100

	// VerificationRetryInterval is how long an organization with nothing
	// waiting, but no clean verification since its last backfill, waits
	// before BackfillDue reports it due again. A failed verification needs a
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
	logger            *slog.Logger
	db                *pgxpool.Pool
	runner            *Runner
	transitions       *Transitions
	stores            map[UseCase][]CopyStore
	useCases          []string
	stragglers        metric.Int64Counter
	candidatePageSize int32
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
		logger:            logger,
		db:                db,
		runner:            runner,
		transitions:       transitions,
		stores:            stores,
		useCases:          useCases,
		stragglers:        newStragglerCounter(logger, meterProvider),
		candidatePageSize: backfillCandidatePageSize,
	}, nil
}

// BackfillCursor is a position in one pass over the backfill candidates.
// The zero value starts a new pass.
type BackfillCursor struct {
	started             bool
	cutoff              time.Time
	afterStarted        pgtype.Timestamptz
	afterOrganizationID string
	afterUseCase        string
}

// BackfillPage is one page of backfill candidates.
type BackfillPage struct {
	// Candidates are dual_write transitions that may be due a backfill,
	// least recently attempted first. BackfillDue decides each one.
	Candidates []Transition

	// Next continues the pass after this page.
	Next BackfillCursor

	// More reports whether another page may follow.
	More bool
}

// NextBackfillCandidates returns the page of backfill candidates after
// cursor. A pass covers every dual_write transition of the migrator's use
// cases, least recently attempted first, except those whose backfill started
// after the pass began, so an organization backfilled during the pass is not
// offered again. Check and backfill each candidate before reading the next
// page, so work starts without scanning every organization first.
func (m *Migrator) NextBackfillCandidates(ctx context.Context, cursor BackfillCursor) (BackfillPage, error) {
	q := repo.New(m.db)
	page := BackfillPage{Candidates: nil, Next: cursor, More: false}

	if !cursor.started {
		now, err := q.GetEncryptionClock(ctx)
		if err != nil {
			return page, fmt.Errorf("read database clock: %w", err)
		}
		cursor = BackfillCursor{
			started:             true,
			cutoff:              now.Time,
			afterStarted:        pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.NegativeInfinity, Valid: true},
			afterOrganizationID: "",
			afterUseCase:        "",
		}
	}

	rows, err := q.ListEncryptionBackfillCandidates(ctx, repo.ListEncryptionBackfillCandidatesParams{
		UseCases:            m.useCases,
		Cutoff:              pgtype.Timestamptz{Time: cursor.cutoff, InfinityModifier: pgtype.Finite, Valid: true},
		AfterStarted:        cursor.afterStarted,
		AfterOrganizationID: cursor.afterOrganizationID,
		AfterUseCase:        cursor.afterUseCase,
		LimitValue:          m.candidatePageSize,
	})
	if err != nil {
		return page, fmt.Errorf("list encryption backfill candidates: %w", err)
	}

	page.Candidates = make([]Transition, 0, len(rows))
	for _, row := range rows {
		transition, err := toTransition(row)
		if err != nil {
			return page, err
		}
		page.Candidates = append(page.Candidates, transition)
	}

	page.Next = cursor
	if len(rows) == int(m.candidatePageSize) {
		last := rows[len(rows)-1]
		page.Next.afterStarted = last.LastBackfillStartedAt
		if !page.Next.afterStarted.Valid {
			page.Next.afterStarted = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.NegativeInfinity, Valid: true}
		}
		page.Next.afterOrganizationID = last.OrganizationID
		page.Next.afterUseCase = last.UseCase
		page.More = true
	}

	return page, nil
}

// BackfillDue reports whether a candidate is due a backfill now: values wait
// for an organization-key copy; or it has no clean verification since it
// entered dual_write or since its last backfill started, and that attempt
// started at least VerificationRetryInterval ago; or its last run started but
// never recorded its end, and that run's claim expired.
func (m *Migrator) BackfillDue(ctx context.Context, transition Transition) (bool, error) {
	stores, err := m.storesFor(transition.UseCase)
	if err != nil {
		return false, err
	}

	now, err := repo.New(m.db).GetEncryptionClock(ctx)
	if err != nil {
		return false, fmt.Errorf("read database clock: %w", err)
	}

	started, completed, verified := transition.LastBackfill.StartedAt, transition.LastBackfill.CompletedAt, transition.LastVerifiedAt
	if started != nil && completed == nil {
		// A run holds the claim, or its process died before it recorded its
		// end. Once the claim expires nothing would pick it up otherwise,
		// and finalize refuses until a run completes.
		return now.Time.Sub(*started) >= RunLease, nil
	}

	cleanlyVerified := verified != nil && !verified.Before(transition.StateChangedAt) && (started == nil || !verified.Before(*started))
	if !cleanlyVerified && (started == nil || now.Time.Sub(*started) >= VerificationRetryInterval) {
		return true, nil
	}

	counts, err := CountStores(ctx, transition.OrganizationID, stores)
	if err != nil {
		return false, err
	}

	return counts.Waiting > 0, nil
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

// RunOnDemand does the work an operator asks for on one organization's use
// case. In dual_write it backfills and verifies through Runner. In org_kms
// with the clear unfinished, such as right after finalize, it finishes the
// clear, attributed to actor; that waits the clear settle delay after each
// pass. Any other state returns ErrTransitionNotAllowed.
func (m *Migrator) RunOnDemand(ctx context.Context, organizationID string, useCase UseCase, actor Actor) (RunResult, error) {
	empty := RunResult{Converted: 0, Counted: false, Remaining: 0, Unreadable: 0, Unattributed: 0, Verified: 0, Mismatched: 0, Unopenable: 0, VerificationPassed: false}

	transition, err := m.transitions.GetFresh(ctx, organizationID, useCase)
	if err != nil {
		return empty, err
	}

	switch {
	case transition.State == TransitionStateOrgKMS && transition.PlatformCopiesClearedAt == nil:
		return empty, m.clear(ctx, organizationID, useCase, actor)
	case transition.State == TransitionStateOrgKMS:
		return empty, fmt.Errorf("%w: run in org_kms after clearing finished", ErrTransitionNotAllowed)
	default:
		return m.Backfill(ctx, organizationID, useCase)
	}
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
	return m.clear(ctx, organizationID, useCase, SystemActor())
}

func (m *Migrator) clear(ctx context.Context, organizationID string, useCase UseCase, actor Actor) error {
	stores, err := m.storesFor(useCase)
	if err != nil {
		return err
	}

	if _, err := m.transitions.ClearPlatformCopies(ctx, organizationID, useCase, stores, actor); err != nil {
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
		cleared += straggler.Cleared.Values()
		m.logger.ErrorContext(ctx, "platform-key copies found after clearing finished",
			attr.SlogOrganizationID(organizationID),
			attr.SlogOrgEncryptionUseCase(useCase),
			attr.SlogOrgEncryptionSourceColumn(straggler.Columns.PlatformColumn),
			attr.SlogOrgEncryptionPlatformOnlyCount(straggler.Cleared.PlatformOnly),
			attr.SlogOrgEncryptionBesideOrgCopyCount(straggler.Cleared.BesideOrgCopy),
		)
		m.recordStragglers(ctx, useCase, straggler.Columns.PlatformColumn, StragglerKindPlatformOnly, straggler.Cleared.PlatformOnly)
		m.recordStragglers(ctx, useCase, straggler.Columns.PlatformColumn, StragglerKindBesideOrgCopy, straggler.Cleared.BesideOrgCopy)
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
