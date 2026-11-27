package slackrequests

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

// Scheduler runs the durable side of the Slack DMs: one run per request that
// sends them, then updates them once the request is decided, withdrawn or
// lapses.
type Scheduler interface {
	// StartNotify starts the run for a request. Starting it twice is a no-op.
	StartNotify(ctx context.Context, target Target) error

	// SignalDecided tells a request's run that it is no longer pending. A
	// request without a run is left alone.
	SignalDecided(ctx context.Context, target Target) error
}

// EventHandler reads access request audit events from the outbox. A new
// request starts its Slack run, and a decision or withdrawal wakes it. The
// audit event commits with the change it records, so no request is missed
// and none is sent before it is stored.
type EventHandler struct {
	logger    *slog.Logger
	scheduler Scheduler
}

// NewEventHandler builds the handler.
func NewEventHandler(logger *slog.Logger, scheduler Scheduler) *EventHandler {
	return &EventHandler{
		logger:    logger.With(attr.SlogComponent("access_request_slack_events")),
		scheduler: scheduler,
	}
}

// Handle starts or wakes the Slack run for an access request event and
// ignores every other event on the topic.
func (h *EventHandler) Handle(ctx context.Context, event *webhooksv1.Event, _ gcp.MessageMetadata) error {
	if event == nil || event.GetEventType() != string(events.AccessRequestV1.EventType()) {
		return nil
	}
	var payload events.AuditLogCreatedPayloadV1
	if err := json.Unmarshal(event.GetPayload(), &payload); err != nil {
		h.logger.ErrorContext(ctx, "dropping unreadable access request event", attr.SlogOutboxPublicID(event.GetEventId()), attr.SlogError(err))
		return nil
	}
	action := audit.Action(payload.Action)
	switch action {
	case audit.ActionAccessRequestCreate, audit.ActionAccessRequestApprove, audit.ActionAccessRequestDeny, audit.ActionAccessRequestWithdraw:
	default:
		return nil
	}
	// The snapshot is the request's API view, which has no JSON tags, so its
	// keys are the Go field names.
	var snapshot struct {
		ID string `json:"ID"`
	}
	if err := json.Unmarshal(payload.AfterSnapshot, &snapshot); err != nil {
		h.logger.ErrorContext(ctx, "dropping access request event without a snapshot", attr.SlogOutboxPublicID(event.GetEventId()), attr.SlogError(err))
		return nil
	}
	requestID, err := uuid.Parse(snapshot.ID)
	if err != nil || payload.OrganizationID == "" || payload.OrganizationID != event.GetOrganizationId() {
		h.logger.ErrorContext(ctx, "dropping mismatched access request event", attr.SlogOutboxPublicID(event.GetEventId()))
		return nil
	}
	target := Target{OrganizationID: payload.OrganizationID, RequestID: requestID}
	if action == audit.ActionAccessRequestCreate {
		if err := h.scheduler.StartNotify(ctx, target); err != nil {
			return fmt.Errorf("start access request Slack notification: %w", err)
		}
		return nil
	}
	if err := h.scheduler.SignalDecided(ctx, target); err != nil {
		return fmt.Errorf("signal access request Slack notification: %w", err)
	}
	return nil
}
