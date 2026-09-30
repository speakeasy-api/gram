package adminmcp

import (
	"context"
	"encoding/json"

	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

type recordingBillingDetail struct {
	recordingOrganizationReader
	subscriptionInput *gen.GetStripeSubscriptionPayload
	meterInput        *gen.GetMeterUsagePayload
	spendInput        *gen.GetSpendBreakdownPayload
	subscription      *gen.AdminStripeSubscription
	meter             *gen.AdminMeterUsageResponse
	spend             *gen.AdminSpendBreakdownResponse
	billingErr        error
}

func (r *recordingBillingDetail) GetStripeSubscription(_ context.Context, input *gen.GetStripeSubscriptionPayload) (*gen.AdminStripeSubscription, error) {
	r.subscriptionInput = input
	return r.subscription, r.billingErr
}

func (r *recordingBillingDetail) GetMeterUsage(_ context.Context, input *gen.GetMeterUsagePayload) (*gen.AdminMeterUsageResponse, error) {
	r.meterInput = input
	return r.meter, r.billingErr
}

func (r *recordingBillingDetail) GetSpendBreakdown(_ context.Context, input *gen.GetSpendBreakdownPayload) (*gen.AdminSpendBreakdownResponse, error) {
	r.spendInput = input
	return r.spend, r.billingErr
}

func TestBillingStatusUsesStoredAssociationAndRedactsProviderData(t *testing.T) {
	t.Parallel()
	start, end := "2026-08-01T00:00:00Z", "2026-09-01T00:00:00Z"
	privateID := "cus_private_fixture"
	reads := &recordingBillingDetail{
		recordingOrganizationReader: recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-fixture", StripeCustomerID: &privateID, StripeSubscriptionID: &privateID}},
		subscription:                &gen.AdminStripeSubscription{Status: "past_due", CurrentPeriodStart: start, CurrentPeriodEnd: end, PaymentFailed: true},
	}
	body, data := issuerToolCall(t, reads, "get_organization_billing_status", `{"organization_id":"org-fixture"}`, true)
	require.NotContains(t, body, privateID)

	require.Equal(t, "org-fixture", reads.subscriptionInput.OrganizationID)
	require.Nil(t, reads.subscriptionInput.AdminSessionToken)
	var status BillingStatusDetail
	require.NoError(t, json.Unmarshal(data, &status))
	require.Equal(t, BillingStatusDetail{OrganizationID: "org-fixture", HasStripeCustomer: true, HasStripeSubscription: true, SubscriptionStatus: "past_due", CurrentPeriodStart: start, CurrentPeriodEnd: end, PaymentFailed: true}, status)
	requireJSONKeys(t, data, "organization_id", "has_stripe_customer", "has_stripe_subscription", "subscription_status", "current_period_start", "current_period_end", "cancel_at_period_end", "payment_failed")

	reads.subscriptionInput = nil
	reads.org = &gen.AdminOrganization{ID: "org-fixture"}
	body, data = issuerToolCall(t, reads, "get_organization_billing_status", `{"organization_id":"org-fixture"}`, true)
	require.NotContains(t, body, `"isError":true`)
	require.Nil(t, reads.subscriptionInput)
	require.NoError(t, json.Unmarshal(data, &status))
	require.False(t, status.HasStripeCustomer)
	require.False(t, status.HasStripeSubscription)
	require.Equal(t, "not_subscribed", status.SubscriptionStatus)
}

func TestBillingDetailRequiresVerifiedStaffAndHidesDependencyErrors(t *testing.T) {
	t.Parallel()
	reads := &recordingBillingDetail{recordingOrganizationReader: recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-fixture", StripeSubscriptionID: new("stored-association")}}, billingErr: context.DeadlineExceeded}
	body, _ := issuerToolCall(t, reads, "get_organization_billing_status", `{"organization_id":"org-fixture"}`, false)
	require.Contains(t, body, `"isError":true`)
	require.Nil(t, reads.subscriptionInput)
	body, _ = issuerToolCall(t, reads, "get_organization_billing_status", `{"organization_id":"org-fixture"}`, true)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, "context deadline exceeded")
	require.NotContains(t, body, "stored-association")
}

func TestMeterUsageDetailValidatesWindowAndBoundsBuckets(t *testing.T) {
	t.Parallel()
	from, to := "2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z"
	reads := &recordingBillingDetail{
		recordingOrganizationReader: recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-fixture"}},
		meter:                       &gen.AdminMeterUsageResponse{Family: "mcp_bandwidth", Window: &gen.MeterUsageWindow{From: from, To: to}, Unit: "bytes", Total: "12", QueriedAt: "2026-02-01T00:00:00Z", Buckets: []*gen.AdminMeterUsageBucket{{From: from, To: "2026-01-02T00:00:00Z", Total: "12"}}},
	}
	body, data := issuerToolCall(t, reads, "get_organization_meter_usage_detail", `{"organization_id":"org-fixture","family":"mcp_bandwidth","from":"2026-01-01T00:00:00Z","to":"2026-02-01T00:00:00Z"}`, true)
	require.NotContains(t, body, `"isError":true`)
	require.Equal(t, "org-fixture", reads.meterInput.OrganizationID)
	require.Equal(t, "mcp_bandwidth", reads.meterInput.Family)
	require.Equal(t, &from, reads.meterInput.From)
	require.Equal(t, &to, reads.meterInput.To)
	require.Nil(t, reads.meterInput.AdminSessionToken)
	var usage OrganizationMeterUsageDetail
	require.NoError(t, json.Unmarshal(data, &usage))
	require.Equal(t, []MeterUsageBucketDetail{{From: from, To: "2026-01-02T00:00:00Z", Total: "12"}}, usage.Buckets)
	requireJSONKeys(t, data, "organization_id", "family", "window_from", "window_to", "unit", "total", "buckets", "queried_at")

	reads.meterInput = nil
	for _, args := range []string{
		`{"organization_id":"org-fixture","family":"unknown"}`,
		`{"organization_id":"org-fixture","family":"mcp_bandwidth","from":"2026-01-01T00:00:00Z"}`,
		`{"organization_id":"org-fixture","family":"mcp_bandwidth","from":"2026-01-01T00:00:00Z","to":"2026-05-01T00:00:00Z"}`,
		`{"organization_id":"org-fixture","family":"mcp_bandwidth","from":"2026-01-01T12:00:00Z","to":"2026-01-02T00:00:00Z"}`,
		`{"organization_id":"org-fixture","family":"mcp_bandwidth","from":"2026-01-01T00:00:00.001Z","to":"2026-01-02T00:00:00Z"}`,
	} {
		body, _ = issuerToolCall(t, reads, "get_organization_meter_usage_detail", args, true)
		require.Contains(t, body, `"isError":true`)
		require.Nil(t, reads.meterInput)
	}
}

func TestBillingDetailRejectsSwappedOrganization(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, args string }{
		{"get_organization_billing_status", `{"organization_id":"org-a"}`},
		{"get_organization_spend_breakdown", `{"organization_id":"org-a"}`},
		{"get_organization_meter_usage_detail", `{"organization_id":"org-a","family":"mcp_bandwidth"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reads := &recordingBillingDetail{recordingOrganizationReader: recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-b", Name: "Private Other Account"}}}
			body, _ := issuerToolCall(t, reads, tc.name, tc.args, true)
			require.Contains(t, body, `"isError":true`)
			require.NotContains(t, body, "Private Other Account")
			require.Nil(t, reads.subscriptionInput)
			require.Nil(t, reads.meterInput)
			require.Nil(t, reads.spendInput)
		})
	}
}

func TestMeterUsageDetailRejectsOversizedBucketSeries(t *testing.T) {
	t.Parallel()
	reads := &recordingBillingDetail{
		recordingOrganizationReader: recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}},
		meter:                       &gen.AdminMeterUsageResponse{Family: "mcp_bandwidth", Window: &gen.MeterUsageWindow{}, Buckets: make([]*gen.AdminMeterUsageBucket, maxBillingDetailBuckets+1)},
	}
	body, _ := issuerToolCall(t, reads, "get_organization_meter_usage_detail", `{"organization_id":"org-a","family":"mcp_bandwidth"}`, true)
	require.Contains(t, body, `"isError":true`)
}

func TestBillingDetailsAppearInContext(t *testing.T) {
	t.Parallel()
	reads := &recordingBillingDetail{}
	body, data := issuerToolCall(t, reads, "get_admin_context", `{}`, true)
	require.NotContains(t, body, `"isError":true`)
	var out AdminContext
	require.NoError(t, json.Unmarshal(data, &out))
	require.Contains(t, out.Workflows, "inspect organization billing status and bounded product or daily usage details")
}

func TestSpendBreakdownBoundsProductsAndDailyBucketsAreOptIn(t *testing.T) {
	t.Parallel()
	from, to := "2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z"
	privateID := "product-private-id"
	reads := &recordingBillingDetail{
		recordingOrganizationReader: recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-fixture"}},
		spend: &gen.AdminSpendBreakdownResponse{
			Window: &gen.MeterUsageWindow{From: from, To: to}, Currency: "USD", PricingBasis: "current_payg_list_price", TotalCostUsd: "0.24", QueriedAt: to,
			Products: []*gen.SpendProduct{{ID: privateID, Label: "Meter One", Unit: "events", Quantity: "12", RateQuantity: "1000", RateUsd: "0.02", CostUsd: "0.24", Buckets: []*gen.SpendBucket{{From: from, To: "2026-01-02T00:00:00Z", Quantity: "12", CostUsd: "0.24"}}}},
		},
	}
	body, data := issuerToolCall(t, reads, "get_organization_spend_breakdown", `{"organization_id":"org-fixture","from":"2026-01-01T00:00:00Z","to":"2026-02-01T00:00:00Z"}`, true)
	require.NotContains(t, body, privateID)
	require.Nil(t, reads.spendInput.AdminSessionToken)
	require.Equal(t, &from, reads.spendInput.From)
	require.Equal(t, &to, reads.spendInput.To)
	var spend OrganizationSpendBreakdown
	require.NoError(t, json.Unmarshal(data, &spend))
	require.Len(t, spend.Products, 1)
	require.Empty(t, spend.Products[0].Buckets)
	requireJSONKeys(t, data, "organization_id", "window_from", "window_to", "currency", "pricing_basis", "total_cost_usd", "queried_at", "products")

	body, data = issuerToolCall(t, reads, "get_organization_spend_breakdown", `{"organization_id":"org-fixture","include_daily_buckets":true}`, true)
	require.NotContains(t, body, privateID)
	require.NoError(t, json.Unmarshal(data, &spend))
	require.Equal(t, []SpendBucketDetail{{From: from, To: "2026-01-02T00:00:00Z", Quantity: "12", CostUSD: "0.24"}}, spend.Products[0].Buckets)

	reads.spend.Products = make([]*gen.SpendProduct, maxBillingDetailProducts+1)
	body, _ = issuerToolCall(t, reads, "get_organization_spend_breakdown", `{ "organization_id":"org-fixture" }`, true)
	require.Contains(t, body, `"isError":true`)
}
