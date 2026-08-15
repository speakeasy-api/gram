package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
)

// PaygKeyRefreshHandler schedules desired-state reconciliation after a
// committed PAYG billing transition.
type PaygKeyRefreshHandler struct {
	logger    *slog.Logger
	refresher PaygKeyRefreshScheduler
}

// PaygKeyRefreshScheduler starts the idempotent reconciliation associated with
// one committed outbox event.
type PaygKeyRefreshScheduler interface {
	SchedulePaygOpenRouterChatKeyReconciliation(context.Context, string, string) error
}

func NewPaygKeyRefreshHandler(logger *slog.Logger, refresher PaygKeyRefreshScheduler) *PaygKeyRefreshHandler {
	return &PaygKeyRefreshHandler{
		logger:    logger.With(attr.SlogComponent("payg-key-refresh")),
		refresher: refresher,
	}
}

func (h *PaygKeyRefreshHandler) Handle(ctx context.Context, event *webhooksv1.Event, _ gcp.MessageMetadata) error {
	if event == nil {
		h.logger.ErrorContext(ctx, "dropping nil PAYG key refresh event")
		return nil
	}

	eventID := event.GetEventId()
	organizationID := event.GetOrganizationId()
	if eventID == "" || organizationID == "" || event.GetEventType() != string(events.OrganizationBillingV1.EventType()) {
		h.logger.ErrorContext(ctx, "dropping invalid PAYG key refresh event",
			attr.SlogOrganizationID(organizationID),
			attr.SlogOutboxPublicID(eventID),
			attr.SlogEvent(event.GetEventType()),
		)
		return nil
	}

	var payload events.AuditLogCreatedPayloadV1
	if err := json.Unmarshal(event.GetPayload(), &payload); err != nil {
		h.logger.ErrorContext(ctx, "dropping unreadable PAYG key refresh event",
			attr.SlogOrganizationID(organizationID),
			attr.SlogOutboxPublicID(eventID),
			attr.SlogError(err),
		)
		return nil
	}
	switch audit.Action(payload.Action) {
	case audit.ActionOrganizationPaygActivated, audit.ActionOrganizationPaygDeactivated:
	default:
		return nil
	}
	if payload.OrganizationID != organizationID || payload.SubjectID != organizationID || payload.SubjectType != "organization" {
		h.logger.ErrorContext(ctx, "dropping mismatched PAYG key refresh event",
			attr.SlogOrganizationID(organizationID),
			attr.SlogOutboxPublicID(eventID),
		)
		return nil
	}
	if h.refresher == nil {
		return errors.New("openrouter key refresh scheduler is unavailable")
	}

	if err := h.refresher.SchedulePaygOpenRouterChatKeyReconciliation(ctx, eventID, organizationID); err != nil {
		return fmt.Errorf("schedule PAYG OpenRouter chat key reconciliation: %w", err)
	}

	return nil
}
