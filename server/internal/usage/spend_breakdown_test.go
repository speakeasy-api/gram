package usage

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

func TestBuildSpendBreakdownPreservesExactLargeCostsAndPeriodSums(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 3)
	queriedAt := from.Add(36 * time.Hour)
	response, err := buildSpendBreakdownResponse(from, to, queriedAt, nil, spendAvailabilityAvailable, []chrepo.SpendRow{
		{Day: from, ProductID: "agent_session_storage", Quantity: "9007199254740993"},
		{Day: from, ProductID: "risk_content_scans", Quantity: "1000000"},
		{Day: from.AddDate(0, 0, 1), ProductID: "risk_content_scans", Quantity: "1000000"},
		{Day: from.AddDate(0, 0, 1), ProductID: "mcp_egress", Quantity: "1073741825"},
	})
	require.NoError(t, err)
	require.Equal(t, spendAvailabilityAvailable, response.Availability)
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

func TestGetSpendBreakdownReturnsUnsupportedPlanWithoutClickHouse(t *testing.T) {
	t.Parallel()
	organizationID := "org-spend-enterprise"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierEnterprise)
	_, err := repo.New(db).UpsertBillingMetadata(t.Context(), repo.UpsertBillingMetadataParams{
		OrganizationID:         organizationID,
		TumMonthlyTokenLimit:   pgtype.Int8{},
		AlertEmail:             pgtype.Text{},
		BillingCycleAnchorDay:  17,
		TunneledMcpServerLimit: pgtype.Int4{},
	})
	require.NoError(t, err)
	service.meterReadConn = nil
	service.now = func() time.Time { return time.Date(2026, time.April, 20, 10, 0, 0, 0, time.UTC) }
	from, to := "2026-04-17T00:00:00Z", "2026-04-20T00:00:00Z"
	ctx := authztest.WithExactGrants(t, billingEmailAdminContext(t, organizationID), authz.NewGrant(authz.ScopeOrgRead, organizationID))

	result, err := service.GetSpendBreakdown(ctx, &gen.GetSpendBreakdownPayload{From: &from, To: &to})

	require.NoError(t, err)
	require.Equal(t, spendAvailabilityUnsupportedPlan, result.Availability)
	require.Equal(t, from, result.Window.From)
	require.Equal(t, to, result.Window.To)
	require.Len(t, result.BillingCycles, 12)
	require.Equal(t, "2026-04-17T00:00:00Z", result.BillingCycles[11].From)
	require.Equal(t, "2026-05-17T00:00:00Z", result.BillingCycles[11].To)
	require.Equal(t, "USD", result.Currency)
	require.Equal(t, "current_payg_list_price", result.PricingBasis)
	require.Equal(t, "2026-04-20T10:00:00Z", result.QueriedAt)
	require.Equal(t, "0", result.TotalCostUsd)
	require.NotNil(t, result.Products)
	require.Empty(t, result.Products)
}

func TestGetSpendBreakdownValidatesRangeForUnsupportedPlan(t *testing.T) {
	t.Parallel()
	organizationID := "org-spend-invalid-range"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierEnterprise)
	service.meterReadConn = nil
	service.now = func() time.Time { return time.Date(2026, time.April, 20, 10, 0, 0, 0, time.UTC) }
	from, to := "not-a-date", "2026-04-20T00:00:00Z"
	ctx := authztest.WithExactGrants(t, billingEmailAdminContext(t, organizationID), authz.NewGrant(authz.ScopeOrgRead, organizationID))

	_, err := service.GetSpendBreakdown(ctx, &gen.GetSpendBreakdownPayload{From: &from, To: &to})

	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestGetSpendBreakdownPaygZeroUsageIsAvailable(t *testing.T) {
	t.Parallel()
	organizationID := "org-spend-payg"
	service, db, clickhouse, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	service.meterReadConn = clickhouse
	service.now = func() time.Time { return time.Date(2026, time.April, 20, 10, 0, 0, 0, time.UTC) }
	from, to := "2026-04-17T00:00:00Z", "2026-04-20T00:00:00Z"
	ctx := authztest.WithExactGrants(t, billingEmailAdminContext(t, organizationID), authz.NewGrant(authz.ScopeOrgRead, organizationID))

	result, err := service.GetSpendBreakdown(ctx, &gen.GetSpendBreakdownPayload{From: &from, To: &to})

	require.NoError(t, err)
	require.Equal(t, spendAvailabilityAvailable, result.Availability)
	require.Equal(t, "0", result.TotalCostUsd)
	require.Len(t, result.Products, 3)
	for _, product := range result.Products {
		require.Equal(t, "0", product.Quantity)
		require.Equal(t, "0", product.CostUsd)
		require.Len(t, product.Buckets, 3)
	}
}

func TestGetSpendBreakdownRequiresAuthoritativeOrganizationTier(t *testing.T) {
	t.Parallel()
	organizationID := "org-spend-missing-tier"
	service := newTestService(t, &mockBillingRepo{}, organizationID, 0)
	service.now = func() time.Time { return time.Date(2026, time.April, 20, 10, 0, 0, 0, time.UTC) }
	ctx := authztest.WithExactGrants(t, billingEmailAdminContext(t, organizationID), authz.NewGrant(authz.ScopeOrgRead, organizationID))

	_, err := service.GetSpendBreakdown(ctx, &gen.GetSpendBreakdownPayload{From: nil, To: nil})

	requireOopsCode(t, err, oops.CodeUnexpected)
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
