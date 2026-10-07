package admin

import (
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usage"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

func TestListCustomerUsageSelectsPayingOrganizations(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	fake := &fakeBillingOperations{}
	svc.billing = fake

	now := time.Now().UTC()
	disabledAt := now.Add(-time.Hour)
	convertedAt := now.Add(-48 * time.Hour)
	// Names sort in the order the response is expected to list included orgs.
	seedOrg(t, ctx, db, orgFixture{id: "org_ent_no_trial", name: "A Enterprise no trial", slug: "a-ent", accountType: "enterprise"})
	seedOrg(t, ctx, db, orgFixture{id: "org_ent_expired", name: "B Enterprise expired", slug: "b-ent", accountType: "enterprise"})
	seedTrial(t, ctx, db, trialFixture{orgID: "org_ent_expired", endsAt: now.Add(-24 * time.Hour)})
	seedOrg(t, ctx, db, orgFixture{id: "org_ent_converted", name: "C Enterprise converted", slug: "c-ent", accountType: "enterprise"})
	seedTrial(t, ctx, db, trialFixture{orgID: "org_ent_converted", endsAt: now.Add(30 * 24 * time.Hour), convertedAt: &convertedAt})
	seedOrg(t, ctx, db, orgFixture{id: "org_payg", name: "D PAYG", slug: "d-payg", accountType: "payg"})
	seedOrg(t, ctx, db, orgFixture{id: "org_pro", name: "e Pro", slug: "e-pro", accountType: "pro"})

	seedOrg(t, ctx, db, orgFixture{id: "org_ent_running", name: "Enterprise running", slug: "ent-running", accountType: "enterprise"})
	seedTrial(t, ctx, db, trialFixture{orgID: "org_ent_running", endsAt: now.Add(30 * 24 * time.Hour)})
	seedOrg(t, ctx, db, orgFixture{id: "org_ent_ending", name: "Enterprise ending", slug: "ent-ending", accountType: "enterprise"})
	seedTrial(t, ctx, db, trialFixture{orgID: "org_ent_ending", endsAt: now.Add(2 * 24 * time.Hour)})
	seedOrg(t, ctx, db, orgFixture{id: "org_pro_trialled", name: "Pro trialled", slug: "pro-trialled", accountType: "pro"})
	seedTrial(t, ctx, db, trialFixture{orgID: "org_pro_trialled", endsAt: now.Add(-24 * time.Hour)})
	seedOrg(t, ctx, db, orgFixture{id: "org_free", name: "Free", slug: "free", accountType: "free"})
	seedOrg(t, ctx, db, orgFixture{id: "org_ent_disabled", name: "Enterprise disabled", slug: "ent-disabled", accountType: "enterprise", disabledAt: &disabledAt})

	result, err := svc.ListCustomerUsage(ctx, &gen.ListCustomerUsagePayload{Interval: "monthly"})
	require.NoError(t, err)

	ids := make([]string, 0, len(result.Customers))
	trialStates := make(map[string]string, len(result.Customers))
	for _, customer := range result.Customers {
		ids = append(ids, customer.OrganizationID)
		trialStates[customer.OrganizationID] = customer.TrialState
	}
	require.Equal(t, []string{"org_ent_no_trial", "org_ent_expired", "org_ent_converted", "org_payg", "org_pro"}, ids)
	require.Equal(t, "none", trialStates["org_ent_no_trial"])
	require.Equal(t, "expired", trialStates["org_ent_expired"])
	require.Equal(t, "converted", trialStates["org_ent_converted"])
	require.Equal(t, usage.CustomerUsageMonthly, fake.customerUsageInterval)
	require.Len(t, fake.customerUsageOrgs, 5)
}

func TestListCustomerUsagePassesBillingAnchorAndCreation(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	fake := &fakeBillingOperations{}
	svc.billing = fake

	createdAt := time.Date(2026, time.March, 4, 5, 6, 7, 0, time.UTC)
	seedOrg(t, ctx, db, orgFixture{id: "org_anchored", name: "Anchored", slug: "anchored", accountType: "enterprise", createdAt: &createdAt})
	_, err := usagerepo.New(db).UpsertBillingMetadata(ctx, usagerepo.UpsertBillingMetadataParams{
		OrganizationID: "org_anchored", TumMonthlyTokenLimit: pgtype.Int8{Int64: 1, Valid: true},
		AlertEmail: conv.ToPGText("billing@example.test"), BillingCycleAnchorDay: 25,
		TunneledMcpServerLimit: pgtype.Int4{Int32: 1, Valid: true},
	})
	require.NoError(t, err)
	seedOrg(t, ctx, db, orgFixture{id: "org_unanchored", name: "Unanchored", slug: "unanchored", accountType: "payg"})

	_, err = svc.ListCustomerUsage(ctx, &gen.ListCustomerUsagePayload{Interval: "weekly"})
	require.NoError(t, err)

	require.Equal(t, usage.CustomerUsageWeekly, fake.customerUsageInterval)
	require.Equal(t, []usage.CustomerUsageOrganization{
		{ID: "org_anchored", BillingCycleAnchorDay: 25, CreatedAt: createdAt},
		{ID: "org_unanchored", BillingCycleAnchorDay: 1, CreatedAt: time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)},
	}, fake.customerUsageOrgs)
}

func TestListCustomerUsageMapsUsageAndPerOrganizationErrors(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	fake := &fakeBillingOperations{customerUsageErrs: map[string]error{
		"org_mixed":  fmt.Errorf("wrapped: %w", chrepo.ErrMixedMeasurement),
		"org_broken": fmt.Errorf("unexpected"),
	}}
	svc.billing = fake
	seedOrg(t, ctx, db, orgFixture{id: "org_ok", name: "A OK", slug: "ok", accountType: "enterprise"})
	seedOrg(t, ctx, db, orgFixture{id: "org_mixed", name: "B Mixed", slug: "mixed", accountType: "enterprise"})
	seedOrg(t, ctx, db, orgFixture{id: "org_broken", name: "C Broken", slug: "broken", accountType: "enterprise"})

	result, err := svc.ListCustomerUsage(ctx, &gen.ListCustomerUsagePayload{Interval: "daily"})
	require.NoError(t, err)
	require.Equal(t, "daily", result.Interval)
	require.Equal(t, "2026-10-07T12:00:00Z", result.QueriedAt)
	require.Len(t, result.Customers, 3)

	ok := result.Customers[0]
	require.Nil(t, ok.Error)
	require.Equal(t, "A OK", ok.Name)
	require.Equal(t, "enterprise", ok.AccountType)
	require.Equal(t, &gen.MeterUsageWindow{From: "2026-09-25T00:00:00Z", To: "2026-10-25T00:00:00Z"}, ok.CurrentCycle)
	require.Len(t, ok.Products, 1)
	require.Equal(t, "0.35", ok.Products[0].CostUsd)
	require.Len(t, ok.Products[0].Buckets, 1)
	require.Equal(t, &gen.MeterUsageWindow{From: "2026-08-25T00:00:00Z", To: "2026-09-07T00:00:00Z"}, ok.PreviousPeriod)
	require.Equal(t, []*gen.AdminCustomerUsageProductCost{{ProductID: "agent_session_storage", CostUsd: "0.1"}}, ok.PreviousPeriodCosts)

	mixed := result.Customers[1]
	require.NotNil(t, mixed.Error)
	require.Equal(t, customerUsageMixedMeasurementMessage, *mixed.Error)
	require.Empty(t, mixed.Products)
	require.Nil(t, mixed.PreviousPeriod)

	broken := result.Customers[2]
	require.NotNil(t, broken.Error)
	require.Equal(t, "Usage for this organization could not be read.", *broken.Error)
}

func TestListCustomerUsageUnavailableWithoutBilling(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	svc.billing = nil

	_, err := svc.ListCustomerUsage(ctx, &gen.ListCustomerUsagePayload{Interval: "monthly"})
	requireOopsCode(t, err, oops.CodeUnavailable)
}

func TestListCustomerUsageRejectsUnknownInterval(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	svc.billing = &fakeBillingOperations{}

	_, err := svc.ListCustomerUsage(ctx, &gen.ListCustomerUsagePayload{Interval: "yearly"})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
