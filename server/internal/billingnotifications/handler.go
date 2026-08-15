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

type AccessPausedScheduler interface {
	ScheduleAccessPaused(context.Context, SendAccessPausedInput) error
}

type EventHandler struct {
	logger    *slog.Logger
	scheduler AccessPausedScheduler
}

func NewEventHandler(logger *slog.Logger, scheduler AccessPausedScheduler) *EventHandler {
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

	var kind AccessPausedKind
	switch event.GetEventType() {
	case string(events.OrganizationBillingV1.EventType()):
		kind = AccessPausedSubscriptionLoss
	case string(events.OrganizationEnterpriseTrialV1.EventType()):
		kind = AccessPausedTrialDemotion
	default:
		return nil
	}

	var payload events.AuditLogCreatedPayloadV1
	if err := json.Unmarshal(event.GetPayload(), &payload); err != nil {
		h.logger.ErrorContext(ctx, "dropping unreadable billing notification event", attr.SlogOutboxPublicID(eventID), attr.SlogError(err))
		return nil
	}
	expectedAction := audit.ActionOrganizationPaygDeactivated
	if kind == AccessPausedTrialDemotion {
		expectedAction = audit.ActionOrganizationEnterpriseTrialDemoted
	}
	if audit.Action(payload.Action) != expectedAction || payload.OrganizationID != organizationID || payload.SubjectID != organizationID || payload.SubjectType != "organization" {
		if audit.Action(payload.Action) == expectedAction {
			h.logger.ErrorContext(ctx, "dropping mismatched billing notification event",
				attr.SlogOrganizationID(organizationID),
				attr.SlogOutboxPublicID(eventID),
			)
		}
		return nil
	}
	if h.scheduler == nil {
		return fmt.Errorf("access paused scheduler is unavailable")
	}
	if err := h.scheduler.ScheduleAccessPaused(ctx, SendAccessPausedInput{
		EventID:        eventID,
		OrganizationID: organizationID,
		Kind:           kind,
	}); err != nil {
		return fmt.Errorf("schedule access paused email: %w", err)
	}
	return nil
}
