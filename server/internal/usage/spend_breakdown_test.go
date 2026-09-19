package usage

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestBuildSpendBreakdownPreservesExactLargeCostsAndPeriodSums(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 3)
	queriedAt := from.Add(36 * time.Hour)
	response, err := buildSpendBreakdownResponse(from, to, queriedAt, nil, []chrepo.SpendRow{
		{Day: from, ProductID: "agent_session_storage", Quantity: "9007199254740993"},
		{Day: from, ProductID: "risk_content_scans", Quantity: "1000000"},
		{Day: from.AddDate(0, 0, 1), ProductID: "risk_content_scans", Quantity: "1000000"},
		{Day: from.AddDate(0, 0, 1), ProductID: "mcp_egress", Quantity: "1073741825"},
	})
	require.NoError(t, err)
	require.Equal(t, "USD", response.Currency)
	require.Equal(t, "current_payg_list_price", response.PricingBasis)
	require.Equal(t, queriedAt.Format(time.RFC3339Nano), response.QueriedAt)
	require.Equal(t, "3152519761.1393475686264514923095703125", response.TotalCostUsd)
	require.Len(t, response.Products, 3)

	storage, risk, egress := response.Products[0], response.Products[1], response.Products[2]
	require.Equal(t, "agent_session_storage", storage.ID)
	require.Equal(t, "9007199254740993", storage.Quantity)
	require.Equal(t, "3152519739.15934755", storage.CostUsd)
	require.Equal(t, []string{"9007199254740993", "0", "0"}, spendBucketQuantities(storage.Buckets))

	require.Equal(t, "risk_content_scans", risk.ID)
	require.Equal(t, "2000000", risk.Quantity)
	require.Equal(t, "1.98", risk.CostUsd)
	require.Equal(t, []string{"0.99", "0.99", "0"}, spendBucketCosts(risk.Buckets))

	require.Equal(t, "mcp_egress", egress.ID)
	require.Equal(t, "1073741825", egress.Quantity)
	require.Equal(t, "20.0000000186264514923095703125", egress.CostUsd)
	require.Equal(t, []string{"0", "1073741825", "0"}, spendBucketQuantities(egress.Buckets))
	for _, product := range response.Products {
		require.Len(t, product.Buckets, 3)
		require.Equal(t, from.Format(time.RFC3339Nano), product.Buckets[0].From)
		require.Equal(t, to.Format(time.RFC3339Nano), product.Buckets[2].To)
	}
}

func TestGetSpendBreakdownRequiresOrganizationRead(t *testing.T) {
	t.Parallel()
	organizationID := "org-" + uuid.NewString()
	service := newTestService(t, &mockBillingRepo{}, organizationID, 0)
	ctx := authztest.WithExactGrants(t, billingEmailAdminContext(t, organizationID))

	_, err := service.GetSpendBreakdown(ctx, &gen.GetSpendBreakdownPayload{From: nil, To: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func spendBucketQuantities(buckets []*gen.SpendBucket) []string {
	result := make([]string, 0, len(buckets))
	for _, bucket := range buckets {
		result = append(result, bucket.Quantity)
	}
	return result
}

func spendBucketCosts(buckets []*gen.SpendBucket) []string {
	result := make([]string, 0, len(buckets))
	for _, bucket := range buckets {
		result = append(result, bucket.CostUsd)
	}
	return result
}
