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

type EnterpriseTrialConversionKeyReconcileScheduler interface {
	ScheduleEnterpriseTrialConversionKeyReconciliation(context.Context, string, string) error
}

type EnterpriseTrialConversionKeyReconcileHandler struct {
	logger    *slog.Logger
	scheduler EnterpriseTrialConversionKeyReconcileScheduler
}

func NewEnterpriseTrialConversionKeyReconcileHandler(logger *slog.Logger, scheduler EnterpriseTrialConversionKeyReconcileScheduler) *EnterpriseTrialConversionKeyReconcileHandler {
	return &EnterpriseTrialConversionKeyReconcileHandler{logger: logger.With(attr.SlogComponent("enterprise-trial-conversion-key-reconcile")), scheduler: scheduler}
}

func (h *EnterpriseTrialConversionKeyReconcileHandler) Handle(ctx context.Context, event *webhooksv1.Event, _ gcp.MessageMetadata) error {
	if event == nil {
		h.logger.ErrorContext(ctx, "dropping nil enterprise trial conversion key reconciliation event")
		return nil
	}
	eventID, organizationID := event.GetEventId(), event.GetOrganizationId()
	if eventID == "" || organizationID == "" || event.GetEventType() != string(events.OrganizationEnterpriseTrialV1.EventType()) {
		return nil
	}
	var payload events.AuditLogCreatedPayloadV1
	if err := json.Unmarshal(event.GetPayload(), &payload); err != nil {
		h.logger.ErrorContext(ctx, "dropping unreadable enterprise trial conversion key reconciliation event", attr.SlogError(err))
		return nil
	}
	if audit.Action(payload.Action) != audit.ActionOrganizationEnterpriseTrialConverted {
		return nil
	}
	var metadata struct {
		ConversionSource string `json:"conversion_source"`
	}
	if err := json.Unmarshal(payload.Metadata, &metadata); err != nil || metadata.ConversionSource != "stripe_checkout" {
		return nil
	}
	if payload.OrganizationID != organizationID || payload.SubjectID != organizationID || payload.SubjectType != "organization" {
		h.logger.ErrorContext(ctx, "dropping mismatched enterprise trial conversion key reconciliation event", attr.SlogOrganizationID(organizationID), attr.SlogOutboxPublicID(eventID))
		return nil
	}
	if h.scheduler == nil {
		return errors.New("enterprise trial conversion key reconciliation scheduler is unavailable")
	}
	if err := h.scheduler.ScheduleEnterpriseTrialConversionKeyReconciliation(ctx, eventID, organizationID); err != nil {
		return fmt.Errorf("schedule enterprise trial conversion key reconciliation: %w", err)
	}
	return nil
}
