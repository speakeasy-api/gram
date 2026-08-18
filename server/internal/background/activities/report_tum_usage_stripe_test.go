package activities_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/usage"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

type mockTUMStripeClient struct {
	mock.Mock
	catalog stripeclient.Catalog
}

func nullableOrganizationID(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: true}
}

func (m *mockTUMStripeClient) Catalog() stripeclient.Catalog {
	return m.catalog
}

func (m *mockTUMStripeClient) CreateMeterEvent(ctx context.Context, input stripeclient.CreateMeterEventInput) error {
	args := m.Called(ctx, input)
	return args.Error(0)
}

func (m *mockTUMStripeClient) GetMeterEventSummary(ctx context.Context, input stripeclient.GetMeterEventSummaryInput) (float64, error) {
	args := m.Called(ctx, input)
	value, ok := args.Get(0).(float64)
	if !ok {
		panic("mock meter summary value is not a float64")
	}

	return value, args.Error(1)
}

func setupTUMStripeReporter(t *testing.T, name string, accountType string, anchor time.Time) (*activities.ReportTUMUsageToStripe, *mockTUMStripeClient, *pgxpool.Pool, string) {
	t.Helper()
	ctx := t.Context()

	db, err := infra.CloneTestDatabase(t, name)
	require.NoError(t, err)

	organizationID := "org-" + uuid.NewString()[:8]
	organizations := orgrepo.New(db)
	_, err = organizations.UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Test Organization",
		Slug:        organizationID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, organizations.SetAccountType(ctx, orgrepo.SetAccountTypeParams{
		GramAccountType: accountType,
		ID:              organizationID,
	}))

	usageQueries := usagerepo.New(db)
	require.NoError(t, usageQueries.CreateStripeSubscriptionBillingMetadataFixture(ctx, usagerepo.CreateStripeSubscriptionBillingMetadataFixtureParams{
		OrganizationID:       organizationID,
		StripeCustomerID:     pgtype.Text{String: "cus_test", Valid: true},
		StripeSubscriptionID: pgtype.Text{String: "sub_test", Valid: true},
	}))
	_, err = usageQueries.ActivatePaygBillingMetadata(ctx, usagerepo.ActivatePaygBillingMetadataParams{
		StripeSubscriptionID:     pgtype.Text{String: "sub_test", Valid: true},
		StripeBillingCycleAnchor: pgtype.Timestamptz{Time: anchor, Valid: true},
		BillingCycleAnchorDay:    int32(anchor.Day()),
		OrganizationID:           organizationID,
		StripeCustomerID:         pgtype.Text{String: "cus_test", Valid: true},
	})
	require.NoError(t, err)

	stripe := &mockTUMStripeClient{
		Mock: mock.Mock{},
		catalog: stripeclient.Catalog{
			PriceIDTUM:     "price_tum",
			MeterIDTUM:     "mtr_tum",
			MeterEventName: "tum_tokens",
		},
	}
	reporter := activities.NewReportTUMUsageToStripe(testenv.NewLogger(t), db, stripe)
	return reporter, stripe, db, organizationID
}

func upsertTUMCycle(t *testing.T, db *pgxpool.Pool, organizationID string, start, end time.Time, tokens int64, finalizedAt *time.Time) {
	t.Helper()
	finalized := pgtype.Timestamptz{}
	if finalizedAt != nil {
		finalized = pgtype.Timestamptz{Time: finalizedAt.UTC(), Valid: true}
	}
	require.NoError(t, usagerepo.New(db).UpsertBillingCycleUsage(t.Context(), usagerepo.UpsertBillingCycleUsageParams{
		OrganizationID: organizationID,
		CycleStart:     pgtype.Timestamptz{Time: start.UTC(), Valid: true},
		CycleEnd:       pgtype.Timestamptz{Time: end.UTC(), Valid: true},
		TumTokens:      tokens,
		FinalizedAt:    finalized,
	}))
}

func TestReportTUMUsageToStripe_ReportsSignedDurableDeltas(t *testing.T) {
	t.Parallel()
	anchor := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	reporter, stripe, db, organizationID := setupTUMStripeReporter(t, "tum_stripe_deltas", "payg", anchor)
	cycleEnd := anchor.AddDate(0, 1, 0)
	upsertTUMCycle(t, db, organizationID, anchor, cycleEnd, 100, nil)

	stripe.On("CreateMeterEvent", mock.Anything, mock.MatchedBy(func(input stripeclient.CreateMeterEventInput) bool {
		expectedIdentifier := "tum:" + organizationID + ":" + anchor.Format(time.RFC3339) + ":1"
		return input.CustomerID == "cus_test" && input.EventName == "tum_tokens" && input.Value == 100 && input.IdempotencyKey == expectedIdentifier
	})).Return(nil).Once()
	now := anchor.Add(10 * 24 * time.Hour)
	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{OrganizationIDs: []string{organizationID}, Now: now}))

	// A missed hour heals from the new durable total, while the existing
	// intended amount prevents replaying the first 100 tokens.
	upsertTUMCycle(t, db, organizationID, anchor, cycleEnd, 70, nil)
	stripe.On("CreateMeterEvent", mock.Anything, mock.MatchedBy(func(input stripeclient.CreateMeterEventInput) bool {
		return input.Value == -30 && input.IdempotencyKey != ""
	})).Return(nil).Once()
	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{OrganizationIDs: []string{organizationID}, Now: now.Add(time.Hour)}))

	reports, err := usagerepo.New(db).ListTUMMeterReportsFixture(t.Context(), nullableOrganizationID(organizationID))
	require.NoError(t, err)
	require.Len(t, reports, 2)
	require.Equal(t, int64(100), reports[0].DeltaTokens)
	require.Equal(t, int64(-30), reports[1].DeltaTokens)
	require.Equal(t, "confirmed", reports[0].DeliveryState)
	require.Equal(t, "confirmed", reports[1].DeliveryState)
	require.NotEqual(t, reports[0].StripeIdentifier.String, reports[1].StripeIdentifier.String)
	stripe.AssertExpectations(t)
}

func TestReportTUMUsageToStripe_BoundsRemoteDeliveryPerOrganization(t *testing.T) {
	t.Parallel()
	anchor := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	reporter, stripe, db, organizationID := setupTUMStripeReporter(t, "tum_stripe_bounded_delivery", "payg", anchor)
	firstEnd := anchor.AddDate(0, 1, 0)
	secondEnd := firstEnd.AddDate(0, 1, 0)
	upsertTUMCycle(t, db, organizationID, anchor, firstEnd, 100, nil)
	upsertTUMCycle(t, db, organizationID, firstEnd, secondEnd, 200, nil)

	stripe.On("CreateMeterEvent", mock.Anything, mock.MatchedBy(func(input stripeclient.CreateMeterEventInput) bool {
		return input.Value == 100
	})).Return(nil).Once()
	now := firstEnd.Add(24 * time.Hour)
	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             now,
	}))

	reports, err := usagerepo.New(db).ListTUMMeterReportsFixture(t.Context(), nullableOrganizationID(organizationID))
	require.NoError(t, err)
	require.Len(t, reports, 2)
	require.Equal(t, "confirmed", reports[0].DeliveryState)
	require.Equal(t, "pending", reports[1].DeliveryState)

	stripe.On("CreateMeterEvent", mock.Anything, mock.MatchedBy(func(input stripeclient.CreateMeterEventInput) bool {
		return input.Value == 200
	})).Return(nil).Once()
	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             now.Add(time.Minute),
	}))
	stripe.AssertExpectations(t)
}

func TestReportTUMUsageToStripe_ContinuesLegacyCycleSequence(t *testing.T) {
	t.Parallel()
	anchor := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	reporter, stripe, db, organizationID := setupTUMStripeReporter(t, "tum_stripe_legacy_sequence", "payg", anchor)
	cycleEnd := anchor.AddDate(0, 1, 0)
	upsertTUMCycle(t, db, organizationID, anchor, cycleEnd, 100, nil)

	queries := usagerepo.New(db)
	_, err := queries.CreateLegacyTUMMeterReportFixture(t.Context(), usagerepo.CreateLegacyTUMMeterReportFixtureParams{
		OrganizationID: nullableOrganizationID(organizationID),
		CycleStart:     pgtype.Timestamptz{Time: anchor, Valid: true, InfinityModifier: pgtype.Finite},
		Seq:            1,
		DeltaTokens:    40,
	})
	require.NoError(t, err)

	stripe.On("CreateMeterEvent", mock.Anything, mock.MatchedBy(func(input stripeclient.CreateMeterEventInput) bool {
		return input.Value == 60
	})).Return(nil).Once()
	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             anchor.Add(24 * time.Hour),
	}))

	reports, err := queries.ListTUMMeterReportsFixture(t.Context(), nullableOrganizationID(organizationID))
	require.NoError(t, err)
	require.Len(t, reports, 2)
	require.EqualValues(t, 1, reports[0].Seq)
	require.EqualValues(t, 2, reports[1].Seq)
	require.Equal(t, int64(60), reports[1].DeltaTokens)
	require.Equal(t, "confirmed", reports[1].DeliveryState)
	stripe.AssertExpectations(t)
}

func TestReportTUMUsageToStripe_FreezesAt48HoursAndCarriesAt72Hours(t *testing.T) {
	t.Parallel()
	anchor := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	reporter, stripe, db, organizationID := setupTUMStripeReporter(t, "tum_stripe_freeze", "payg", anchor)
	cycleEnd := anchor.AddDate(0, 1, 0)
	upsertTUMCycle(t, db, organizationID, anchor, cycleEnd, 100, nil)

	stripe.On("CreateMeterEvent", mock.Anything, mock.MatchedBy(func(input stripeclient.CreateMeterEventInput) bool {
		return input.Value == 100 && input.Timestamp.Equal(cycleEnd.Add(-time.Second))
	})).Return(nil).Once()
	freezeTime := cycleEnd.Add(48 * time.Hour)
	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{OrganizationIDs: []string{organizationID}, Now: freezeTime}))

	// Late telemetry changes only the observed value after the freeze.
	upsertTUMCycle(t, db, organizationID, anchor, cycleEnd, 130, nil)
	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{OrganizationIDs: []string{organizationID}, Now: cycleEnd.Add(60 * time.Hour)}))
	finalizedAt := cycleEnd.Add(73 * time.Hour)
	upsertTUMCycle(t, db, organizationID, anchor, cycleEnd, 130, &finalizedAt)
	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{OrganizationIDs: []string{organizationID}, Now: finalizedAt}))
	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{OrganizationIDs: []string{organizationID}, Now: finalizedAt.Add(time.Hour)}))

	cycles, err := usagerepo.New(db).ListBillingCycleUsage(t.Context(), organizationID)
	require.NoError(t, err)
	require.Len(t, cycles, 1)
	require.Equal(t, int64(100), cycles[0].BilledTumTokens.Int64)
	require.Equal(t, int64(130), cycles[0].TumTokens)

	allocations, err := usagerepo.New(db).ListTUMCarryAllocationsFixture(t.Context(), pgtype.Text{String: organizationID, Valid: true})
	require.NoError(t, err)
	require.Len(t, allocations, 1)
	require.Equal(t, int64(30), allocations[0].DeltaTokens.Int64)
	require.Equal(t, "pending", allocations[0].DeliveryState)
	unitPrice, err := allocations[0].OriginalTumUnitPriceUsd.Float64Value()
	require.NoError(t, err)
	require.InDelta(t, 0.00000035, unitPrice.Float64, 0.000000000001)
	stripe.AssertExpectations(t)
}

func TestCreateTUMCarryAllocationRoundsCumulativeChargesToCents(t *testing.T) {
	t.Parallel()
	anchor := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	_, _, db, organizationID := setupTUMStripeReporter(t, "tum_carry_cent_boundary", "payg", anchor)
	cycleEnd := anchor.AddDate(0, 1, 0)
	upsertTUMCycle(t, db, organizationID, anchor, cycleEnd, 14_285, nil)

	queries := usagerepo.New(db)
	cycles, err := queries.ListBillingCycleUsage(t.Context(), organizationID)
	require.NoError(t, err)
	require.Len(t, cycles, 1)
	_, err = queries.FreezeTUMBillingCycleBaseline(t.Context(), usagerepo.FreezeTUMBillingCycleBaselineParams{
		FrozenAt:            pgtype.Timestamptz{Time: cycleEnd.Add(48 * time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
		OrganizationID:      nullableOrganizationID(organizationID),
		BillingCycleUsageID: cycles[0].ID,
	})
	require.NoError(t, err)

	finalizedAt := cycleEnd.Add(73 * time.Hour)
	upsertTUMCycle(t, db, organizationID, anchor, cycleEnd, 14_286, &finalizedAt)
	_, err = queries.CreateTUMCarryAllocation(t.Context(), usagerepo.CreateTUMCarryAllocationParams{
		TumUnitPriceUsd:     usage.TUMUnitPriceUSD,
		OrganizationID:      nullableOrganizationID(organizationID),
		BillingCycleUsageID: cycles[0].ID,
	})
	require.NoError(t, err)

	allocations, err := queries.ListTUMCarryAllocationsFixture(t.Context(), nullableOrganizationID(organizationID))
	require.NoError(t, err)
	require.Len(t, allocations, 1)
	require.Equal(t, int64(1), allocations[0].DeltaTokens.Int64)
	amount, err := allocations[0].AmountUsd.Float64Value()
	require.NoError(t, err)
	require.InDelta(t, 0.01, amount.Float64, 0.0000001)
}

func TestReportTUMUsageToStripe_CarriesFullCycleAfterMissedFreezeWindow(t *testing.T) {
	t.Parallel()
	anchor := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	reporter, stripe, db, organizationID := setupTUMStripeReporter(t, "tum_stripe_missed_freeze", "payg", anchor)
	cycleEnd := anchor.AddDate(0, 1, 0)
	recoveredAt := cycleEnd.Add(73 * time.Hour)
	upsertTUMCycle(t, db, organizationID, anchor, cycleEnd, 130, &recoveredAt)

	reportInput := activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             recoveredAt,
	}
	require.NoError(t, reporter.Do(t.Context(), reportInput))
	require.NoError(t, reporter.Do(t.Context(), reportInput))

	cycles, err := usagerepo.New(db).ListBillingCycleUsage(t.Context(), organizationID)
	require.NoError(t, err)
	require.Len(t, cycles, 1)
	require.Equal(t, int64(0), cycles[0].BilledTumTokens.Int64)
	require.True(t, cycles[0].BilledFrozenAt.Valid)

	allocations, err := usagerepo.New(db).ListTUMCarryAllocationsFixture(t.Context(), pgtype.Text{String: organizationID, Valid: true})
	require.NoError(t, err)
	require.Len(t, allocations, 1)
	require.Equal(t, int64(130), allocations[0].DeltaTokens.Int64)
	require.Equal(t, "pending", allocations[0].DeliveryState)

	reports, err := usagerepo.New(db).ListTUMMeterReportsFixture(t.Context(), nullableOrganizationID(organizationID))
	require.NoError(t, err)
	require.Empty(t, reports)
	stripe.AssertNotCalled(t, "CreateMeterEvent", mock.Anything, mock.Anything)
}

func TestReportTUMUsageToStripe_RepairsFrozenBaselineAfterObservationCutoff(t *testing.T) {
	t.Parallel()
	anchor := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	reporter, stripe, db, organizationID := setupTUMStripeReporter(t, "tum_stripe_post_cutoff_repair", "payg", anchor)
	cycleEnd := anchor.AddDate(0, 1, 0)
	upsertTUMCycle(t, db, organizationID, anchor, cycleEnd, 100, nil)

	stripe.On("CreateMeterEvent", mock.Anything, mock.MatchedBy(func(input stripeclient.CreateMeterEventInput) bool {
		return input.Value == 100 && input.Timestamp.Equal(cycleEnd.Add(-time.Second))
	})).Return(errors.New("response lost")).Once()
	freezeTime := cycleEnd.Add(48 * time.Hour)
	require.Error(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             freezeTime,
	}))

	finalizedAt := cycleEnd.Add(73 * time.Hour)
	upsertTUMCycle(t, db, organizationID, anchor, cycleEnd, 130, &finalizedAt)
	stripe.On("GetMeterEventSummary", mock.Anything, stripeclient.GetMeterEventSummaryInput{
		CustomerID: "cus_test",
		Start:      anchor,
		End:        cycleEnd,
	}).Return(float64(0), nil).Once()
	require.Error(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             freezeTime.Add(25 * time.Hour),
	}))

	stripe.On("GetMeterEventSummary", mock.Anything, stripeclient.GetMeterEventSummaryInput{
		CustomerID: "cus_test",
		Start:      anchor,
		End:        cycleEnd,
	}).Return(float64(0), nil).Once()
	stripe.On("CreateMeterEvent", mock.Anything, mock.MatchedBy(func(input stripeclient.CreateMeterEventInput) bool {
		return input.Value == 100 && input.Timestamp.Equal(cycleEnd.Add(-time.Second))
	})).Return(nil).Once()
	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             freezeTime.Add(49 * time.Hour),
	}))
	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             freezeTime.Add(49*time.Hour + time.Minute),
	}))

	reports, err := usagerepo.New(db).ListTUMMeterReportsFixture(t.Context(), nullableOrganizationID(organizationID))
	require.NoError(t, err)
	require.Len(t, reports, 2)
	require.Equal(t, "reconciled_missing", reports[0].DeliveryState)
	require.Equal(t, "confirmed", reports[1].DeliveryState)
	require.NotEqual(t, reports[0].StripeIdentifier.String, reports[1].StripeIdentifier.String)

	allocations, err := usagerepo.New(db).ListTUMCarryAllocationsFixture(t.Context(), pgtype.Text{String: organizationID, Valid: true})
	require.NoError(t, err)
	require.Len(t, allocations, 1)
	require.Equal(t, int64(30), allocations[0].DeltaTokens.Int64)
	stripe.AssertExpectations(t)
}

func TestReportTUMUsageToStripe_ReconcilesMissingAmbiguousDelivery(t *testing.T) {
	t.Parallel()
	anchor := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	reporter, stripe, db, organizationID := setupTUMStripeReporter(t, "tum_stripe_reconcile", "payg", anchor)
	cycleEnd := anchor.AddDate(0, 1, 0)
	upsertTUMCycle(t, db, organizationID, anchor, cycleEnd, 100, nil)

	stripe.On("CreateMeterEvent", mock.Anything, mock.Anything).Return(errors.New("connection reset")).Once()
	firstAttempt := anchor.Add(5 * 24 * time.Hour)
	require.Error(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{OrganizationIDs: []string{organizationID}, Now: firstAttempt}))

	stripe.On("GetMeterEventSummary", mock.Anything, stripeclient.GetMeterEventSummaryInput{
		CustomerID: "cus_test",
		Start:      anchor,
		End:        cycleEnd,
	}).Return(float64(0), nil).Once()
	require.Error(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             firstAttempt.Add(25 * time.Hour),
	}))

	reports, err := usagerepo.New(db).ListTUMMeterReportsFixture(t.Context(), nullableOrganizationID(organizationID))
	require.NoError(t, err)
	require.Len(t, reports, 1)
	require.Equal(t, "ambiguous", reports[0].DeliveryState)
	require.True(t, reports[0].ReconciledAt.Valid)

	stripe.On("GetMeterEventSummary", mock.Anything, stripeclient.GetMeterEventSummaryInput{
		CustomerID: "cus_test",
		Start:      anchor,
		End:        cycleEnd,
	}).Return(float64(0), nil).Once()
	stripe.On("CreateMeterEvent", mock.Anything, mock.MatchedBy(func(input stripeclient.CreateMeterEventInput) bool {
		return input.Value == 100
	})).Return(nil).Once()
	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             firstAttempt.Add(49 * time.Hour),
	}))
	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             firstAttempt.Add(49*time.Hour + time.Minute),
	}))

	reports, err = usagerepo.New(db).ListTUMMeterReportsFixture(t.Context(), nullableOrganizationID(organizationID))
	require.NoError(t, err)
	require.Len(t, reports, 2)
	require.Equal(t, "reconciled_missing", reports[0].DeliveryState)
	require.True(t, reports[0].ReconciledAt.Valid)
	require.Equal(t, "confirmed", reports[1].DeliveryState)
	require.NotEqual(t, reports[0].StripeIdentifier.String, reports[1].StripeIdentifier.String)
	stripe.AssertExpectations(t)
}

func TestReportTUMUsageToStripe_RetriesSameIdentifierWithin24Hours(t *testing.T) {
	t.Parallel()
	anchor := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	reporter, stripe, db, organizationID := setupTUMStripeReporter(t, "tum_stripe_retry", "payg", anchor)
	cycleEnd := anchor.AddDate(0, 1, 0)
	upsertTUMCycle(t, db, organizationID, anchor, cycleEnd, 100, nil)

	var identifier string
	stripe.On("CreateMeterEvent", mock.Anything, mock.MatchedBy(func(input stripeclient.CreateMeterEventInput) bool {
		identifier = input.IdempotencyKey
		return input.Value == 100 && identifier != ""
	})).Return(errors.New("connection reset")).Once()
	firstAttempt := anchor.Add(5 * 24 * time.Hour)
	err := reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             firstAttempt,
	})
	require.Error(t, err)
	var applicationErr *temporal.ApplicationError
	require.ErrorAs(t, err, &applicationErr)
	var failureDetails activities.ReportTUMUsageToStripeFailureDetails
	require.NoError(t, applicationErr.Details(&failureDetails))
	require.Equal(t, 1, failureDetails.FailedOrganizationCount)

	stripe.On("CreateMeterEvent", mock.Anything, mock.MatchedBy(func(input stripeclient.CreateMeterEventInput) bool {
		return input.Value == 100 && input.IdempotencyKey == identifier
	})).Return(nil).Once()
	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             firstAttempt.Add(time.Hour),
	}))

	reports, err := usagerepo.New(db).ListTUMMeterReportsFixture(t.Context(), nullableOrganizationID(organizationID))
	require.NoError(t, err)
	require.Len(t, reports, 1)
	require.Equal(t, "confirmed", reports[0].DeliveryState)
	require.Equal(t, identifier, reports[0].StripeIdentifier.String)
	stripe.AssertNotCalled(t, "GetMeterEventSummary", mock.Anything, mock.Anything)
	stripe.AssertExpectations(t)
}

func TestReportTUMUsageToStripe_ReconcilesDeliveredAmbiguousIntent(t *testing.T) {
	t.Parallel()
	anchor := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	reporter, stripe, db, organizationID := setupTUMStripeReporter(t, "tum_stripe_reconcile_delivered", "payg", anchor)
	cycleEnd := anchor.AddDate(0, 1, 0)
	upsertTUMCycle(t, db, organizationID, anchor, cycleEnd, 100, nil)

	stripe.On("CreateMeterEvent", mock.Anything, mock.Anything).Return(errors.New("response lost after delivery")).Once()
	firstAttempt := anchor.Add(5 * 24 * time.Hour)
	require.Error(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             firstAttempt,
	}))

	stripe.On("GetMeterEventSummary", mock.Anything, stripeclient.GetMeterEventSummaryInput{
		CustomerID: "cus_test",
		Start:      anchor,
		End:        cycleEnd,
	}).Return(float64(100), nil).Once()
	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             firstAttempt.Add(25 * time.Hour),
	}))

	reports, err := usagerepo.New(db).ListTUMMeterReportsFixture(t.Context(), nullableOrganizationID(organizationID))
	require.NoError(t, err)
	require.Len(t, reports, 1)
	require.Equal(t, "confirmed", reports[0].DeliveryState)
	require.True(t, reports[0].ReconciledAt.Valid)
	stripe.AssertExpectations(t)
}

func TestReportTUMUsageToStripe_SkipsNonPayg(t *testing.T) {
	t.Parallel()
	anchor := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	reporter, stripe, db, organizationID := setupTUMStripeReporter(t, "tum_stripe_skip_free", "free", anchor)
	upsertTUMCycle(t, db, organizationID, anchor, anchor.AddDate(0, 1, 0), 100, nil)

	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             anchor.Add(24 * time.Hour),
	}))
	reports, err := usagerepo.New(db).ListTUMMeterReportsFixture(t.Context(), nullableOrganizationID(organizationID))
	require.NoError(t, err)
	require.Empty(t, reports)
	stripe.AssertNotCalled(t, "CreateMeterEvent", mock.Anything, mock.Anything)
}

func TestReportTUMUsageToStripe_SkipsPreAnchorCycles(t *testing.T) {
	t.Parallel()
	anchor := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	reporter, stripe, db, organizationID := setupTUMStripeReporter(t, "tum_stripe_skip_pre_anchor", "payg", anchor)
	upsertTUMCycle(t, db, organizationID, anchor.AddDate(0, -1, 0), anchor, 100, nil)

	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             anchor.Add(24 * time.Hour),
	}))
	reports, err := usagerepo.New(db).ListTUMMeterReportsFixture(t.Context(), nullableOrganizationID(organizationID))
	require.NoError(t, err)
	require.Empty(t, reports)
	stripe.AssertNotCalled(t, "CreateMeterEvent", mock.Anything, mock.Anything)
}
