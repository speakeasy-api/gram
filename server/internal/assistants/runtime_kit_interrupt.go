package assistants

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
)

// requestKitInterrupt is shared by API processes and worker processes through
// PostgreSQL. No ACP connection or process-local controller registry is needed.
func (p *KitRuntimeProtocol) requestKitInterrupt(ctx context.Context, r assistantRuntimeRecord, thread uuid.UUID) (bool, error) {
	tx, q, err := p.config.Core.beginKitRuntimeTransaction(ctx, r.ProjectID, r.AssistantID, r.ID)
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrKitInterruptPending, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	id, err := q.RequestKitInterrupt(ctx, assistantrepo.RequestKitInterruptParams{
		ProjectID: r.ProjectID, AssistantID: r.AssistantID, RuntimeID: r.ID, ThreadID: thread,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%w: enqueue: %w", ErrKitInterruptPending, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("%w: commit: %w", ErrKitInterruptPending, err)
	}
	// Never wait while holding the admission fence: the projector needs it.
	wait, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return waitKitInterrupt(wait, 100*time.Millisecond, func(ctx context.Context) (string, error) {
		return assistantrepo.New(p.config.Core.db).GetKitInterruptOutcome(ctx, assistantrepo.GetKitInterruptOutcomeParams{ProjectID: r.ProjectID, RequestID: id})
	})
}

func (p *KitRuntimeProtocol) kitInterruptPending(ctx context.Context, r assistantRuntimeRecord, req runTurnRequest) (bool, error) {
	if req.EventID == uuid.Nil || req.EventAttempt <= 0 {
		return false, nil
	}
	pending, err := assistantrepo.New(p.config.Core.db).KitInterruptPending(ctx, assistantrepo.KitInterruptPendingParams{
		ProjectID: r.ProjectID, RuntimeID: r.ID, EventID: req.EventID, Attempt: int64(req.EventAttempt),
	})
	if err != nil {
		return false, fmt.Errorf("read Kit interrupt request: %w", err)
	}
	return pending, nil
}

func (p *KitRuntimeProtocol) acknowledgeKitInterrupt(ctx context.Context, r assistantRuntimeRecord, req runTurnRequest, outcome string) error {
	if req.EventID == uuid.Nil || req.EventAttempt <= 0 {
		return nil
	}
	queries := assistantrepo.New(p.config.Core.db)
	count, err := queries.AcknowledgeKitInterrupt(ctx, assistantrepo.AcknowledgeKitInterruptParams{
		ProjectID: r.ProjectID, RuntimeID: r.ID, EventID: req.EventID, Attempt: int64(req.EventAttempt), Outcome: outcome,
	})
	if err != nil {
		return fmt.Errorf("acknowledge Kit interruption: %w", err)
	}
	if count == 0 {
		pending, err := queries.HasPendingKitInterruptRequest(ctx, assistantrepo.HasPendingKitInterruptRequestParams{ProjectID: r.ProjectID, RuntimeID: r.ID, EventID: req.EventID, Attempt: int64(req.EventAttempt)})
		if err != nil {
			return fmt.Errorf("verify Kit interruption acknowledgment: %w", err)
		}
		if pending {
			return ErrKitInterruptPending
		}
	}
	return nil
}
