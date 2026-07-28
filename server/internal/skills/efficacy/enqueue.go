package efficacy

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

// EnqueuePageResult reports what one page did for a project.
type EnqueuePageResult struct {
	// Scanned is the number of pending activations the page read, including
	// ones whose unit could not be enqueued. It never exceeds the page size.
	Scanned int
	// Units is the number of distinct scoring units those activations folded into.
	Units int
	// Confirmed is the number of distinct units that exist as evaluations after
	// the page, whether this page inserted them or an earlier one did.
	Confirmed int
	// Stamped is the number of activations marked efficacy_enqueued_at.
	Stamped int
	// NextCursor is where the next page starts. It is strictly past the cursor
	// the page was given whenever the page read anything, whether or not the
	// page confirmed a single unit, and equal to it when the queue was already
	// empty. Chaining it is what carries a walk past activations that can never
	// be scored.
	NextCursor EnqueueCursor
	// Exhausted reports that the page reached the end of the pending set — it
	// read fewer activations than it asked for. A caller resumes from
	// NextCursor when it is false and stops when it is true.
	Exhausted bool
}

// EnqueuePage turns one bounded page of reconciled skill activations into
// pending evaluations, one per (project, surface, session, skill version)
// scoring unit.
//
// This is the durable primitive the efficacy pipeline is built on, and it is
// deliberately the whole of it: a call reads at most pageSize activations in
// one short transaction and returns the cursor it stopped at, so a coordinator
// — a Temporal workflow persisting NextCursor between activities — decides how
// far a walk goes, and no single transaction is ever held open across it. Pass
// a zero EnqueueCursor to start at the head of the queue.
//
// Eligibility is not part of the scan: the chat id is derived from the session
// id with the same mapping the capture paths write under, and the insert is
// what rechecks that the project and chat are live and the transcript has gone
// quiet. Confirmation and reservation also require the activation itself to be
// quiet. An activation whose unit cannot be enqueued is paged past — the cursor
// advances over every row read, scoreable or not — so a head of abandoned
// sessions can never starve the activations behind it. Deleted-chat observations
// are retired because they can never become scoreable.
//
// The organization's entitlement and its two off switches are checked before
// anything is read: an organization no reservation can spend for gets no queue
// built for it at all, rather than a backlog of pending rows nothing will ever
// pick up.
//
// The insert is idempotent and refreshes a pending unit when its session resumes.
// The confirmation read is the only thing that authorises the stamp — an
// activation whose unit is absent or not yet quiet stays unstamped for a later
// walk.
func EnqueuePage(ctx context.Context, db *pgxpool.Pool, features FeatureChecker, projectID uuid.UUID, cursor EnqueueCursor, pageSize int32) (EnqueuePageResult, error) {
	if pageSize <= 0 || pageSize > MaxEnqueuePageSize {
		return EnqueuePageResult{}, fmt.Errorf("enqueue skill efficacy evaluations: page size must be between 1 and %d, got %d", MaxEnqueuePageSize, pageSize)
	}

	admits, err := admitsWork(ctx, repo.New(db), features, projectID)
	if err != nil {
		return EnqueuePageResult{}, err
	}
	if !admits {
		// An organization the reservation can never spend for gains nothing from
		// a queue: the rows would sit pending until the entitlement arrived, and
		// until then the discovery read would name the project on every tick for
		// a pass that reserves none of them. Reporting the walk exhausted from the
		// head leaves the activations unstamped, so an entitlement granted later
		// still finds them.
		return EnqueuePageResult{Scanned: 0, Units: 0, Confirmed: 0, Stamped: 0, NextCursor: EnqueueCursor{SeenAt: time.Time{}, ID: uuid.Nil}, Exhausted: true}, nil
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return EnqueuePageResult{}, fmt.Errorf("begin skill efficacy enqueue: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := repo.New(tx)

	// An observation id is never nil, so it is what distinguishes a resumed
	// cursor from the zero value that starts at the head.
	started := cursor.ID != uuid.Nil
	page, err := queries.ListPendingSkillObservations(ctx, repo.ListPendingSkillObservationsParams{
		ProjectID:   projectID,
		AfterSeenAt: pgtype.Timestamptz{Time: cursor.SeenAt, InfinityModifier: pgtype.Finite, Valid: started},
		AfterID:     uuid.NullUUID{UUID: cursor.ID, Valid: started},
		BatchSize:   pageSize,
	})
	if err != nil {
		return EnqueuePageResult{}, fmt.Errorf("list pending skill observations: %w", err)
	}
	if len(page) == 0 {
		return EnqueuePageResult{Scanned: 0, Units: 0, Confirmed: 0, Stamped: 0, NextCursor: cursor, Exhausted: true}, nil
	}

	last := page[len(page)-1]
	result := EnqueuePageResult{
		Scanned:    len(page),
		Units:      0,
		Confirmed:  0,
		Stamped:    0,
		NextCursor: EnqueueCursor{SeenAt: last.SeenAt.Time, ID: last.ID},
		Exhausted:  len(page) < int(pageSize),
	}

	inactivity := pgtype.Interval{Microseconds: InactivityWindow.Microseconds(), Days: 0, Months: 0, Valid: true}
	units, confirmed, stamped, err := enqueueCandidates(ctx, queries, projectID, inactivity, page)
	if err != nil {
		return EnqueuePageResult{}, err
	}
	result.Units = units
	result.Confirmed = confirmed
	result.Stamped = stamped

	if err := tx.Commit(ctx); err != nil {
		return EnqueuePageResult{}, fmt.Errorf("commit skill efficacy enqueue: %w", err)
	}

	return result, nil
}

// enqueueCandidates inserts, confirms and stamps the units one page of
// activations folds into, returning how many units it saw, how many of those
// exist as evaluations, and how many activations it stamped.
func enqueueCandidates(
	ctx context.Context,
	queries *repo.Queries,
	projectID uuid.UUID,
	inactivity pgtype.Interval,
	page []repo.ListPendingSkillObservationsRow,
) (int, int, int, error) {
	candidates := foldCandidates(page)

	insert := repo.EnqueueSkillEfficacyEvaluationsParams{
		ProjectID:        projectID,
		Inactivity:       inactivity,
		SessionIds:       make([]string, 0, len(candidates)),
		Surfaces:         make([]string, 0, len(candidates)),
		ChatIds:          make([]uuid.UUID, 0, len(candidates)),
		SkillIds:         make([]uuid.UUID, 0, len(candidates)),
		SkillVersionIds:  make([]uuid.UUID, 0, len(candidates)),
		CanonicalSha256s: make([]string, 0, len(candidates)),
		ObservedAts:      make([]pgtype.Timestamptz, 0, len(candidates)),
		UserIds:          make([]string, 0, len(candidates)),
		UserEmails:       make([]string, 0, len(candidates)),
	}
	for _, candidate := range candidates {
		insert.SessionIds = append(insert.SessionIds, candidate.SessionID)
		insert.Surfaces = append(insert.Surfaces, candidate.Surface)
		insert.UserIds = append(insert.UserIds, candidate.UserID)
		insert.UserEmails = append(insert.UserEmails, candidate.UserEmail)
		insert.ChatIds = append(insert.ChatIds, candidate.ChatID)
		insert.SkillIds = append(insert.SkillIds, candidate.SkillID)
		insert.SkillVersionIds = append(insert.SkillVersionIds, candidate.SkillVersionID)
		insert.CanonicalSha256s = append(insert.CanonicalSha256s, candidate.CanonicalSha256)
		insert.ObservedAts = append(insert.ObservedAts, conv.ToPGTimestamptz(candidate.ObservedAt))
	}

	if err := queries.EnqueueSkillEfficacyEvaluations(ctx, insert); err != nil {
		return 0, 0, 0, fmt.Errorf("insert skill efficacy evaluations: %w", err)
	}

	rows, err := queries.ListSkillEfficacyEvaluationUnits(ctx, repo.ListSkillEfficacyEvaluationUnitsParams{
		ProjectID:       projectID,
		SessionIds:      insert.SessionIds,
		Surfaces:        insert.Surfaces,
		SkillVersionIds: insert.SkillVersionIds,
		UserIds:         insert.UserIds,
		UserEmails:      insert.UserEmails,
		Inactivity:      inactivity,
	})
	if err != nil {
		return 0, 0, 0, fmt.Errorf("confirm skill efficacy evaluation units: %w", err)
	}

	present := make(map[unitKey]struct{}, len(rows))
	for _, row := range rows {
		present[unitKey{
			sessionID:      row.SessionID,
			surface:        row.Surface,
			skillVersionID: row.SkillVersionID,
			userID:         row.UserID,
			userEmail:      row.UserEmail,
		}] = struct{}{}
	}

	deletedChatIDs, err := queries.ListDeletedSkillEfficacyChatIDs(ctx, repo.ListDeletedSkillEfficacyChatIDsParams{
		ProjectID: projectID,
		ChatIds:   insert.ChatIds,
	})
	if err != nil {
		return 0, 0, 0, fmt.Errorf("list deleted skill efficacy chats: %w", err)
	}
	deleted := make(map[uuid.UUID]struct{}, len(deletedChatIDs))
	for _, id := range deletedChatIDs {
		deleted[id] = struct{}{}
	}

	observationIDs := make([]uuid.UUID, 0, len(page))
	retiredIDs := make([]uuid.UUID, 0, len(page))
	for _, candidate := range candidates {
		if _, ok := deleted[candidate.ChatID]; ok {
			retiredIDs = append(retiredIDs, candidate.ObservationIDs...)
			continue
		}
		if _, ok := present[candidate.key()]; !ok {
			continue
		}
		observationIDs = append(observationIDs, candidate.ObservationIDs...)
	}
	if len(retiredIDs) > 0 {
		if _, err := queries.RetireSkillObservationsForDeletedChats(ctx, repo.RetireSkillObservationsForDeletedChatsParams{
			ProjectID:      projectID,
			ObservationIds: retiredIDs,
		}); err != nil {
			return 0, 0, 0, fmt.Errorf("retire deleted-chat skill observations: %w", err)
		}
	}
	if len(observationIDs) == 0 {
		return len(candidates), len(present), 0, nil
	}

	stamped, err := queries.MarkSkillObservationsEfficacyEnqueued(ctx, repo.MarkSkillObservationsEfficacyEnqueuedParams{
		ProjectID:      projectID,
		ObservationIds: observationIDs,
	})
	if err != nil {
		return 0, 0, 0, fmt.Errorf("mark skill observations efficacy enqueued: %w", err)
	}

	return len(candidates), len(present), int(stamped), nil
}

// unitKey is the scoring-unit identity within a project. The actor is part of
// it even though the stored evaluation is unique on (session, surface, skill
// version) alone: activations that disagree about whose session it is stay
// separate candidates, each answerable for its own claim, so one that names
// another actor's session is refused on its own rather than dragging the
// rightful owner's activations down with it.
type unitKey struct {
	sessionID      string
	surface        string
	skillVersionID uuid.UUID
	userID         string
	userEmail      string
}

func (c Candidate) key() unitKey {
	return unitKey{
		sessionID:      c.SessionID,
		surface:        c.Surface,
		skillVersionID: c.SkillVersionID,
		userID:         c.UserID,
		userEmail:      c.UserEmail,
	}
}

// foldCandidates folds activations onto their scoring unit, preserving the scan
// order so a page is deterministic. The chat is derived from the session id with
// the capture-path mapping, which is the only place a session's transcript is
// ever written. observed_at is the latest activation the unit saw, which is what
// the reservation pass orders on.
//
// Activations only fold together when they agree on the actor, so a page that
// mixes claims about one session carries a candidate per claim and each stands
// or falls on the insert's binding alone.
func foldCandidates(observations []repo.ListPendingSkillObservationsRow) []Candidate {
	candidates := make([]Candidate, 0, len(observations))
	indexes := make(map[unitKey]int, len(observations))

	for _, observation := range observations {
		key := unitKey{
			sessionID:      observation.SessionID,
			surface:        observation.Surface,
			skillVersionID: observation.SkillVersionID,
			userID:         observation.UserID,
			userEmail:      observation.UserEmail,
		}
		index, ok := indexes[key]
		if !ok {
			indexes[key] = len(candidates)
			candidates = append(candidates, Candidate{
				SessionID:       observation.SessionID,
				Surface:         observation.Surface,
				UserID:          observation.UserID,
				UserEmail:       observation.UserEmail,
				ChatID:          chat.SessionIDToChatID(observation.SessionID),
				SkillID:         observation.SkillID,
				SkillVersionID:  observation.SkillVersionID,
				CanonicalSha256: observation.CanonicalSha256,
				ObservedAt:      observation.SeenAt.Time,
				ObservationIDs:  []uuid.UUID{observation.ID},
			})
			continue
		}

		candidates[index].ObservationIDs = append(candidates[index].ObservationIDs, observation.ID)
		if observation.SeenAt.Time.After(candidates[index].ObservedAt) {
			candidates[index].ObservedAt = observation.SeenAt.Time
		}
	}

	return candidates
}
