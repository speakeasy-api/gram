package billingnotifications

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
)

type BillingEmailScheduler interface {
	ScheduleAccessPaused(context.Context, SendAccessPausedInput) error
	SchedulePaygActivated(context.Context, SendPaygActivatedInput) error
}

type EventHandler struct {
	logger    *slog.Logger
	scheduler BillingEmailScheduler
}

func NewEventHandler(logger *slog.Logger, scheduler BillingEmailScheduler) *EventHandler {
	return &EventHandler{
		logger:    logger.With(attr.SlogComponent("billing-notification-events")),
		scheduler: scheduler,
	}
}

func (h *EventHandler) Handle(ctx context.Context, event *webhooksv1.Event, _ gcp.MessageMetadata) error {
	if event == nil {
		h.logger.ErrorContext(ctx, "dropping nil billing notification event")
		return nil
	}
	eventID := event.GetEventId()
	organizationID := event.GetOrganizationId()
	if eventID == "" || organizationID == "" {
		h.logger.ErrorContext(ctx, "dropping invalid billing notification event",
			attr.SlogOrganizationID(organizationID),
			attr.SlogOutboxPublicID(eventID),
			attr.SlogEvent(event.GetEventType()),
		)
		return nil
	}

	// Every webhook event on the topic reaches this handler, so anything that is
	// not an audited billing transition must leave before the payload is read.
	billingEvent := string(events.OrganizationBillingV1.EventType())
	trialEvent := string(events.OrganizationEnterpriseTrialV1.EventType())
	eventType := event.GetEventType()
	if eventType != billingEvent && eventType != trialEvent {
		return nil
	}

	var payload events.AuditLogCreatedPayloadV1
	if err := json.Unmarshal(event.GetPayload(), &payload); err != nil {
		h.logger.ErrorContext(ctx, "dropping unreadable billing notification event", attr.SlogOutboxPublicID(eventID), attr.SlogError(err))
		return nil
	}

	// Activation and subscription loss share the billing event type, so the
	// audited action is what separates them.
	action := audit.Action(payload.Action)
	var schedule func(context.Context) error
	switch {
	case eventType == billingEvent && action == audit.ActionOrganizationPaygActivated:
		schedule = func(ctx context.Context) error {
			return h.schedulePaygActivated(ctx, eventID, organizationID)
		}
	case eventType == billingEvent && action == audit.ActionOrganizationPaygDeactivated:
		schedule = func(ctx context.Context) error {
			return h.scheduleAccessPaused(ctx, eventID, organizationID, AccessPausedSubscriptionLoss)
		}
	case eventType == trialEvent && action == audit.ActionOrganizationEnterpriseTrialDemoted:
		schedule = func(ctx context.Context) error {
			return h.scheduleAccessPaused(ctx, eventID, organizationID, AccessPausedTrialDemotion)
		}
	default:
		return nil
	}

	if payload.OrganizationID != organizationID || payload.SubjectID != organizationID || payload.SubjectType != "organization" {
		h.logger.ErrorContext(ctx, "dropping mismatched billing notification event",
			attr.SlogOrganizationID(organizationID),
			attr.SlogOutboxPublicID(eventID),
		)
		return nil
	}
	if h.scheduler == nil {
		return fmt.Errorf("billing email scheduler is unavailable")
	}
	return schedule(ctx)
}

func (h *EventHandler) schedulePaygActivated(ctx context.Context, eventID, organizationID string) error {
	if err := h.scheduler.SchedulePaygActivated(ctx, SendPaygActivatedInput{
		EventID:        eventID,
		OrganizationID: organizationID,
	}); err != nil {
		return fmt.Errorf("schedule PAYG activation email: %w", err)
	}
	return nil
}

func (h *EventHandler) scheduleAccessPaused(ctx context.Context, eventID, organizationID string, kind AccessPausedKind) error {
	if err := h.scheduler.ScheduleAccessPaused(ctx, SendAccessPausedInput{
		EventID:        eventID,
		OrganizationID: organizationID,
		Kind:           kind,
	}); err != nil {
		return fmt.Errorf("schedule access paused email: %w", err)
	}
	return nil
}
