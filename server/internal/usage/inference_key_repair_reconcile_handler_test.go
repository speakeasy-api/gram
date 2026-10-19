package usage

import (
	"encoding/json"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestInferenceKeyRepairReconcileHandler(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		action, operation string
		valid             bool
	}{
		{"organization:inference_key_repaired", "inference_key_repair", true},
		{"organization:inference_key_repaired", "account_type_change", false},
		{"organization:account_type_changed", "inference_key_repair", false},
		{"organization:inference_key_repaired", "", false},
	} {
		t.Run(tc.action+tc.operation, func(t *testing.T) {
			t.Parallel()
			scheduler := &captureTrialConversionKeyReconcileScheduler{}
			handler := NewEnterpriseTrialConversionKeyReconcileHandler(testenv.NewLogger(t), scheduler)
			event := enterpriseTrialConversionEvent(t, "repair_event")
			event.SetEventType("audit_log.organization_account_type_event_v1")
			payload, err := json.Marshal(map[string]any{"organization_id": stripeWebhookOrganizationID, "subject_id": stripeWebhookOrganizationID, "subject_type": "organization", "action": tc.action, "metadata": map[string]string{"operation": tc.operation}})
			require.NoError(t, err)
			event.SetPayload(payload)
			require.NoError(t, handler.Handle(t.Context(), event, gcp.MessageMetadata{}))
			if tc.valid {
				require.EqualValues(t, 1, scheduler.calls.Load())
			} else {
				require.Zero(t, scheduler.calls.Load())
			}
		})
	}
}
