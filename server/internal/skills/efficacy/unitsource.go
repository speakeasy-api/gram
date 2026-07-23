package efficacy

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	analysisrepo "github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

// unitCursor is the source's position in a project's pending activations,
// which are walked oldest-first on the unique (seen_at, id) key.
type unitCursor struct {
	SeenAt time.Time `json:"seen_at"`
	ID     uuid.UUID `json:"id"`
}

// EnqueueUnitsPage implements analysis.UnitSource: one bounded page of
// reconciled, unstamped skill activations is folded into per-session units and
// enqueued as (chat, skill_efficacy) evaluations.
//
// The chat id is derived from the session id with the same mapping the capture
// paths write under. An activation whose chat is deleted is retired — it can
// never become judgeable — and one whose chat has not arrived yet is left
// unstamped for a later walk. Everything else is stamped once its unit is in
// the queue, which is what keeps the walk from re-reading a growing history:
// the stamp, not the cursor, is the durable progress marker, and the cursor
// resetting to the head on exhaustion only re-reads what is still unstamped.
func (j *Judge) EnqueueUnitsPage(ctx context.Context, db *pgxpool.Pool, projectID uuid.UUID, rawCursor json.RawMessage, pageSize int32) (analysis.EnqueueSourcePage, error) {
	var cursor unitCursor
	if len(rawCursor) > 0 {
		if err := json.Unmarshal(rawCursor, &cursor); err != nil {
			return analysis.EnqueueSourcePage{}, fmt.Errorf("decode skill efficacy unit cursor: %w", err)
		}
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return analysis.EnqueueSourcePage{}, fmt.Errorf("begin skill efficacy unit enqueue: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := repo.New(tx)

	started := cursor.ID != uuid.Nil
	page, err := queries.ListPendingSkillObservations(ctx, repo.ListPendingSkillObservationsParams{
		ProjectID:   projectID,
		AfterSeenAt: pgtype.Timestamptz{Time: cursor.SeenAt, InfinityModifier: pgtype.Finite, Valid: started},
		AfterID:     uuid.NullUUID{UUID: cursor.ID, Valid: started},
		BatchSize:   pageSize,
	})
	if err != nil {
		return analysis.EnqueueSourcePage{}, fmt.Errorf("list pending skill observations: %w", err)
	}
	if len(page) == 0 {
		return analysis.EnqueueSourcePage{Scanned: 0, NextCursor: rawCursor, Exhausted: true}, nil
	}

	organizationID, err := queries.GetSkillEfficacyProjectOrganization(ctx, projectID)
	if err != nil {
		return analysis.EnqueueSourcePage{}, fmt.Errorf("resolve skill efficacy project organization: %w", err)
	}

	// Fold activations onto their session. The unit is the session: every
	// activation in it is stamped or retired together, and the judge re-derives
	// the per-skill grouping at publication time.
	type sessionUnit struct {
		chatID         uuid.UUID
		observedAt     time.Time
		observationIDs []uuid.UUID
	}
	units := make(map[string]*sessionUnit, len(page))
	order := make([]string, 0, len(page))
	for _, observation := range page {
		unit, ok := units[observation.SessionID]
		if !ok {
			unit = &sessionUnit{
				chatID:         chat.SessionIDToChatID(observation.SessionID),
				observedAt:     observation.SeenAt.Time,
				observationIDs: nil,
			}
			units[observation.SessionID] = unit
			order = append(order, observation.SessionID)
		}
		unit.observationIDs = append(unit.observationIDs, observation.ID)
		if observation.SeenAt.Time.After(unit.observedAt) {
			unit.observedAt = observation.SeenAt.Time
		}
	}

	chatIDs := make([]uuid.UUID, 0, len(order))
	for _, sessionID := range order {
		chatIDs = append(chatIDs, units[sessionID].chatID)
	}
	states, err := queries.ListSkillEfficacyChatStates(ctx, repo.ListSkillEfficacyChatStatesParams{
		ProjectID: projectID,
		ChatIds:   chatIDs,
	})
	if err != nil {
		return analysis.EnqueueSourcePage{}, fmt.Errorf("list skill efficacy chat states: %w", err)
	}
	deleted := make(map[uuid.UUID]struct{}, len(states))
	live := make(map[uuid.UUID]struct{}, len(states))
	for _, state := range states {
		if state.Deleted {
			deleted[state.ID] = struct{}{}
		} else {
			live[state.ID] = struct{}{}
		}
	}

	insert := analysisrepo.EnqueueChatAnalysisEvaluationsParams{
		ProjectID:       projectID,
		OrganizationIds: make([]string, 0, len(order)),
		ChatIds:         make([]uuid.UUID, 0, len(order)),
		SessionIds:      make([]string, 0, len(order)),
		ObservedAts:     make([]pgtype.Timestamptz, 0, len(order)),
		Judges:          []string{JudgeName},
	}
	stampIDs := make([]uuid.UUID, 0, len(page))
	retireIDs := make([]uuid.UUID, 0, len(page))
	for _, sessionID := range order {
		unit := units[sessionID]
		if _, ok := deleted[unit.chatID]; ok {
			retireIDs = append(retireIDs, unit.observationIDs...)
			continue
		}
		if _, ok := live[unit.chatID]; !ok {
			// The chat has not arrived yet; the activation stays unstamped so a
			// later walk retries once the transcript exists.
			continue
		}
		insert.OrganizationIds = append(insert.OrganizationIds, organizationID)
		insert.ChatIds = append(insert.ChatIds, unit.chatID)
		insert.SessionIds = append(insert.SessionIds, sessionID)
		insert.ObservedAts = append(insert.ObservedAts, pgtype.Timestamptz{Time: unit.observedAt, InfinityModifier: pgtype.Finite, Valid: true})
		stampIDs = append(stampIDs, unit.observationIDs...)
	}

	if len(insert.ChatIds) > 0 {
		if err := analysisrepo.New(tx).EnqueueChatAnalysisEvaluations(ctx, insert); err != nil {
			return analysis.EnqueueSourcePage{}, fmt.Errorf("enqueue skill efficacy units: %w", err)
		}
	}
	if len(retireIDs) > 0 {
		if _, err := queries.RetireSkillObservationsForDeletedChats(ctx, repo.RetireSkillObservationsForDeletedChatsParams{
			ProjectID:      projectID,
			ObservationIds: retireIDs,
		}); err != nil {
			return analysis.EnqueueSourcePage{}, fmt.Errorf("retire deleted-chat skill observations: %w", err)
		}
	}
	if len(stampIDs) > 0 {
		if _, err := queries.MarkSkillObservationsEfficacyEnqueued(ctx, repo.MarkSkillObservationsEfficacyEnqueuedParams{
			ProjectID:      projectID,
			ObservationIds: stampIDs,
		}); err != nil {
			return analysis.EnqueueSourcePage{}, fmt.Errorf("mark skill observations enqueued: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return analysis.EnqueueSourcePage{}, fmt.Errorf("commit skill efficacy unit enqueue: %w", err)
	}

	last := page[len(page)-1]
	next, err := json.Marshal(unitCursor{SeenAt: last.SeenAt.Time, ID: last.ID})
	if err != nil {
		return analysis.EnqueueSourcePage{}, fmt.Errorf("encode skill efficacy unit cursor: %w", err)
	}

	return analysis.EnqueueSourcePage{
		Scanned:    len(page),
		NextCursor: next,
		Exhausted:  len(page) < int(pageSize),
	}, nil
}
