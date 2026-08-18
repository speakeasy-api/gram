package usage

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type capturePaygKeyRefreshScheduler struct {
	calls          atomic.Int32
	eventID        string
	organizationID string
	desiredState   openrouter.KeyDesiredState
	err            error
}

func (c *capturePaygKeyRefreshScheduler) SchedulePaygOpenRouterChatKeyReconciliation(_ context.Context, eventID, organizationID string, desiredState openrouter.KeyDesiredState) error {
	c.calls.Add(1)
	c.eventID = eventID
	c.organizationID = organizationID
	c.desiredState = desiredState
	return c.err
}

func paygKeyRefreshEvent(t *testing.T) *webhooksv1.Event {
	t.Helper()
	return paygKeyRefreshEventForAction(t, "outbox_event_placeholder", audit.ActionOrganizationPaygActivated)
}

func paygKeyRefreshEventForAction(t *testing.T, eventID string, action audit.Action) *webhooksv1.Event {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		"organization_id": stripeWebhookOrganizationID,
		"action":          string(action),
		"subject_id":      stripeWebhookOrganizationID,
		"subject_type":    "organization",
	})
	require.NoError(t, err)

	eventType := string(events.OrganizationBillingV1.EventType())
	organizationID := stripeWebhookOrganizationID
	createdAt := "2026-08-14T12:00:00Z"
	return webhooksv1.Event_builder{
		EventId:        &eventID,
		OrganizationId: &organizationID,
		EventType:      &eventType,
		Payload:        payload,
		CreatedAt:      &createdAt,
	}.Build()
}

func TestPaygKeyRefreshHandlerRetriesSchedulingFailure(t *testing.T) {
	t.Parallel()

	refresher := &capturePaygKeyRefreshScheduler{err: errors.New("scheduler unavailable")}
	handler := NewPaygKeyRefreshHandler(testenv.NewLogger(t), refresher)
	metadata := gcp.MessageMetadata{ID: "message_placeholder", Attributes: nil, DeliveryAttempt: nil}
	event := paygKeyRefreshEvent(t)

	err := handler.Handle(t.Context(), event, metadata)
	require.ErrorContains(t, err, "schedule PAYG OpenRouter chat key reconciliation")
	refresher.err = nil
	require.NoError(t, handler.Handle(t.Context(), event, metadata))

	require.EqualValues(t, 2, refresher.calls.Load())
	require.Equal(t, "outbox_event_placeholder", refresher.eventID)
	require.Equal(t, stripeWebhookOrganizationID, refresher.organizationID)
	require.Equal(t, openrouter.KeyDesiredStateEnabled, refresher.desiredState)
}

func TestPaygKeyRefreshHandlerSchedulesReverseBillingTransitionsAsWakeups(t *testing.T) {
	t.Parallel()

	refresher := &capturePaygKeyRefreshScheduler{}
	handler := NewPaygKeyRefreshHandler(testenv.NewLogger(t), refresher)
	metadata := gcp.MessageMetadata{ID: "message_placeholder", Attributes: nil, DeliveryAttempt: nil}

	deactivated := paygKeyRefreshEventForAction(t, "outbox_deactivated", audit.ActionOrganizationPaygDeactivated)
	activated := paygKeyRefreshEventForAction(t, "outbox_activated", audit.ActionOrganizationPaygActivated)
	require.NoError(t, handler.Handle(t.Context(), deactivated, metadata))
	require.NoError(t, handler.Handle(t.Context(), activated, metadata))

	require.EqualValues(t, 2, refresher.calls.Load())
	require.Equal(t, "outbox_activated", refresher.eventID)
	require.Equal(t, stripeWebhookOrganizationID, refresher.organizationID)
	require.Equal(t, openrouter.KeyDesiredStateEnabled, refresher.desiredState)
}

func TestPaygKeyRefreshHandlerDropsUnrelatedBillingAction(t *testing.T) {
	t.Parallel()

	event := paygKeyRefreshEvent(t)
	payload, err := json.Marshal(map[string]any{
		"organization_id": stripeWebhookOrganizationID,
		"action":          "organization:other_billing_change",
		"subject_id":      stripeWebhookOrganizationID,
		"subject_type":    "organization",
	})
	require.NoError(t, err)
	event.SetPayload(payload)

	refresher := &capturePaygKeyRefreshScheduler{}
	handler := NewPaygKeyRefreshHandler(testenv.NewLogger(t), refresher)
	require.NoError(t, handler.Handle(t.Context(), event, gcp.MessageMetadata{ID: "message_placeholder", Attributes: nil, DeliveryAttempt: nil}))
	require.Zero(t, refresher.calls.Load())
}
