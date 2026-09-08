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
)

type captureTrialConversionKeyReconcileScheduler struct {
	calls          atomic.Int32
	eventID        string
	organizationID string
	err            error
}

func (s *captureTrialConversionKeyReconcileScheduler) ScheduleEnterpriseTrialConversionKeyReconciliation(_ context.Context, eventID, organizationID string) error {
	s.calls.Add(1)
	s.eventID = eventID
	s.organizationID = organizationID
	return s.err
}

func enterpriseTrialConversionEvent(t *testing.T, eventID string) *webhooksv1.Event {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"organization_id": stripeWebhookOrganizationID,
		"action":          string(audit.ActionOrganizationEnterpriseTrialConverted),
		"subject_id":      stripeWebhookOrganizationID,
		"subject_type":    "organization",
		"metadata":        map[string]any{"conversion_source": "stripe_checkout"},
	})
	require.NoError(t, err)
	eventType := string(events.OrganizationEnterpriseTrialV1.EventType())
	organizationID := stripeWebhookOrganizationID
	createdAt := "2026-08-14T12:00:00Z"
	return webhooksv1.Event_builder{EventId: &eventID, OrganizationId: &organizationID, EventType: &eventType, Payload: payload, CreatedAt: &createdAt}.Build()
}

func TestEnterpriseTrialConversionKeyReconcileHandlerRedeliversSchedulingFailure(t *testing.T) {
	t.Parallel()
	scheduler := &captureTrialConversionKeyReconcileScheduler{err: errors.New("temporal unavailable")}
	handler := NewEnterpriseTrialConversionKeyReconcileHandler(testenv.NewLogger(t), scheduler)
	event := enterpriseTrialConversionEvent(t, "event_placeholder")
	metadata := gcp.MessageMetadata{ID: "message_placeholder"}

	require.ErrorContains(t, handler.Handle(t.Context(), event, metadata), "schedule enterprise trial conversion key reconciliation")
	scheduler.err = nil
	require.NoError(t, handler.Handle(t.Context(), event, metadata))
	require.EqualValues(t, 2, scheduler.calls.Load())
	require.Equal(t, "event_placeholder", scheduler.eventID)
	require.Equal(t, stripeWebhookOrganizationID, scheduler.organizationID)
}

func TestEnterpriseTrialConversionKeyReconcileHandlerFiltersAdminConversion(t *testing.T) {
	t.Parallel()
	scheduler := &captureTrialConversionKeyReconcileScheduler{}
	handler := NewEnterpriseTrialConversionKeyReconcileHandler(testenv.NewLogger(t), scheduler)
	event := enterpriseTrialConversionEvent(t, "event_placeholder")
	payload, err := json.Marshal(map[string]any{
		"organization_id": stripeWebhookOrganizationID,
		"action":          string(audit.ActionOrganizationEnterpriseTrialConverted),
		"subject_id":      stripeWebhookOrganizationID,
		"subject_type":    "organization",
		"metadata":        map[string]any{"conversion_source": "admin"},
	})
	require.NoError(t, err)
	event.SetPayload(payload)

	require.NoError(t, handler.Handle(t.Context(), event, gcp.MessageMetadata{}))
	require.Zero(t, scheduler.calls.Load())
}

func TestEnterpriseTrialConversionKeyReconcileHandlerFiltersOtherTrialEvents(t *testing.T) {
	t.Parallel()
	scheduler := &captureTrialConversionKeyReconcileScheduler{}
	handler := NewEnterpriseTrialConversionKeyReconcileHandler(testenv.NewLogger(t), scheduler)
	event := enterpriseTrialConversionEvent(t, "event_placeholder")
	payload, err := json.Marshal(map[string]any{
		"organization_id": stripeWebhookOrganizationID,
		"action":          string(audit.ActionOrganizationEnterpriseTrialRearmed),
		"subject_id":      stripeWebhookOrganizationID,
		"subject_type":    "organization",
	})
	require.NoError(t, err)
	event.SetPayload(payload)

	require.NoError(t, handler.Handle(t.Context(), event, gcp.MessageMetadata{}))
	require.Zero(t, scheduler.calls.Load())
}
