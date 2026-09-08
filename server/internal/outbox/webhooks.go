package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/outbox/repo"
)

// eventTypeAttribute carries the Svix event type as a Pub/Sub message
// attribute. The body of a message is opaque to subscription filters, so a
// consumer that wants a subset of the webhook stream has to filter on this.
const eventTypeAttribute = "event_type"

// PublishWebhookEvent enqueues a customer-facing webhook event.
//
// THIS METHOD MUST BE CALLED WITHIN A TRANSACTION.
//
// The typed EventDef catalog is unchanged by the move to Pub/Sub: it still
// defines what customers see and still generates the Svix webhook spec. This
// only changes how an event reaches Svix — it is wrapped in a transport
// envelope, published to a topic, and delivered by a subscriber rather than by
// a Temporal activity.
func PublishWebhookEvent[T any](ctx context.Context, dbtx DBTX, orgID string, def *EventDef[T], payload T) (PublishResult, error) {
	eventID, err := uuid.NewV7()
	if err != nil {
		return PublishResult{}, fmt.Errorf("generate webhook event id: %w", err)
	}

	msg, err := buildWebhookMessage(orgID, def, payload, eventID)
	if err != nil {
		return PublishResult{}, err
	}

	return Publish(ctx, dbtx, orgID, msg)
}

// IdentifiedWebhookEvent pairs a webhook payload with a caller-pinned event id
// for PublishIdentifiedWebhookEvents.
type IdentifiedWebhookEvent[T any] struct {
	// ID becomes the envelope's event_id and the outbox row's public_id — the
	// value the delivering subscriber presents to Svix as the idempotency key.
	// Derive it deterministically from the event's identity, never randomly per
	// attempt.
	ID uuid.UUID

	// Payload is the typed event payload.
	Payload T
}

// PublishIdentifiedWebhookEvents enqueues many events of the same type in one
// COPY, with each event's id pinned by the caller instead of minted fresh. Use
// it from producers that can emit the same logical event more than once — e.g.
// a redriven Temporal activity re-running a batch whose transaction committed
// before a later step failed: a pinned id makes the repeat emission carry the
// same Svix idempotency key, so delivery dedups instead of sending the
// customer a duplicate webhook. The repeat emission is a second outbox row
// under the same public_id — publish_outbox deliberately has no unique index
// on that column, and redriven producers depend on the duplicate insert
// succeeding.
//
// THIS METHOD MUST BE CALLED WITHIN A TRANSACTION.
func PublishIdentifiedWebhookEvents[T any](ctx context.Context, dbtx repo.DBTX, orgID string, def *EventDef[T], evs []IdentifiedWebhookEvent[T]) (PublishBatchResult, error) {
	msgs := make([]Message, 0, len(evs))
	for _, ev := range evs {
		if ev.ID == uuid.Nil {
			return PublishBatchResult{}, oops.Permanent(fmt.Errorf("identified webhook event for %s has no id", def.EventType()))
		}
		msg, err := buildWebhookMessage(orgID, def, ev.Payload, ev.ID)
		if err != nil {
			return PublishBatchResult{}, err
		}

		msgs = append(msgs, msg)
	}

	return PublishBatch(ctx, dbtx, orgID, msgs)
}

// buildWebhookMessage wraps a typed payload in the transport envelope.
//
// The event id is taken as a parameter rather than defaulted by the database,
// because it has to appear both in the envelope and on the row: the subscriber
// sends it as the Svix idempotency key, and a redelivered message must present
// the same value or Svix will treat it as a new event.
func buildWebhookMessage[T any](orgID string, def *EventDef[T], payload T, eventID uuid.UUID) (Message, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Message{}, fmt.Errorf("marshal webhook event payload: %w", err)
	}

	eventIDStr := eventID.String()
	eventType := string(def.EventType())
	createdAt := time.Now().UTC().Format(time.RFC3339)

	return Message{
		Proto: webhooksv1.Event_builder{
			EventId:        &eventIDStr,
			OrganizationId: &orgID,
			EventType:      &eventType,
			Payload:        encoded,
			CreatedAt:      &createdAt,
		}.Build(),
		PublicID:   eventID,
		Attributes: map[string]string{eventTypeAttribute: eventType},
	}, nil
}
