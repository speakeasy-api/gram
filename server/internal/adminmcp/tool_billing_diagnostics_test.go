package adminmcp

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
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

// requireJSONKeys proves a projection's exact field set; decoding into a struct alone ignores extra fields.
func requireJSONKeys(t *testing.T, data json.RawMessage, keys ...string) {
	t.Helper()
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &fields))
	require.ElementsMatch(t, keys, slices.Collect(maps.Keys(fields)))
}

func TestBillingDiagnosticsBoundAndRedact(t *testing.T) {
	t.Parallel()
	window := &gen.MeterUsageWindow{From: "2026-01-01", To: "2026-02-01"}
	privateCycles := []*gen.MeterUsageWindow{{From: "private-cycle-start", To: "private-cycle-end"}}
	reads := &recordingBillingDiagnostics{recordingOrganizationReader: recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}},
		keys: []*gen.AdminInferenceKey{
			{KeyType: "chat", MonthlyCredits: 100},
			{KeyType: "agents", CreditsUsed: 1.5, MonthlyCredits: 50, Disabled: true, DisableCauses: []string{"monthly_limit"}, DisableCausesClassified: true},
		},
		months: []*gen.AdminInferenceSpendMonth{{PeriodStart: "2026-01-01", PeriodEnd: "2026-02-01", SpendUsd: "1.23"}},
		meter: &gen.AdminMeterUsageResponse{Family: "mcp_bandwidth", Window: window, BillingCycles: privateCycles, Unit: "bytes", Total: "500",
			Buckets: []*gen.AdminMeterUsageBucket{{Total: "private-bucket"}}, QueriedAt: "2026-01-15T00:00:00Z", MeasurementMethod: "private-method"},
		spend: &gen.AdminSpendBreakdownResponse{Window: window, BillingCycles: privateCycles, Currency: "USD", PricingBasis: "payg_list",
			QueriedAt: "2026-01-15T00:00:00Z", TotalCostUsd: "2.34", Products: []*gen.SpendProduct{{Label: "private-product"}}},
	}

	body, data := issuerToolCall(t, reads, "get_organization_inference_key_state", `{"organization_id":"org-a"}`, true)
	require.NotContains(t, body, `"isError":true`)
	var keys InferenceKeyStates
	require.NoError(t, json.Unmarshal(data, &keys))
	require.Equal(t, InferenceKeyStates{OrganizationID: "org-a", Keys: []InferenceKeyState{
		{KeyType: "chat", MonthlyCredits: 100, DisableCauses: []string{}},
		{KeyType: "agents", CreditsUsed: 1.5, MonthlyCredits: 50, Disabled: true, DisableCauses: []string{"monthly_limit"}, DisableCausesClassified: true},
	}}, keys)
	var rawKeys struct {
		Keys []json.RawMessage `json:"keys"`
	}
	require.NoError(t, json.Unmarshal(data, &rawKeys))
	requireJSONKeys(t, data, "organization_id", "keys")
	requireJSONKeys(t, rawKeys.Keys[0], "key_type", "credits_used", "monthly_credits", "disabled", "disable_causes", "disable_causes_classified")
	var legacyKey struct {
		DisableCauses json.RawMessage `json:"disable_causes"`
	}
	require.NoError(t, json.Unmarshal(rawKeys.Keys[0], &legacyKey))
	require.JSONEq(t, `[]`, string(legacyKey.DisableCauses))

	body, data = issuerToolCall(t, reads, "get_organization_inference_spend_history", `{"organization_id":"org-a"}`, true)
	require.NotContains(t, body, `"isError":true`)
	var history InferenceSpendHistory
	require.NoError(t, json.Unmarshal(data, &history))
	require.Equal(t, InferenceSpendHistory{OrganizationID: "org-a", Months: []InferenceSpendMonth{{PeriodStart: "2026-01-01", PeriodEnd: "2026-02-01", SpendUSD: "1.23"}}}, history)
	var rawMonths struct {
		Months []json.RawMessage `json:"months"`
	}
	require.NoError(t, json.Unmarshal(data, &rawMonths))
	requireJSONKeys(t, data, "organization_id", "months")
	requireJSONKeys(t, rawMonths.Months[0], "period_start", "period_end", "spend_usd")

	body, data = issuerToolCall(t, reads, "get_organization_meter_usage", `{"organization_id":"org-a","family":"mcp_bandwidth"}`, true)
	require.NotContains(t, body, `"isError":true`)
	var meter MeterUsageSummary
	require.NoError(t, json.Unmarshal(data, &meter))
	require.Equal(t, MeterUsageSummary{OrganizationID: "org-a", Family: "mcp_bandwidth", WindowFrom: "2026-01-01", WindowTo: "2026-02-01", Unit: "bytes", Total: "500", QueriedAt: "2026-01-15T00:00:00Z"}, meter)
	requireJSONKeys(t, data, "organization_id", "family", "window_from", "window_to", "unit", "total", "queried_at")
	for _, omitted := range []string{"private-bucket", "private-cycle-start", "private-method"} {
		require.NotContains(t, body, omitted)
	}

	body, data = issuerToolCall(t, reads, "get_organization_spend_breakdown_total", `{"organization_id":"org-a"}`, true)
	require.NotContains(t, body, `"isError":true`)
	var spend SpendBreakdownSummary
	require.NoError(t, json.Unmarshal(data, &spend))
	require.Equal(t, SpendBreakdownSummary{OrganizationID: "org-a", WindowFrom: "2026-01-01", WindowTo: "2026-02-01", Currency: "USD", PricingBasis: "payg_list", TotalCostUSD: "2.34", QueriedAt: "2026-01-15T00:00:00Z"}, spend)
	requireJSONKeys(t, data, "organization_id", "window_from", "window_to", "currency", "pricing_basis", "total_cost_usd", "queried_at")
	for _, omitted := range []string{"private-product", "private-cycle-start"} {
		require.NotContains(t, body, omitted)
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
	body, _ = issuerToolCall(t, reads, "get_organization_inference_key_state", `{"organization_id":"org-a"}`, false)
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
}
