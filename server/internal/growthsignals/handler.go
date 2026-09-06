package growthsignals

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// auditEventTypePrefix selects the audit-derived events out of the webhook
// topic. Every audited subject publishes under its own `audit_log.*_event_v1`
// type, so matching the prefix covers all of them and keeps covering the ones
// services add later, without a list here to extend.
const auditEventTypePrefix = "audit_log."

// propertyInsertID is PostHog's deduplication key. Two captures carrying the
// same value describe one occurrence, which is what makes an at-least-once
// topic safe to report from.
const propertyInsertID = "$insert_id"

// EventHandler turns audited mutations into growth activities.
//
// It reads the webhook stream the audit logger already publishes, so every
// mutation Gram records reaches PostHog without any service having to emit
// analytics of its own.
type EventHandler struct {
	logger  *slog.Logger
	emitter *Emitter
}

func NewEventHandler(logger *slog.Logger, emitter *Emitter) *EventHandler {
	return &EventHandler{
		logger:  logger.With(attr.SlogComponent("growth-signal-events")),
		emitter: emitter,
	}
}

// Handle emits one activity for an audited mutation.
//
// It returns an error only for a failure worth redelivering. Every other
// outcome — an envelope that makes no sense, a payload that will not decode, an
// action with no ops value — acks the message, because a message this handler
// can never make sense of would otherwise redeliver until the subscription's
// retention runs out. Emission itself cannot fail the message: an activity that
// could not be captured is logged by the emitter and dropped, since analytics
// must never hold up the stream.
func (h *EventHandler) Handle(ctx context.Context, event *webhooksv1.Event, _ gcp.MessageMetadata) error {
	if event == nil {
		h.logger.ErrorContext(ctx, "dropping nil growth signal event")
		return nil
	}

	eventID := event.GetEventId()
	organizationID := event.GetOrganizationId()
	if eventID == "" || organizationID == "" {
		h.logger.ErrorContext(ctx, "dropping invalid growth signal event",
			attr.SlogOrganizationID(organizationID),
			attr.SlogOutboxPublicID(eventID),
			attr.SlogEvent(event.GetEventType()),
		)
		return nil
	}

	// Every webhook event on the topic reaches this handler, so anything that
	// is not an audit record must leave before the payload is decoded.
	if !strings.HasPrefix(event.GetEventType(), auditEventTypePrefix) {
		return nil
	}

	var payload events.AuditLogCreatedPayloadV1
	if err := json.Unmarshal(event.GetPayload(), &payload); err != nil {
		h.logger.ErrorContext(ctx, "dropping unreadable growth signal event",
			attr.SlogOutboxPublicID(eventID),
			attr.SlogEvent(event.GetEventType()),
			attr.SlogError(err),
		)
		return nil
	}

	// The event type is a coarse bucket — an MCP server creation and a metadata
	// edit share one — so the action is what says which activity this is.
	action := audit.Action(payload.Action)
	mapping := ActivityForAction(action)
	if mapping.Activity == ActivitySkip {
		h.logger.DebugContext(ctx, "growth signal excluded by action",
			attr.SlogOutboxPublicID(eventID),
			attr.SlogEvent(event.GetEventType()),
		)
		return nil
	}

	projectID := uuid.Nil
	if payload.ProjectID.Valid {
		projectID = payload.ProjectID.UUID
	}

	h.emitter.Emit(ctx, ActivityEvent{
		Activity:       mapping.Activity,
		OrganizationID: organizationID,
		ProjectID:      projectID,
		ActorID:        payload.ActorID,
		ActorType:      urn.PrincipalType(payload.ActorType),
		// The audit record carries the actor's id, not their address, so the
		// emitter resolves the email itself behind its cache.
		ActorEmail:    "",
		ActorName:     payload.ActorDisplayName,
		SubjectName:   payload.SubjectDisplayName,
		ActingSurface: payload.ActingSurface,
		AuditAction:   action,
		// Audit records address subjects by id and type rather than by page, so
		// the event takes the emitter's organization-level fallback link.
		DashboardURL: "",
		Extra:        withInsertID(mapping.Extra, eventID),
	})

	return nil
}

// withInsertID stamps the outbox event id as PostHog's deduplication key.
//
// The topic is at-least-once and this handler shares a message with its
// siblings: when any of them fails, the whole message nacks and every handler
// sees the event again. Without a stable key that is a duplicate Slack line for
// something that happened once. The outbox id is stable across redeliveries,
// which is exactly what the key needs to be.
func withInsertID(extra map[string]string, eventID string) map[string]string {
	stamped := make(map[string]string, len(extra)+1)
	for key, value := range extra {
		stamped[key] = value
	}
	stamped[propertyInsertID] = eventID

	return stamped
}
