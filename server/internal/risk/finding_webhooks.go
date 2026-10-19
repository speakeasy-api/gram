package risk

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/risk/policyflags"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// RiskFindingCreatedEvent carries one finding webhook and its flag scope.
type RiskFindingCreatedEvent struct {
	// ProjectID scopes the rollout flag.
	ProjectID uuid.UUID

	// OrganizationID scopes the event and rollout flag.
	OrganizationID string

	// Event is the payload with its stable delivery id.
	Event outbox.IdentifiedWebhookEvent[events.RiskFindingCreatedPayloadV1]
}

// RiskFindingCreatedEmitter publishes finding-created events after ClickHouse accepts their rows.
type RiskFindingCreatedEmitter interface {
	Emit(ctx context.Context, candidates []RiskFindingCreatedEvent) error
}

// OutboxRiskFindingCreatedEmitter publishes enabled organizations through the transactional outbox.
type OutboxRiskFindingCreatedEmitter struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	flags  feature.Provider
}

// NewOutboxRiskFindingCreatedEmitter builds the ClickHouse-side webhook emitter.
func NewOutboxRiskFindingCreatedEmitter(logger *slog.Logger, db *pgxpool.Pool, flags feature.Provider) *OutboxRiskFindingCreatedEmitter {
	return &OutboxRiskFindingCreatedEmitter{logger: logger, db: db, flags: flags}
}

// Emit appends stable-id events only for projects switched to this emitter.
func (e *OutboxRiskFindingCreatedEmitter) Emit(ctx context.Context, candidates []RiskFindingCreatedEvent) error {
	if len(candidates) == 0 {
		return nil
	}

	ctx = policyflags.WithRequestMemo(ctx)
	queries := repo.New(e.db)
	enabledProjects := make(map[uuid.UUID]bool)
	byOrganization := make(map[string][]outbox.IdentifiedWebhookEvent[events.RiskFindingCreatedPayloadV1])
	for _, candidate := range candidates {
		enabled, ok := enabledProjects[candidate.ProjectID]
		if !ok {
			// A failed lookup fails the batch so redelivery retries it rather than dropping the webhook.
			var err error
			enabled, err = policyflags.ProjectFlagChecked(ctx, e.logger, queries, e.flags, candidate.OrganizationID, candidate.ProjectID, feature.FlagRiskFindingsClickHouseCutover)
			if err != nil {
				return fmt.Errorf("check finding webhook emitter flag: %w", err)
			}
			enabledProjects[candidate.ProjectID] = enabled
		}
		if enabled {
			byOrganization[candidate.OrganizationID] = append(byOrganization[candidate.OrganizationID], candidate.Event)
		}
	}
	if len(byOrganization) == 0 {
		return nil
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
