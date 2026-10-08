package usage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
)

func utcDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func TestPlanCustomerUsageMonthlyAlignsBucketsToBillingCycles(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 7, 15, 0, 0, 0, time.UTC)
	plan, err := planCustomerUsage(CustomerUsageOrganization{ID: "org", BillingCycleAnchorDay: 25, CreatedAt: utcDate(2024, time.January, 1)}, CustomerUsageMonthly, now)
	require.NoError(t, err)

	require.Equal(t, BillingCyclePeriod{Start: utcDate(2026, time.September, 25), End: utcDate(2026, time.October, 25)}, plan.current)
	require.Len(t, plan.buckets, customerUsageMonthlyCycles)
	require.Equal(t, BillingCyclePeriod{Start: utcDate(2026, time.April, 25), End: utcDate(2026, time.May, 25)}, plan.buckets[0])
	require.Equal(t, plan.current, plan.buckets[len(plan.buckets)-1])
	for index := 1; index < len(plan.buckets); index++ {
		require.Equal(t, plan.buckets[index-1].End, plan.buckets[index].Start)
	}
}

func TestPlanCustomerUsageMonthlyDropsCyclesBeforeCreation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 7, 15, 0, 0, 0, time.UTC)
	plan, err := planCustomerUsage(CustomerUsageOrganization{ID: "org", BillingCycleAnchorDay: 1, CreatedAt: time.Date(2026, time.August, 20, 9, 0, 0, 0, time.UTC)}, CustomerUsageMonthly, now)
	require.NoError(t, err)

	require.Equal(t, []BillingCyclePeriod{
		{Start: utcDate(2026, time.August, 1), End: utcDate(2026, time.September, 1)},
		{Start: utcDate(2026, time.September, 1), End: utcDate(2026, time.October, 1)},
		{Start: utcDate(2026, time.October, 1), End: utcDate(2026, time.November, 1)},
	}, plan.buckets)
}

func TestPlanCustomerUsageWeeklyStartsMondayWithinCycle(t *testing.T) {
	t.Parallel()
	// The Sep 25 - Oct 25 2026 cycle starts on a Friday.
	now := time.Date(2026, time.October, 7, 15, 0, 0, 0, time.UTC)
	plan, err := planCustomerUsage(CustomerUsageOrganization{ID: "org", BillingCycleAnchorDay: 25, CreatedAt: utcDate(2024, time.January, 1)}, CustomerUsageWeekly, now)
	require.NoError(t, err)

	require.Equal(t, []BillingCyclePeriod{
		{Start: utcDate(2026, time.September, 25), End: utcDate(2026, time.September, 28)},
		{Start: utcDate(2026, time.September, 28), End: utcDate(2026, time.October, 5)},
		{Start: utcDate(2026, time.October, 5), End: utcDate(2026, time.October, 12)},
		{Start: utcDate(2026, time.October, 12), End: utcDate(2026, time.October, 19)},
		{Start: utcDate(2026, time.October, 19), End: utcDate(2026, time.October, 25)},
	}, plan.buckets)
}

func TestPlanCustomerUsageDailyCoversCurrentCycle(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 7, 15, 0, 0, 0, time.UTC)
	plan, err := planCustomerUsage(CustomerUsageOrganization{ID: "org", BillingCycleAnchorDay: 1, CreatedAt: utcDate(2024, time.January, 1)}, CustomerUsageDaily, now)
	require.NoError(t, err)

	require.Len(t, plan.buckets, 31)
	require.Equal(t, BillingCyclePeriod{Start: utcDate(2026, time.October, 1), End: utcDate(2026, time.October, 2)}, plan.buckets[0])
	require.Equal(t, BillingCyclePeriod{Start: utcDate(2026, time.October, 31), End: utcDate(2026, time.November, 1)}, plan.buckets[30])
}

func TestPlanCustomerUsageComparesSameElapsedDays(t *testing.T) {
	t.Parallel()
	// 13 days into the cycle, today included.
	now := time.Date(2026, time.October, 7, 15, 0, 0, 0, time.UTC)
	plan, err := planCustomerUsage(CustomerUsageOrganization{ID: "org", BillingCycleAnchorDay: 25, CreatedAt: utcDate(2024, time.January, 1)}, CustomerUsageDaily, now)
	require.NoError(t, err)

	require.NotNil(t, plan.previous)
	require.Equal(t, BillingCyclePeriod{Start: utcDate(2026, time.August, 25), End: utcDate(2026, time.September, 7)}, *plan.previous)
	require.Equal(t, utcDate(2026, time.August, 25), plan.earliestNeeded)
}

func TestPlanCustomerUsageClampsComparisonToShorterPreviousCycle(t *testing.T) {
	t.Parallel()
	// Mar 31 is in the cycle starting Mar 1, but the previous cycle (February) has 28 days.
	now := time.Date(2026, time.March, 31, 15, 0, 0, 0, time.UTC)
	plan, err := planCustomerUsage(CustomerUsageOrganization{ID: "org", BillingCycleAnchorDay: 1, CreatedAt: utcDate(2024, time.January, 1)}, CustomerUsageDaily, now)
	require.NoError(t, err)

	require.Equal(t, BillingCyclePeriod{Start: utcDate(2026, time.February, 1), End: utcDate(2026, time.March, 1)}, *plan.previous)
}

func TestPlanCustomerUsageHasNoComparisonForNewOrganization(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 7, 15, 0, 0, 0, time.UTC)
	plan, err := planCustomerUsage(CustomerUsageOrganization{ID: "org", BillingCycleAnchorDay: 1, CreatedAt: utcDate(2026, time.October, 3)}, CustomerUsageMonthly, now)
	require.NoError(t, err)

	require.Nil(t, plan.previous)
	require.Equal(t, []BillingCyclePeriod{{Start: utcDate(2026, time.October, 1), End: utcDate(2026, time.November, 1)}}, plan.buckets)
}

func TestPlanCustomerUsageRejectsUnknownInterval(t *testing.T) {
	t.Parallel()
	_, err := planCustomerUsage(CustomerUsageOrganization{ID: "org", BillingCycleAnchorDay: 1, CreatedAt: utcDate(2024, time.January, 1)}, CustomerUsageInterval("yearly"), time.Now())
	require.Error(t, err)
}

func TestBuildCustomerUsageSplitsCurrentPreviousAndBuckets(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 7, 15, 0, 0, 0, time.UTC)
	plan, err := planCustomerUsage(CustomerUsageOrganization{ID: "org", BillingCycleAnchorDay: 25, CreatedAt: utcDate(2024, time.January, 1)}, CustomerUsageMonthly, now)
	require.NoError(t, err)

	rows := []chrepo.OrganizationSpendRow{
		// Previous cycle, inside the same-elapsed-days window.
		{OrganizationID: "org", Day: utcDate(2026, time.August, 26), ProductID: "agent_session_storage", Quantity: "2000000"},
		// Previous cycle, after the comparison window: in the chart, not in the comparison.
		{OrganizationID: "org", Day: utcDate(2026, time.September, 20), ProductID: "agent_session_storage", Quantity: "4000000"},
		// Current cycle.
		{OrganizationID: "org", Day: utcDate(2026, time.September, 25), ProductID: "agent_session_storage", Quantity: "1000000"},
		{OrganizationID: "org", Day: utcDate(2026, time.October, 7), ProductID: "agent_session_storage", Quantity: "1000000"},
		{OrganizationID: "org", Day: utcDate(2026, time.October, 1), ProductID: "mcp_egress", Quantity: "1073741824"},
		{OrganizationID: "org", Day: utcDate(2026, time.October, 2), ProductID: "risk_content_scans", Quantity: "1"},
	}
	customer, err := buildCustomerUsage(plan, rows)
	require.NoError(t, err)

	require.Equal(t, plan.current, customer.CurrentCycle)
	require.Equal(t, BillingCyclePeriod{Start: utcDate(2026, time.April, 25), End: utcDate(2026, time.October, 25)}, customer.Window)
	require.Len(t, customer.Products, 3)

	storage, scans, egress := customer.Products[0], customer.Products[1], customer.Products[2]
	require.Equal(t, "agent_session_storage", storage.ID)
	require.Equal(t, "2000000", storage.Quantity)
	require.Equal(t, "0.7", storage.CostUsd)
	require.Equal(t, "risk_content_scans", scans.ID)
	require.Equal(t, "0.00000099", scans.CostUsd)
	require.Equal(t, "mcp_egress", egress.ID)
	require.Equal(t, "20", egress.CostUsd)

	require.Len(t, storage.Buckets, customerUsageMonthlyCycles)
	previousBucket := storage.Buckets[len(storage.Buckets)-2]
	require.Equal(t, "2026-08-25T00:00:00Z", previousBucket.From)
	require.Equal(t, "6000000", previousBucket.Quantity)
	require.Equal(t, "2.1", previousBucket.CostUsd)
	currentBucket := storage.Buckets[len(storage.Buckets)-1]
	require.Equal(t, "2000000", currentBucket.Quantity)
	require.Equal(t, storage.CostUsd, currentBucket.CostUsd)

	require.Equal(t, []CustomerUsageProductCost{
		{ProductID: "agent_session_storage", CostUSD: "0.7"},
		{ProductID: "risk_content_scans", CostUSD: "0"},
		{ProductID: "mcp_egress", CostUSD: "0"},
	}, customer.PreviousPeriodCosts)
}

func TestBuildCustomerUsageRejectsUnknownProduct(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 7, 15, 0, 0, 0, time.UTC)
	plan, err := planCustomerUsage(CustomerUsageOrganization{ID: "org", BillingCycleAnchorDay: 1, CreatedAt: utcDate(2024, time.January, 1)}, CustomerUsageDaily, now)
	require.NoError(t, err)

	_, err = buildCustomerUsage(plan, []chrepo.OrganizationSpendRow{{OrganizationID: "org", Day: utcDate(2026, time.October, 2), ProductID: "inference", Quantity: "1"}})
	require.Error(t, err)
}
