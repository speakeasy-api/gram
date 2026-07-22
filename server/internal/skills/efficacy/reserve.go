package efficacy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

// FeatureChecker is the product features client narrowed to the single question
// the reservation asks of it.
type FeatureChecker interface {
	IsFeatureEnabled(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

// Reserve moves pending evaluations to reserved, spending the organization's
// budget for the current UTC day.
//
// A project that no longer resolves reserves nothing and reports no error: a
// coordinator can be holding an id that was deleted between passes.
//
// An organization without the skills product feature reserves nothing: the
// entitlement is checked before any budget is read, so a batch is only ever
// spent by an organization that is entitled to the pipeline at all.
//
// The whole pass is one transaction whose first statement is an advisory lock
// keyed on the project's organization. The lock is held to commit, so counting
// and reserving are serialised per organization even across its projects: a
// concurrent reserver waits and then reads the committed spend of the batch
// before it. That, not the row locks, is what makes double spending impossible.
//
// Candidates are walked recent-first and every counter is decremented in memory
// as they are admitted, so one batch can never grant more slots than the caps
// leave. The walk pages through the queue rather than reading a fixed head: a
// hot skill whose cap is spent would otherwise fill that head every pass and
// starve every other skill behind it, so the pass keeps paging until the batch
// is filled, the page bound is reached, or the queue is exhausted.
//
// The returned cursor is where that walk stopped, and it is the caller's to
// hand back on the next pass. The zero cursor — returned when the walk reached
// the end of the queue, and when there was nothing to walk at all — starts the
// next one at the head, which is what picks up everything written behind it.
func Reserve(ctx context.Context, db *pgxpool.Pool, features FeatureChecker, projectID uuid.UUID, cursor PendingCursor, batchSize int32) ([]Evaluation, PendingCursor, error) {
	var fromHead PendingCursor
	if batchSize <= 0 {
		return nil, fromHead, fmt.Errorf("reserve skill efficacy evaluations: batch size must be positive")
	}
	// The batch is handed straight to the caller to judge, so it is owned by the
	// same lease a claim would take. A wider batch than the lease covers would
	// let its tail fall out of the lease and be claimed a second time while this
	// caller is still working through it.
	if batchSize > MaxReservedClaimBatch {
		return nil, fromHead, fmt.Errorf("reserve skill efficacy evaluations: batch size %d exceeds the %d the claim lease covers", batchSize, MaxReservedClaimBatch)
	}

	now := time.Now().UTC()
	reservedOn := pgtype.Date{
		Time:             time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC),
		InfinityModifier: pgtype.Finite,
		Valid:            true,
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fromHead, fmt.Errorf("begin skill efficacy reservation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := repo.New(tx)
	if err := queries.LockProjectOrganizationSkillEfficacyBudget(ctx, projectID); err != nil {
		return nil, fromHead, fmt.Errorf("lock skill efficacy budget: %w", err)
	}

	settingsRow, err := queries.GetSkillEfficacySettingsForProject(ctx, projectID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// The project is gone or was never there, so it names no organization to
		// bill and holds no candidates. Nothing to reserve, and nothing wrong.
		return nil, fromHead, nil
	case err != nil:
		return nil, fromHead, fmt.Errorf("read skill efficacy settings: %w", err)
	}

	// The settings read is what names the organization, so the entitlement is
	// checked here rather than before the transaction: the advisory lock stays the
	// first statement and an unentitled organization still leaves without writing.
	entitled, err := features.IsFeatureEnabled(ctx, settingsRow.OrganizationID, productfeatures.FeatureSkills)
	if err != nil {
		return nil, fromHead, fmt.Errorf("check skills product feature: %w", err)
	}
	if !entitled {
		return nil, fromHead, nil
	}

	settings := Effective(settingsRow)
	if !settings.admitsWork() {
		return nil, fromHead, nil
	}

	orgSpend, err := queries.CountSkillEfficacyOrgSpendForProject(ctx, repo.CountSkillEfficacyOrgSpendForProjectParams{
		ProjectID:  projectID,
		ReservedOn: reservedOn,
	})
	if err != nil {
		return nil, fromHead, fmt.Errorf("count skill efficacy organization spend: %w", err)
	}
	orgRemaining := max(0, int64(settings.OrgDailyCap)-orgSpend)

	skillRemaining := make(map[uuid.UUID]int64)
	burstRemaining := make(map[uuid.UUID]int64)
	admitted := make([]repo.SkillEfficacyEvaluation, 0, batchSize)
	ids := make([]uuid.UUID, 0, batchSize)

	// The head of the queue can be entirely capped — one hot skill's backlog is
	// enough — so the walk pages past it rather than reading a single fixed head.
	// The cursor advances over every candidate examined, admitted or not, which
	// stops a capped skill starving the ones behind it without skipping the tail
	// of a page when the batch fills early.
	resumed := cursor != fromHead
	page := repo.ListPendingSkillEfficacyEvaluationsParams{
		ProjectID:        projectID,
		CursorObservedAt: pgtype.Timestamptz{Time: cursor.ObservedAt, InfinityModifier: pgtype.Finite, Valid: resumed},
		CursorID:         uuid.NullUUID{UUID: cursor.ID, Valid: resumed},
		PageSize:         PendingCandidatePage,
		Inactivity:       pgtype.Interval{Microseconds: InactivityWindow.Microseconds(), Days: 0, Months: 0, Valid: true},
	}
	// The walk starts where the last pass stopped and reports where this one
	// does. Reaching the end of the queue reports the head instead, so the next
	// pass starts over rather than sitting past the tail.
	next := cursor
	for range MaxPendingCandidatePages {
		if len(ids) >= int(batchSize) || orgRemaining <= 0 {
			break
		}

		candidates, err := queries.ListPendingSkillEfficacyEvaluations(ctx, page)
		if err != nil {
			return nil, fromHead, fmt.Errorf("list pending skill efficacy evaluations: %w", err)
		}
		if len(candidates) == 0 {
			next = fromHead
			break
		}

		// Spend is read once per grain: only the ids this page introduced are
		// asked about, so a skill already walked keeps the counter the earlier
		// pages decremented instead of being reset to its stored spend.
		skillIDs := make([]uuid.UUID, 0, len(candidates))
		versionIDs := make([]uuid.UUID, 0, len(candidates))
		for _, candidate := range candidates {
			if _, ok := skillRemaining[candidate.SkillID]; !ok {
				skillRemaining[candidate.SkillID] = int64(settings.PerSkillDailyCap)
				skillIDs = append(skillIDs, candidate.SkillID)
			}
			if _, ok := burstRemaining[candidate.SkillVersionID]; !ok {
				burstRemaining[candidate.SkillVersionID] = int64(settings.NewVersionBurst)
				versionIDs = append(versionIDs, candidate.SkillVersionID)
			}
		}

		if len(skillIDs) > 0 {
			skillSpend, err := queries.CountSkillEfficacySkillDailySpend(ctx, repo.CountSkillEfficacySkillDailySpendParams{
				ProjectID:  projectID,
				SkillIds:   skillIDs,
				ReservedOn: reservedOn,
			})
			if err != nil {
				return nil, fromHead, fmt.Errorf("count skill efficacy skill daily spend: %w", err)
			}
			for _, spend := range skillSpend {
				skillRemaining[spend.SkillID] = max(0, int64(settings.PerSkillDailyCap)-spend.Spend)
			}
		}

		if len(versionIDs) > 0 {
			versionSpend, err := queries.CountSkillEfficacyVersionLifetimeSpend(ctx, repo.CountSkillEfficacyVersionLifetimeSpendParams{
				ProjectID:       projectID,
				SkillVersionIds: versionIDs,
				BurstCap:        settings.NewVersionBurst,
			})
			if err != nil {
				return nil, fromHead, fmt.Errorf("count skill efficacy version lifetime spend: %w", err)
			}
			// Version spend is counted only as far as the burst, which leaves the
			// same remaining as any larger true spend would.
			for _, spend := range versionSpend {
				burstRemaining[spend.SkillVersionID] = max(0, int64(settings.NewVersionBurst)-spend.Spend)
			}
		}

		examined := 0
		for _, candidate := range candidates {
			if orgRemaining <= 0 || len(ids) >= int(batchSize) {
				break
			}
			examined++
			next = PendingCursor{ObservedAt: candidate.ObservedAt.Time, ID: candidate.ID}

			// New versions bypass the daily cap until their lifetime burst is spent,
			// but those evaluations still count toward today's skill spend. Once the
			// burst is gone, only the remaining daily allowance admits work.
			switch {
			case burstRemaining[candidate.SkillVersionID] > 0:
				burstRemaining[candidate.SkillVersionID]--
				skillRemaining[candidate.SkillID] = max(0, skillRemaining[candidate.SkillID]-1)
			case skillRemaining[candidate.SkillID] > 0:
				skillRemaining[candidate.SkillID]--
			default:
				continue
			}

			orgRemaining--
			candidate.State = StateReserved
			candidate.ReservedOn = reservedOn
			admitted = append(admitted, candidate)
			ids = append(ids, candidate.ID)
		}
		if examined < len(candidates) {
			break
		}

		if len(candidates) < int(PendingCandidatePage) {
			next = fromHead
			break
		}

		last := candidates[len(candidates)-1]
		page.CursorObservedAt = pgtype.Timestamptz{Time: last.ObservedAt.Time, InfinityModifier: pgtype.Finite, Valid: true}
		page.CursorID = uuid.NullUUID{UUID: last.ID, Valid: true}
	}

	if len(ids) == 0 {
		return nil, next, nil
	}

	reserved, err := queries.ReserveSkillEfficacyEvaluations(ctx, repo.ReserveSkillEfficacyEvaluationsParams{
		ReservedOn: reservedOn,
		ProjectID:  projectID,
		Ids:        ids,
	})
	if err != nil {
		return nil, fromHead, fmt.Errorf("reserve skill efficacy evaluations: %w", err)
	}
	// pending -> reserved is written only under the organization's advisory lock,
	// which this transaction holds, so every admitted candidate must still have
	// been pending. A short count means the budget accounting no longer describes
	// what was written and the batch must not be handed out.
	if reserved != int64(len(ids)) {
		return nil, fromHead, fmt.Errorf("reserve skill efficacy evaluations: reserved %d of %d locked candidates", reserved, len(ids))
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fromHead, fmt.Errorf("commit skill efficacy reservation: %w", err)
	}

	evaluations := make([]Evaluation, 0, len(admitted))
	for _, row := range admitted {
		evaluations = append(evaluations, NewEvaluation(row))
	}

	return evaluations, next, nil
}

// LoadReserved claims reserved evaluations for processing, recent-first.
//
// The claim is the crash-recovery path: a batch that Reserve has just handed
// out is processed from its own return value, and this only picks up rows whose
// previous owner is gone. Ownership is soft and leased — the updated_at bump IS
// the claim, and a row counts as owned while that bump is younger than
// ReservedClaimLease — so a concurrent or immediately repeated claim selects
// nothing and the model call that follows never has to hold a transaction open.
// A row whose owner crashed stops being bumped, falls out of its lease, and is
// claimed again here or, much later, returned to the queue by
// ResetStaleReservations.
func LoadReserved(ctx context.Context, db *pgxpool.Pool, projectID uuid.UUID, batchSize int32) ([]Evaluation, error) {
	return loadReserved(ctx, db, projectID, batchSize, ReservedClaimLease)
}

func loadReserved(ctx context.Context, db *pgxpool.Pool, projectID uuid.UUID, batchSize int32, lease time.Duration) ([]Evaluation, error) {
	if batchSize <= 0 {
		return nil, fmt.Errorf("load reserved skill efficacy evaluations: batch size must be positive")
	}
	// A wider batch than the lease covers would let its tail be claimed by a
	// second pass while this one is still judging it.
	if batchSize > MaxReservedClaimBatch {
		return nil, fmt.Errorf("load reserved skill efficacy evaluations: batch size %d exceeds the %d the claim lease covers", batchSize, MaxReservedClaimBatch)
	}

	rows, err := repo.New(db).LoadReservedSkillEfficacyEvaluations(ctx, repo.LoadReservedSkillEfficacyEvaluationsParams{
		ProjectID:  projectID,
		ClaimLease: pgtype.Interval{Microseconds: lease.Microseconds(), Days: 0, Months: 0, Valid: true},
		BatchSize:  batchSize,
	})
	if err != nil {
		return nil, fmt.Errorf("load reserved skill efficacy evaluations: %w", err)
	}

	// The claim orders its subselect to pick the newest rows, but an UPDATE's
	// RETURNING order is unspecified, so the batch is put back in order here.
	slices.SortFunc(rows, func(a, b repo.SkillEfficacyEvaluation) int {
		if order := b.ObservedAt.Time.Compare(a.ObservedAt.Time); order != 0 {
			return order
		}

		return bytes.Compare(b.ID[:], a.ID[:])
	})

	evaluations := make([]Evaluation, 0, len(rows))
	for _, row := range rows {
		evaluations = append(evaluations, NewEvaluation(row))
	}

	return evaluations, nil
}

// ResetStaleReservations returns evaluations whose owner is gone to the queue.
// A row is stale when it has been reserved without an updated_at bump for
// staleAfter, which callers set to StaleReservationAfter. The reset deliberately
// re-opens the budget slot; attempts is preserved, so a unit that poisons the
// judge still terminates at MaxModelAttempts.
func ResetStaleReservations(ctx context.Context, db *pgxpool.Pool, projectID uuid.UUID, staleAfter time.Duration) (int64, error) {
	if staleAfter.Microseconds() <= 0 {
		return 0, fmt.Errorf("reset stale skill efficacy reservations: stale duration must be positive")
	}

	reset, err := repo.New(db).ResetStaleSkillEfficacyReservations(ctx, repo.ResetStaleSkillEfficacyReservationsParams{
		ProjectID:  projectID,
		StaleAfter: pgtype.Interval{Microseconds: staleAfter.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("reset stale skill efficacy reservations: %w", err)
	}

	return reset, nil
}
