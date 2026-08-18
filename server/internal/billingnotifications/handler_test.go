package billingnotifications

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type captureBillingEmailScheduler struct {
	inputs    []SendAccessPausedInput
	activated []SendPaygActivatedInput
}

func (s *captureBillingEmailScheduler) ScheduleAccessPaused(_ context.Context, input SendAccessPausedInput) error {
	s.inputs = append(s.inputs, input)
	return nil
}

func (s *captureBillingEmailScheduler) SchedulePaygActivated(_ context.Context, input SendPaygActivatedInput) error {
	s.activated = append(s.activated, input)
	return nil
}

func billingNotificationEvent(t *testing.T, eventType string, action audit.Action) *webhooksv1.Event {
	t.Helper()
	organizationID := "<ORGANIZATION_ID>"
	eventID := "<EVENT_ID>"
	createdAt := "2026-08-15T12:00:00Z"
	payload, err := json.Marshal(map[string]string{
		"organization_id": organizationID,
		"action":          string(action),
		"subject_id":      organizationID,
		"subject_type":    "organization",
	})
	require.NoError(t, err)
	return webhooksv1.Event_builder{
		EventId:        &eventID,
		OrganizationId: &organizationID,
		EventType:      &eventType,
		Payload:        payload,
		CreatedAt:      &createdAt,
	}.Build()
}

func TestEventHandlerRoutesBillingTransitionsByAuditedAction(t *testing.T) {
	t.Parallel()
	scheduler := &captureBillingEmailScheduler{}
	handler := NewEventHandler(testenv.NewLogger(t), scheduler)
	metadata := gcp.MessageMetadata{ID: "<MESSAGE_ID>", Attributes: nil, DeliveryAttempt: nil}

	require.NoError(t, handler.Handle(t.Context(), billingNotificationEvent(t, string(events.OrganizationBillingV1.EventType()), audit.ActionOrganizationPaygDeactivated), metadata))
	require.NoError(t, handler.Handle(t.Context(), billingNotificationEvent(t, string(events.OrganizationEnterpriseTrialV1.EventType()), audit.ActionOrganizationEnterpriseTrialDemoted), metadata))
	require.Len(t, scheduler.inputs, 2)
	require.Equal(t, AccessPausedSubscriptionLoss, scheduler.inputs[0].Kind)
	require.Equal(t, AccessPausedTrialDemotion, scheduler.inputs[1].Kind)

	require.NoError(t, handler.Handle(t.Context(), billingNotificationEvent(t, string(events.OrganizationBillingV1.EventType()), audit.ActionOrganizationPaygActivated), metadata))
	require.Len(t, scheduler.inputs, 2)
	require.Len(t, scheduler.activated, 1)
	require.Equal(t, "<EVENT_ID>", scheduler.activated[0].EventID)
	require.Equal(t, "<ORGANIZATION_ID>", scheduler.activated[0].OrganizationID)

	require.NoError(t, handler.Handle(t.Context(), billingNotificationEvent(t, string(events.OrganizationBillingV1.EventType()), audit.ActionOrganizationWebhooksEnabled), metadata))
	require.Len(t, scheduler.inputs, 2)
	require.Len(t, scheduler.activated, 1)
}

func TestEventHandlerDropsMismatchedEnvelope(t *testing.T) {
	t.Parallel()
	scheduler := &captureBillingEmailScheduler{}
	handler := NewEventHandler(testenv.NewLogger(t), scheduler)
	event := billingNotificationEvent(t, string(events.OrganizationBillingV1.EventType()), audit.ActionOrganizationPaygDeactivated)
	payload, err := json.Marshal(map[string]string{
		"organization_id": "<OTHER_ORGANIZATION_ID>",
		"action":          string(audit.ActionOrganizationPaygDeactivated),
		"subject_id":      "<OTHER_ORGANIZATION_ID>",
		"subject_type":    "organization",
	})
	require.NoError(t, err)
	event.SetPayload(payload)

	require.NoError(t, handler.Handle(t.Context(), event, gcp.MessageMetadata{ID: "<MESSAGE_ID>", Attributes: nil, DeliveryAttempt: nil}))
	require.Empty(t, scheduler.inputs)
	require.Empty(t, scheduler.activated)
}
