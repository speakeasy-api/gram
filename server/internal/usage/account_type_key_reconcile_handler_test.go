package usage

import (
	"encoding/json"
	"errors"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAccountTypeKeyReconcileHandler(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		field string
		value any
		valid bool
	}{
		{name: "tier change", valid: true},
		{name: "same tier repair", field: "before_snapshot", value: map[string]string{"account_type": "enterprise"}, valid: true},
		{name: "missing operation", field: "metadata", value: map[string]string{}},
		{name: "wrong operation", field: "metadata", value: map[string]string{"operation": "conversion"}},
		{name: "wrong subject", field: "subject_id", value: "other"},
		{name: "wrong organization", field: "organization_id", value: "other"},
		{name: "wrong subject type", field: "subject_type", value: "project"},
		{name: "conversion action", field: "action", value: "organization:enterprise_trial_converted"},
		{name: "missing event id", field: "event_id"},
		{name: "wrong event type", field: "event_type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scheduler := &captureTrialConversionKeyReconcileScheduler{err: errors.New("temporal unavailable")}
			handler := NewEnterpriseTrialConversionKeyReconcileHandler(testenv.NewLogger(t), scheduler)
			event := enterpriseTrialConversionEvent(t, "event_placeholder")
			event.SetEventType("audit_log.organization_account_type_event_v1")
			payload := map[string]any{"organization_id": stripeWebhookOrganizationID, "subject_id": stripeWebhookOrganizationID, "subject_type": "organization", "action": "organization:account_type_changed", "metadata": map[string]string{"operation": "account_type_change"}, "before_snapshot": map[string]string{"account_type": "free"}, "after_snapshot": map[string]string{"account_type": "enterprise"}}
			if tc.field != "" {
				payload[tc.field] = tc.value
			}
			if tc.field == "event_id" {
				event.SetEventId("")
			}
			if tc.field == "event_type" {
				event.SetEventType("audit_log.organization_enterprise_trial_event_v1")
			}
			data, err := json.Marshal(payload)
			require.NoError(t, err)
			event.SetPayload(data)
			err = handler.Handle(t.Context(), event, gcp.MessageMetadata{})
			if !tc.valid {
				require.NoError(t, err)
				require.Zero(t, scheduler.calls.Load())
				return
			}
			require.ErrorContains(t, err, "temporal unavailable")
			scheduler.err = nil
			require.NoError(t, handler.Handle(t.Context(), event, gcp.MessageMetadata{}))
			require.EqualValues(t, 2, scheduler.calls.Load())
			require.Equal(t, "event_placeholder", scheduler.eventID)
			require.Equal(t, stripeWebhookOrganizationID, scheduler.organizationID)
		})
	}
}
