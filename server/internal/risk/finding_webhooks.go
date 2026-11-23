package risk

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
)

// RiskFindingCreatedEvent carries one finding webhook for its organization.
type RiskFindingCreatedEvent struct {
	// OrganizationID scopes the outbox entry.
	OrganizationID string

	// Event is the payload with its stable delivery id.
	Event outbox.IdentifiedWebhookEvent[events.RiskFindingCreatedPayloadV1]
}

// RiskFindingCreatedEmitter publishes finding-created events after ClickHouse accepts their rows.
type RiskFindingCreatedEmitter interface {
	Emit(ctx context.Context, candidates []RiskFindingCreatedEvent) error
}

// OutboxRiskFindingCreatedEmitter publishes finding-created events through the transactional outbox.
type OutboxRiskFindingCreatedEmitter struct {
	db *pgxpool.Pool
}

// NewOutboxRiskFindingCreatedEmitter builds the ClickHouse-side webhook emitter.
func NewOutboxRiskFindingCreatedEmitter(db *pgxpool.Pool) *OutboxRiskFindingCreatedEmitter {
	return &OutboxRiskFindingCreatedEmitter{db: db}
}

// Emit appends stable-id events grouped by organization in one transaction.
func (e *OutboxRiskFindingCreatedEmitter) Emit(ctx context.Context, candidates []RiskFindingCreatedEvent) error {
	if len(candidates) == 0 {
		return nil
	}

	byOrganization := make(map[string][]outbox.IdentifiedWebhookEvent[events.RiskFindingCreatedPayloadV1])
	for _, candidate := range candidates {
		byOrganization[candidate.OrganizationID] = append(byOrganization[candidate.OrganizationID], candidate.Event)
	}

	tx, err := e.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin finding webhook transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	organizationIDs := make([]string, 0, len(byOrganization))
	for organizationID := range byOrganization {
		organizationIDs = append(organizationIDs, organizationID)
	}
	slices.Sort(organizationIDs)
	for _, organizationID := range organizationIDs {
		if _, err := outbox.PublishIdentifiedWebhookEvents(ctx, tx, organizationID, events.RiskFindingCreatedV1, byOrganization[organizationID]); err != nil {
			return fmt.Errorf("publish risk finding created webhooks: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit finding webhooks: %w", err)
	}
	return nil
}
