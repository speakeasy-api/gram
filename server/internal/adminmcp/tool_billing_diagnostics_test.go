package adminmcp

import (
	"context"
	"encoding/json"

	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

type recordingBillingDiagnostics struct {
	recordingOrganizationReader
	keyInput     *gen.GetInferenceKeysPayload
	historyInput *gen.GetInferenceSpendHistoryPayload
	meterInput   *gen.GetMeterUsagePayload
	spendInput   *gen.GetSpendBreakdownPayload
	keys         []*gen.AdminInferenceKey
	months       []*gen.AdminInferenceSpendMonth
	meter        *gen.AdminMeterUsageResponse
	spend        *gen.AdminSpendBreakdownResponse
}

func (r *recordingBillingDiagnostics) GetInferenceKeys(_ context.Context, input *gen.GetInferenceKeysPayload) ([]*gen.AdminInferenceKey, error) {
	r.keyInput = input
	return r.keys, nil
}
func (r *recordingBillingDiagnostics) GetInferenceSpendHistory(_ context.Context, input *gen.GetInferenceSpendHistoryPayload) ([]*gen.AdminInferenceSpendMonth, error) {
	r.historyInput = input
	return r.months, nil
}
func (r *recordingBillingDiagnostics) GetMeterUsage(_ context.Context, input *gen.GetMeterUsagePayload) (*gen.AdminMeterUsageResponse, error) {
	r.meterInput = input
	return r.meter, nil
}
func (r *recordingBillingDiagnostics) GetSpendBreakdown(_ context.Context, input *gen.GetSpendBreakdownPayload) (*gen.AdminSpendBreakdownResponse, error) {
	r.spendInput = input
	return r.spend, nil
}

func TestBillingDiagnosticsBoundAndRedact(t *testing.T) {
	t.Parallel()
	reads := &recordingBillingDiagnostics{recordingOrganizationReader: recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}},
		keys:   []*gen.AdminInferenceKey{{KeyType: "chat", MonthlyCredits: 100}},
		months: []*gen.AdminInferenceSpendMonth{{PeriodStart: "2026-01-01", PeriodEnd: "2026-02-01", SpendUsd: "1.23"}},
		meter:  &gen.AdminMeterUsageResponse{Family: "mcp_bandwidth", Window: &gen.MeterUsageWindow{From: "2026-01-01", To: "2026-02-01"}, Total: "500", Buckets: []*gen.AdminMeterUsageBucket{{Total: "private-bucket"}}},
		spend:  &gen.AdminSpendBreakdownResponse{Window: &gen.MeterUsageWindow{From: "2026-01-01", To: "2026-02-01"}, TotalCostUsd: "2.34", Products: []*gen.SpendProduct{{Label: "private-product"}}},
	}
	for _, tc := range []struct{ name, args, omitted string }{
		{"get_organization_inference_key_state", `{"organization_id":"org-a"}`, "provider_key"},
		{"get_organization_inference_spend_history", `{"organization_id":"org-a"}`, "provider_key"},
		{"get_organization_meter_usage", `{"organization_id":"org-a","family":"mcp_bandwidth"}`, "private-bucket"},
		{"get_organization_spend_breakdown_total", `{"organization_id":"org-a"}`, "private-product"},
	} {
		body, data := issuerToolCall(t, reads, tc.name, tc.args, true)
		require.NotContains(t, body, `"isError":true`)
		require.NotContains(t, body, tc.omitted)
		require.NotEmpty(t, data)
		require.True(t, json.Valid(data))
	}
	require.Equal(t, "org-a", reads.keyInput.OrganizationID)
	require.Nil(t, reads.keyInput.AdminSessionToken)
	require.Equal(t, "org-a", reads.historyInput.OrganizationID)
	require.Nil(t, reads.historyInput.AdminSessionToken)
	require.Equal(t, "mcp_bandwidth", reads.meterInput.Family)
	require.Nil(t, reads.meterInput.From)
	require.Nil(t, reads.meterInput.To)
	require.Equal(t, "org-a", reads.spendInput.OrganizationID)
	require.Nil(t, reads.spendInput.From)
	require.Nil(t, reads.spendInput.To)
	reads.keyInput = nil
	body, _ := issuerToolCall(t, reads, "get_organization_inference_key_state", `{"organization_id":"org-a"}`, false)
	require.Contains(t, body, `"isError":true`)
	require.Nil(t, reads.keyInput)
	body, _ = issuerToolCall(t, reads, "get_organization_inference_key_state", `{"organization_id":"not-the-id"}`, true)
	require.Contains(t, body, `"isError":true`)
	require.Nil(t, reads.keyInput)
	reads.meterInput = nil
	body, _ = issuerToolCall(t, reads, "get_organization_meter_usage", `{"organization_id":"org-a","family":"unknown"}`, true)
	require.Contains(t, body, `"isError":true`)
	require.Nil(t, reads.meterInput)
	reads.keys = make([]*gen.AdminInferenceKey, 5)
	body, _ = issuerToolCall(t, reads, "get_organization_inference_key_state", `{"organization_id":"org-a"}`, true)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, "private-product")
}
