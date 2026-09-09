package networkingress

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	networkingressv1 "github.com/speakeasy-api/gram/infra/gen/gram/networkingress/v1"
	"github.com/speakeasy-api/gram/server/internal/outbox"
)

// ReconcileRequester atomically records lifecycle work with its desired state.
type ReconcileRequester interface {
	Enqueue(context.Context, pgx.Tx, string, uuid.UUID) error
}

// OutboxRequester routes lifecycle wakes to the environment's authoritative queue.
type OutboxRequester struct {
	queue string
}

func NewOutboxRequester(queue string) *OutboxRequester {
	return &OutboxRequester{queue: queue}
}

func (r *OutboxRequester) Enqueue(ctx context.Context, tx pgx.Tx, organizationID string, ingressID uuid.UUID) error {
	// No lifecycle infrastructure is configured in a dormant deployment.
	// Containment still commits; the authoritative sweep recovers tombstones
	// when infrastructure is restored, while expansion stays admission-gated.
	if r.queue == "" {
		return nil
	}
	if ingressID == uuid.Nil {
		return fmt.Errorf("network ingress reconciliation requires an ingress id")
	}
	_, err := outbox.Publish(ctx, tx, organizationID, outbox.Message{
		Proto: networkingressv1.ReconcileRequested_builder{
			IngressId: new(ingressID.String()), TemporalTaskQueue: new(r.queue),
		}.Build(),
		PublicID: uuid.Nil, Attributes: nil,
	})
	if err != nil {
		return fmt.Errorf("enqueue network ingress reconciliation: %w", err)
	}
	return nil
}

// HealthRefresher waits only for an interactive refresh, not for config writes.
type HealthRefresher interface {
	RefreshNetworkIngress(context.Context, uuid.UUID) error
}
