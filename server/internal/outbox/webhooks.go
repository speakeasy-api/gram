package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
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
	msg, err := buildWebhookMessage(orgID, def, payload)
	if err != nil {
		return PublishResult{}, err
	}

	return Publish(ctx, dbtx, orgID, msg)
}

// PublishWebhookEvents enqueues many events of the same type in one COPY.
//
// THIS METHOD MUST BE CALLED WITHIN A TRANSACTION.
func PublishWebhookEvents[T any](ctx context.Context, dbtx repo.DBTX, orgID string, def *EventDef[T], payloads []T) (PublishBatchResult, error) {
	msgs := make([]Message, 0, len(payloads))
	for _, payload := range payloads {
		msg, err := buildWebhookMessage(orgID, def, payload)
		if err != nil {
			return PublishBatchResult{}, err
		}

		msgs = append(msgs, msg)
	}

	return PublishBatch(ctx, dbtx, orgID, msgs)
}

// buildWebhookMessage wraps a typed payload in the transport envelope.
//
// The event id is minted here rather than by the database default, because it
// has to appear both in the envelope and on the row: the subscriber sends it as
// the Svix idempotency key, and a redelivered message must present the same
// value or Svix will treat it as a new event.
func buildWebhookMessage[T any](orgID string, def *EventDef[T], payload T) (Message, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Message{}, fmt.Errorf("marshal webhook event payload: %w", err)
	}

	eventID, err := uuid.NewV7()
	if err != nil {
		return Message{}, fmt.Errorf("generate webhook event id: %w", err)
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
