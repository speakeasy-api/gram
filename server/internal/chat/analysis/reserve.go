package analysis

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// Reserve moves pending evaluations to reserved, spending the organization's
// per-judge budgets for the current UTC day.
//
// A project that no longer resolves reserves nothing and reports no error: a
// coordinator can be holding an id that was deleted between passes.
//
// The whole pass is one transaction whose first statement is an advisory lock
// keyed on the project's organization. The lock is held to commit, so counting
// and reserving are serialised per organization even across its projects: a
// concurrent reserver waits and then reads the committed spend of the batch
// before it. That, not the row locks, is what makes double spending impossible.
//
// Candidates are walked recent-first and every judge's counter is decremented
// in memory as units are admitted, so one batch can never grant more slots than
// the caps leave. The walk pages through the queue rather than reading a fixed
// head: a judge whose cap is spent would otherwise fill that head every pass
// and starve the judges behind it. The returned cursor is where the walk
// stopped; the zero cursor — returned when the walk reached the end of the
// queue — starts the next one at the head.
func Reserve(ctx context.Context, db *pgxpool.Pool, judges *Judges, projectID uuid.UUID, cursor PendingCursor, batchSize int32) ([]Evaluation, PendingCursor, error) {
	var fromHead PendingCursor
	if batchSize <= 0 {
		return nil, fromHead, fmt.Errorf("reserve chat analysis evaluations: batch size must be positive")
	}
	// The batch is handed straight to the caller to judge, so it is owned by the
	// same lease a claim would take. A wider batch than the lease covers would
	// let its tail fall out of the lease and be claimed a second time while this
	// caller is still working through it.
	if batchSize > MaxReservedClaimBatch {
		return nil, fromHead, fmt.Errorf("reserve chat analysis evaluations: batch size %d exceeds the %d the claim lease covers", batchSize, MaxReservedClaimBatch)
	}

	now := time.Now().UTC()
	reservedOn := pgtype.Date{
		Time:             time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC),
		InfinityModifier: pgtype.Finite,
		Valid:            true,
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fromHead, fmt.Errorf("begin chat analysis reservation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := repo.New(tx)
	if err := queries.LockProjectOrganizationChatAnalysisBudget(ctx, projectID); err != nil {
		return nil, fromHead, fmt.Errorf("lock chat analysis budget: %w", err)
	}

	settings, err := settingsForProject(ctx, queries, judges, projectID)
	if err != nil {
		return nil, fromHead, err
	}
	if settings.OrganizationID == "" || !settings.admitsWork() {
		// The project is gone, or no judge is enabled: nothing to reserve, and
		// nothing wrong.
		return nil, fromHead, nil
	}

	spend, err := queries.CountChatAnalysisJudgeSpendForProject(ctx, repo.CountChatAnalysisJudgeSpendForProjectParams{
		ProjectID:  projectID,
		ReservedOn: reservedOn,
	})
	if err != nil {
		return nil, fromHead, fmt.Errorf("count chat analysis judge spend: %w", err)
	}
	remaining := make(map[string]int64, len(settings.JudgeDailyCaps))
	for judge, dailyCap := range settings.JudgeDailyCaps {
		remaining[judge] = int64(dailyCap)
	}
	for _, row := range spend {
		if _, ok := remaining[row.Judge]; ok {
			remaining[row.Judge] = max(0, remaining[row.Judge]-row.Spend)
		}
	}

	admitted := make([]repo.ChatAnalysisEvaluation, 0, batchSize)
	ids := make([]uuid.UUID, 0, batchSize)

	resumed := cursor != fromHead
	page := repo.ListPendingChatAnalysisEvaluationsParams{
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
		if len(ids) >= int(batchSize) {
			break
		}

		candidates, err := queries.ListPendingChatAnalysisEvaluations(ctx, page)
		if err != nil {
			return nil, fromHead, fmt.Errorf("list pending chat analysis evaluations: %w", err)
		}
		if len(candidates) == 0 {
			next = fromHead
			break
		}

		examined := 0
		for _, candidate := range candidates {
			if len(ids) >= int(batchSize) {
				break
			}
			examined++
			next = PendingCursor{ObservedAt: candidate.ObservedAt.Time, ID: candidate.ID}

			if remaining[candidate.Judge] <= 0 {
				// A disabled judge's units stay pending too: enqueue only admits
				// enabled judges, but a judge can be switched off while its queue
				// still holds rows.
				continue
			}

			remaining[candidate.Judge]--
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

	reserved, err := queries.ReserveChatAnalysisEvaluations(ctx, repo.ReserveChatAnalysisEvaluationsParams{
		ReservedOn: reservedOn,
		ProjectID:  projectID,
		Ids:        ids,
		Inactivity: pgtype.Interval{Microseconds: InactivityWindow.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	if err != nil {
		return nil, fromHead, fmt.Errorf("reserve chat analysis evaluations: %w", err)
	}
	// The UPDATE rechecks the quiet window the candidate read applied, so a
	// candidate whose session wrote a message between the two is legitimately
	// not written: it stays pending, and only the rows actually reserved are
	// handed out. Its in-memory budget decrement dies with this pass — the
	// durable spend count only ever sees committed reserved_on stamps.
	written := make(map[uuid.UUID]struct{}, len(reserved))
	for _, id := range reserved {
		written[id] = struct{}{}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fromHead, fmt.Errorf("commit chat analysis reservation: %w", err)
	}

	evaluations := make([]Evaluation, 0, len(reserved))
	for _, row := range admitted {
		if _, ok := written[row.ID]; ok {
			evaluations = append(evaluations, NewEvaluation(row))
		}
	}

	return evaluations, next, nil
}

// LoadReserved claims reserved evaluations for processing, recent-first.
//
// The claim is the crash-recovery path: a batch that Reserve has just handed
// out is processed from its own return value, and this only picks up rows whose
// previous owner is gone. Ownership is soft and leased — the updated_at bump IS
// the claim — so a concurrent or immediately repeated claim selects nothing and
// the model call that follows never has to hold a transaction open.
func LoadReserved(ctx context.Context, db *pgxpool.Pool, projectID uuid.UUID, batchSize int32) ([]Evaluation, error) {
	if batchSize <= 0 {
		return nil, fmt.Errorf("load reserved chat analysis evaluations: batch size must be positive")
	}
	// A wider batch than the lease covers would let its tail be claimed by a
	// second pass while this one is still judging it.
	if batchSize > MaxReservedClaimBatch {
		return nil, fmt.Errorf("load reserved chat analysis evaluations: batch size %d exceeds the %d the claim lease covers", batchSize, MaxReservedClaimBatch)
	}

	rows, err := repo.New(db).LoadReservedChatAnalysisEvaluations(ctx, repo.LoadReservedChatAnalysisEvaluationsParams{
		ProjectID:  projectID,
		ClaimLease: pgtype.Interval{Microseconds: ReservedClaimLease.Microseconds(), Days: 0, Months: 0, Valid: true},
		BatchSize:  batchSize,
	})
	if err != nil {
		return nil, fmt.Errorf("load reserved chat analysis evaluations: %w", err)
	}

	// The claim orders its subselect to pick the newest rows, but an UPDATE's
	// RETURNING order is unspecified, so the batch is put back in order here.
	slices.SortFunc(rows, func(a, b repo.ChatAnalysisEvaluation) int {
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
// The reset deliberately re-opens the budget slot; attempts is preserved, so a
// unit that poisons the judge still terminates at MaxModelAttempts.
func ResetStaleReservations(ctx context.Context, db *pgxpool.Pool, projectID uuid.UUID, staleAfter time.Duration) (int64, error) {
	if staleAfter.Microseconds() <= 0 {
		return 0, fmt.Errorf("reset stale chat analysis reservations: stale duration must be positive")
	}

	reset, err := repo.New(db).ResetStaleChatAnalysisReservations(ctx, repo.ResetStaleChatAnalysisReservationsParams{
		ProjectID:  projectID,
		StaleAfter: pgtype.Interval{Microseconds: staleAfter.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("reset stale chat analysis reservations: %w", err)
	}

	return reset, nil
}
