package slackapproval

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
)

// Scheduler runs the durable side of Slack notifications: one run per
// request that sends the DMs, then updates them once the request is decided
// or lapses.
type Scheduler interface {
	// StartNotify starts the run for a request. Starting it twice is a no-op.
	StartNotify(ctx context.Context, target Target) error

	// SignalDecided tells a request's run that it was decided. A request
	// without a run is left alone.
	SignalDecided(ctx context.Context, target Target) error
}

// EventHandler reads approval audit events from the outbox. Filing a request
// starts its Slack run, and a decision wakes it. The audit event commits with
// the change it records, so no request is missed and none is notified before
// it is pending.
type EventHandler struct {
	logger    *slog.Logger
	scheduler Scheduler
}

// NewEventHandler builds the handler.
func NewEventHandler(logger *slog.Logger, scheduler Scheduler) *EventHandler {
	return &EventHandler{
		logger:    logger.With(attr.SlogComponent("risk_approval_slack_events")),
		scheduler: scheduler,
	}
}

// Handle starts or wakes the Slack run for an approval event and ignores every
// other event on the topic.
func (h *EventHandler) Handle(ctx context.Context, event *webhooksv1.Event, _ gcp.MessageMetadata) error {
	if event == nil || event.GetEventType() != string(events.RiskPolicyV1.EventType()) {
		return nil
	}
	var payload events.AuditLogCreatedPayloadV1
	if err := json.Unmarshal(event.GetPayload(), &payload); err != nil {
		h.logger.ErrorContext(ctx, "dropping unreadable risk policy event", attr.SlogOutboxPublicID(event.GetEventId()), attr.SlogError(err))
		return nil
	}
	action := audit.Action(payload.Action)
	if action != audit.ActionRiskPolicyApprovalRequest && action != audit.ActionRiskPolicyApprovalApprove && action != audit.ActionRiskPolicyApprovalDeny {
		return nil
	}
	var snapshot audit.RiskPolicyApprovalSnapshot
	if err := json.Unmarshal(payload.AfterSnapshot, &snapshot); err != nil {
		h.logger.ErrorContext(ctx, "dropping approval event without a snapshot", attr.SlogOutboxPublicID(event.GetEventId()), attr.SlogError(err))
		return nil
	}
	approvalID, err := uuid.Parse(snapshot.ID)
	if err != nil || !payload.ProjectID.Valid || payload.OrganizationID == "" || payload.OrganizationID != event.GetOrganizationId() {
		h.logger.ErrorContext(ctx, "dropping mismatched approval event", attr.SlogOutboxPublicID(event.GetEventId()))
		return nil
	}
	target := Target{OrganizationID: payload.OrganizationID, ProjectID: payload.ProjectID.UUID, ApprovalID: approvalID}
	if action == audit.ActionRiskPolicyApprovalRequest {
		if err := h.scheduler.StartNotify(ctx, target); err != nil {
			return fmt.Errorf("start approval Slack notification: %w", err)
		}
		return nil
	}
	if err := h.scheduler.SignalDecided(ctx, target); err != nil {
		return fmt.Errorf("signal approval Slack notification: %w", err)
	}
	return nil
}
