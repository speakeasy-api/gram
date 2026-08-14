package activities_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	backgroundrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
)

type allocationStripeMock struct {
	mu sync.Mutex

	invoices map[string]*stripeclient.InvoiceState
	getState func(string, int) *stripeclient.InvoiceState
	getCalls int

	itemInputs   []stripeclient.CreateInvoiceItemInput
	creditInputs []stripeclient.CreateCreditNoteInput
	itemErrors   []error
	creditErrors []error

	foundItem *stripeclient.InvoiceItem
	foundNote *stripeclient.CreditNote
	findItems int
	findNotes int
}

func (m *allocationStripeMock) GetInvoice(_ context.Context, id string) (*stripeclient.InvoiceState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getCalls++
	var state *stripeclient.InvoiceState
	if m.getState != nil {
		state = m.getState(id, m.getCalls)
	} else {
		state = m.invoices[id]
	}
	if state == nil {
		return nil, fmt.Errorf("invoice %s not found", id)
	}
	invoiceCopy := *state
	return &invoiceCopy, nil
}

func (m *allocationStripeMock) CreateInvoiceItem(_ context.Context, input stripeclient.CreateInvoiceItemInput) (*stripeclient.InvoiceItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.itemInputs = append(m.itemInputs, input)
	if len(m.itemErrors) > 0 {
		err := m.itemErrors[0]
		m.itemErrors = m.itemErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	return &stripeclient.InvoiceItem{
		ID:          fmt.Sprintf("ii_%d", len(m.itemInputs)),
		InvoiceID:   input.InvoiceID,
		Currency:    "usd",
		AmountCents: input.AmountCents,
	}, nil
}

func (m *allocationStripeMock) CreateCreditNote(_ context.Context, input stripeclient.CreateCreditNoteInput) (*stripeclient.CreditNote, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.creditInputs = append(m.creditInputs, input)
	if len(m.creditErrors) > 0 {
		err := m.creditErrors[0]
		m.creditErrors = m.creditErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	return &stripeclient.CreditNote{
		ID:          fmt.Sprintf("cn_%d", len(m.creditInputs)),
		InvoiceID:   input.InvoiceID,
		Currency:    "usd",
		AmountCents: input.AmountCents,
	}, nil
}

func (m *allocationStripeMock) FindInvoiceItem(_ context.Context, _ stripeclient.FindInvoiceAllocationInput) (*stripeclient.InvoiceItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.findItems++
	return m.foundItem, nil
}

func (m *allocationStripeMock) FindCreditNote(_ context.Context, _ stripeclient.FindInvoiceAllocationInput) (*stripeclient.CreditNote, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.findNotes++
	return m.foundNote, nil
}

func setupAllocationTest(t *testing.T, name string) (*pgxpool.Pool, string, *allocationStripeMock, *activities.SettleStripeInvoiceAllocations) {
	t.Helper()
	db, err := infra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	organizationID := "org-" + uuid.NewString()[:8]
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Allocation Test Organization",
		Slug:        organizationID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	stripe := &allocationStripeMock{invoices: make(map[string]*stripeclient.InvoiceState)}
	activity := activities.NewSettleStripeInvoiceAllocations(testenv.NewLogger(t), db, stripe)
	return db, organizationID, stripe, activity
}

func addStripeInvoice(t *testing.T, db *pgxpool.Pool, stripe *allocationStripeMock, organizationID, invoiceID string, start, end time.Time, state string, remaining int64) {
	t.Helper()
	remoteFinalizedAt := time.Time{}
	finalized := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	if state != "draft" {
		remoteFinalizedAt = end.Add(time.Hour)
		finalized = pgtype.Timestamptz{Time: remoteFinalizedAt.UTC(), InfinityModifier: pgtype.Finite, Valid: true}
	}
	err := backgroundrepo.New(db).CreateStripeInvoiceFixture(t.Context(), backgroundrepo.CreateStripeInvoiceFixtureParams{
		StripeInvoiceID:      invoiceID,
		OrganizationID:       pgtype.Text{String: organizationID, Valid: true},
		StripeCustomerID:     "cus_" + organizationID,
		StripeSubscriptionID: "sub_" + organizationID,
		ServicePeriodStart:   pgtype.Timestamptz{Time: start.UTC(), InfinityModifier: pgtype.Finite, Valid: true},
		ServicePeriodEnd:     pgtype.Timestamptz{Time: end.UTC(), InfinityModifier: pgtype.Finite, Valid: true},
		InvoiceState:         state,
		FinalizedAt:          finalized,
	})
	require.NoError(t, err)
	stripe.invoices[invoiceID] = &stripeclient.InvoiceState{
		ID:                 invoiceID,
		CustomerID:         "cus_" + organizationID,
		SubscriptionID:     "sub_" + organizationID,
		Currency:           "usd",
		BillingReason:      "subscription_cycle",
		Status:             state,
		ServicePeriodStart: start.UTC(),
		ServicePeriodEnd:   end.UTC(),
		FinalizedAt:        remoteFinalizedAt,
		AmountRemaining:    remaining,
	}
}

func putDailySpend(t *testing.T, db *pgxpool.Pool, organizationID string, day time.Time, amount string) {
	t.Helper()
	var spend pgtype.Numeric
	require.NoError(t, spend.Scan(amount))
	require.NoError(t, backgroundrepo.New(db).UpsertOpenRouterDailySpend(t.Context(), backgroundrepo.UpsertOpenRouterDailySpendParams{
		TargetOrganizationID: organizationID,
		TargetKeyType:        "chat",
		TargetDay:            pgtype.Date{Time: day.UTC(), InfinityModifier: pgtype.Finite, Valid: true},
		TargetSpendUsd:       spend,
	}))
}

func listAllocationFixtures(t *testing.T, db *pgxpool.Pool, organizationID string) []backgroundrepo.ListStripeInvoiceAllocationsFixtureRow {
	t.Helper()
	rows, err := backgroundrepo.New(db).ListStripeInvoiceAllocationsFixture(
		t.Context(), pgtype.Text{String: organizationID, Valid: true})
	require.NoError(t, err)
	return rows
}

func addTUMCarry(t *testing.T, db *pgxpool.Pool, organizationID string, start, end time.Time, cents int64, key string) {
	t.Helper()
	snapshot := pgtype.Numeric{}
	amount := pgtype.Numeric{}
	require.NoError(t, snapshot.Scan(fmt.Sprintf("%d.%02d", max(cents, -cents)/100, max(cents, -cents)%100)))
	sign := ""
	if cents < 0 {
		sign = "-"
	}
	absCents := max(cents, -cents)
	require.NoError(t, amount.Scan(fmt.Sprintf("%s%d.%02d", sign, absCents/100, absCents%100)))
	require.NoError(t, backgroundrepo.New(db).CreateTUMInvoiceAllocationFixture(t.Context(), backgroundrepo.CreateTUMInvoiceAllocationFixtureParams{
		OrganizationID:    pgtype.Text{String: organizationID, Valid: true},
		SourceKey:         key,
		SourcePeriodStart: pgtype.Timestamptz{Time: start.UTC(), InfinityModifier: pgtype.Finite, Valid: true},
		SourcePeriodEnd:   pgtype.Timestamptz{Time: end.UTC(), InfinityModifier: pgtype.Finite, Valid: true},
		SourceSnapshotUsd: snapshot,
		AmountUsd:         amount,
		IdempotencyKey:    "tum-carry:" + organizationID + ":" + key,
	}))
}

func TestSettleStripeInvoiceAllocations_BoundariesAndHalfAwayFromZero(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_boundaries")
	start := time.Date(2026, time.January, 31, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "draft", 0)
	putDailySpend(t, db, organizationID, start, "1.005000")

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{
		Now: end.Add(48*time.Hour - time.Nanosecond),
	}))
	require.Empty(t, listAllocationFixtures(t, db, organizationID))

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{
		Now: end.Add(48 * time.Hour),
	}))
	rows := listAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 1)
	require.Equal(t, "1.005000", numericString(t, rows[0].SourceSnapshotUsd))
	require.Equal(t, "1.010000", numericString(t, rows[0].AmountUsd))
	require.Equal(t, "confirmed", rows[0].DeliveryState)
	require.Len(t, stripe.itemInputs, 1)
	require.Equal(t, int64(101), stripe.itemInputs[0].AmountCents)
	require.Equal(t, start, stripe.itemInputs[0].PeriodStart)
	require.Equal(t, end, stripe.itemInputs[0].PeriodEnd)

	putDailySpend(t, db, organizationID, start, "1.014999")
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{
		Now: end.Add(72 * time.Hour),
	}))
	rows = listAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 2)
	require.Equal(t, int32(2), rows[1].Seq)
	require.Equal(t, "1.014999", numericString(t, rows[1].SourceSnapshotUsd))
	require.Equal(t, "0", numericString(t, rows[1].AmountUsd))
	require.Equal(t, "confirmed", rows[1].DeliveryState)
}

func TestSettleStripeInvoiceAllocations_MissedWindowRecoversAfter96Hours(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_missed_window")
	start := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "open", 0)
	addStripeInvoice(t, db, stripe, organizationID, "in_future", end.AddDate(0, 0, 4), end.AddDate(0, 0, 5), "draft", 0)
	putDailySpend(t, db, organizationID, start, "2.345678")

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{
		Now: end.Add(120 * time.Hour),
	}))
	rows := listAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 2)
	require.Equal(t, int32(1), rows[0].Seq)
	require.Equal(t, "0", numericString(t, rows[0].SourceSnapshotUsd))
	require.Equal(t, "0", numericString(t, rows[0].AmountUsd))
	require.Equal(t, int32(2), rows[1].Seq)
	require.Equal(t, "2.345678", numericString(t, rows[1].SourceSnapshotUsd))
	require.Equal(t, "2.350000", numericString(t, rows[1].AmountUsd))
	require.Equal(t, "in_future", rows[1].DestinationInvoiceID.String)
	require.Len(t, stripe.itemInputs, 1)
	require.Equal(t, int64(235), stripe.itemInputs[0].AmountCents)
}

func TestSettleStripeInvoiceAllocations_AssignsEarliestDraftWithinTenant(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_earliest_draft")
	start := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "open", 0)
	addStripeInvoice(t, db, stripe, organizationID, "in_later", end.AddDate(0, 0, 3), end.AddDate(0, 0, 4), "draft", 0)
	addStripeInvoice(t, db, stripe, organizationID, "in_earlier", end.AddDate(0, 0, 2), end.AddDate(0, 0, 3), "draft", 0)
	putDailySpend(t, db, organizationID, start, "0.015000")

	otherID := "org-" + uuid.NewString()[:8]
	_, err := orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID: otherID, Name: "Other", Slug: otherID, WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	addStripeInvoice(t, db, stripe, otherID, "in_cross_tenant", end.AddDate(0, 0, 1), end.AddDate(0, 0, 2), "draft", 0)

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{
		Now: end.Add(52 * time.Hour),
	}))
	rows := listAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 1)
	require.Equal(t, "0.020000", numericString(t, rows[0].AmountUsd))
	require.Equal(t, "in_earlier", rows[0].DestinationInvoiceID.String)
	require.Len(t, stripe.itemInputs, 1)
	require.Equal(t, "in_earlier", stripe.itemInputs[0].InvoiceID)
}

func TestSettleStripeInvoiceAllocations_NegativeCarryCreditsPaidPortion(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_negative_credit")
	start := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "draft", 0)
	putDailySpend(t, db, organizationID, start, "2.005000")
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: end.Add(52 * time.Hour)}))

	putDailySpend(t, db, organizationID, start, "1.000000")
	state := stripe.invoices["in_original"]
	state.Status = "open"
	state.FinalizedAt = end.Add(time.Hour)
	state.AmountRemaining = 40
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: end.Add(76 * time.Hour)}))

	rows := listAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 2)
	require.Equal(t, "-1.010000", numericString(t, rows[1].AmountUsd))
	require.Equal(t, "in_original", rows[1].DestinationInvoiceID.String)
	require.Equal(t, "confirmed", rows[1].DeliveryState)
	require.Len(t, stripe.creditInputs, 1)
	require.Equal(t, int64(101), stripe.creditInputs[0].AmountCents)
	require.Equal(t, int64(61), stripe.creditInputs[0].CreditAmountCents)
}

func TestSettleStripeInvoiceAllocations_PaymentRaceRefetchesAndRetries(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_payment_race")
	start := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "draft", 0)
	putDailySpend(t, db, organizationID, start, "2.005000")
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: end.Add(52 * time.Hour)}))

	putDailySpend(t, db, organizationID, start, "1.000000")
	base := *stripe.invoices["in_original"]
	base.Status = "open"
	base.FinalizedAt = end.Add(time.Hour)
	stripe.getState = func(_ string, call int) *stripeclient.InvoiceState {
		state := base
		if call >= 3 {
			state.AmountRemaining = 0
		} else {
			state.AmountRemaining = 101
		}
		return &state
	}
	stripe.creditErrors = []error{errors.New("invoice payment changed"), nil}
	firstAttempt := end.Add(76 * time.Hour)
	require.Error(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: firstAttempt}))
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: firstAttempt.Add(time.Hour)}))
	require.Len(t, stripe.creditInputs, 1, "ambiguous credit must not drift within the idempotency window")
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: firstAttempt.Add(25 * time.Hour)}))
	require.Len(t, stripe.creditInputs, 2)
	require.Equal(t, int64(0), stripe.creditInputs[0].CreditAmountCents)
	require.Equal(t, int64(101), stripe.creditInputs[1].CreditAmountCents)
	require.NotEqual(t, stripe.creditInputs[0].IdempotencyKey, stripe.creditInputs[1].IdempotencyKey)
	require.LessOrEqual(t, len(stripe.creditInputs[1].IdempotencyKey), 255)
}

func TestSettleStripeInvoiceAllocations_ReconcilesAfter24Hours(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_reconcile")
	start := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "draft", 0)
	putDailySpend(t, db, organizationID, start, "1.250000")
	stripe.itemErrors = []error{errors.New("response lost")}
	firstAttempt := end.Add(48 * time.Hour)
	require.Error(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: firstAttempt}))

	rows := listAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 1)
	stripe.foundItem = &stripeclient.InvoiceItem{
		ID:          "ii_reconciled",
		InvoiceID:   "in_original",
		Currency:    "usd",
		AmountCents: 125,
	}
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: firstAttempt.Add(25 * time.Hour)}))
	rows = listAllocationFixtures(t, db, organizationID)
	require.Equal(t, "confirmed", rows[0].DeliveryState)
	require.Equal(t, "ii_reconciled", rows[0].StripeInvoiceItemID.String)
	require.True(t, rows[0].ReconciledAt.Valid)
	require.Equal(t, 1, stripe.findItems)
	require.Len(t, stripe.itemInputs, 1, "reconciled delivery must not be created twice")
}

func TestSettleStripeInvoiceAllocations_ConcurrentClaimsWriteOnce(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_concurrent")
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "draft", 0)
	putDailySpend(t, db, organizationID, start, "0.010000")
	now := end.Add(48 * time.Hour)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			errs <- activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: now})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Len(t, stripe.itemInputs, 1)
}

func TestSettleStripeInvoiceAllocations_MissingBaselineBecomesPositiveCarry(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_missing_then_carry")
	start := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "open", 0)
	addStripeInvoice(t, db, stripe, organizationID, "in_future", end.AddDate(0, 0, 2), end.AddDate(0, 0, 3), "draft", 0)

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: end.Add(52 * time.Hour)}))
	rows := listAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 1)
	require.Equal(t, "0", numericString(t, rows[0].SourceSnapshotUsd))
	require.Equal(t, "0", numericString(t, rows[0].AmountUsd))
	require.Equal(t, "confirmed", rows[0].DeliveryState)

	putDailySpend(t, db, organizationID, start, "0.015000")
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: end.Add(76 * time.Hour)}))
	rows = listAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 2)
	require.Equal(t, "0.020000", numericString(t, rows[1].AmountUsd))
	require.Equal(t, "in_future", rows[1].DestinationInvoiceID.String)
	require.NotEqual(t, "awaiting_source", rows[0].DeliveryState)
}

func TestSettleStripeInvoiceAllocations_TUMPositiveCarryIgnoresChatReadinessAndBindsExactBounds(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_tum_positive")
	start := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "open", 0)
	addStripeInvoice(t, db, stripe, organizationID, "in_future", end, end.AddDate(0, 1, 0), "draft", 0)
	addTUMCarry(t, db, organizationID, start, end, 35, "positive")

	otherID := "org-" + uuid.NewString()[:8]
	_, err := orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID: otherID, Name: "Other", Slug: otherID, WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	addStripeInvoice(t, db, stripe, otherID, "in_cross_tenant_exact", start, end, "open", 0)

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{
		Now:                                    end.Add(24 * time.Hour),
		RestrictOpenRouterToReadyOrganizations: true,
		OpenRouterReadyOrganizationIDs:         []string{},
	}))
	rows := listAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 1)
	require.Equal(t, "in_original", rows[0].OriginalInvoiceID.String)
	require.Equal(t, "in_future", rows[0].DestinationInvoiceID.String)
	require.Equal(t, "confirmed", rows[0].DeliveryState)
	require.Len(t, stripe.itemInputs, 1)
	require.Equal(t, int64(35), stripe.itemInputs[0].AmountCents)
	require.Equal(t, start, stripe.itemInputs[0].PeriodStart)
	require.Equal(t, end, stripe.itemInputs[0].PeriodEnd)
}

func TestSettleStripeInvoiceAllocations_TUMNegativeCarryCreditsOriginal(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_tum_negative")
	start := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "paid", 0)
	addTUMCarry(t, db, organizationID, start, end, -35, "negative")

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: end.Add(24 * time.Hour)}))
	rows := listAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 1)
	require.Equal(t, "in_original", rows[0].DestinationInvoiceID.String)
	require.Equal(t, "confirmed", rows[0].DeliveryState)
	require.Len(t, stripe.creditInputs, 1)
	require.Equal(t, int64(35), stripe.creditInputs[0].AmountCents)
	require.Equal(t, int64(35), stripe.creditInputs[0].CreditAmountCents)
}

func TestSettleStripeInvoiceAllocations_AmbiguousItemWaitsForVisibilityBeforeClosedRebind(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_closed_reconcile")
	start := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "draft", 0)
	addStripeInvoice(t, db, stripe, organizationID, "in_future", end.AddDate(0, 0, 2), end.AddDate(0, 0, 3), "draft", 0)
	putDailySpend(t, db, organizationID, start, "1.000000")
	stripe.itemErrors = []error{errors.New("response lost")}
	firstAttempt := end.Add(52 * time.Hour)
	require.Error(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: firstAttempt}))
	oldKey := stripe.itemInputs[0].IdempotencyKey

	state := stripe.invoices["in_original"]
	state.Status = "open"
	state.FinalizedAt = end.Add(time.Hour)
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: firstAttempt.Add(time.Hour)}))
	require.Zero(t, stripe.findItems, "eventual visibility cannot be decided inside 24 hours")
	rows := listAllocationFixtures(t, db, organizationID)
	require.Equal(t, "in_original", rows[0].DestinationInvoiceID.String)

	stripe.foundItem = &stripeclient.InvoiceItem{ID: "ii_visible_later", InvoiceID: "in_original", Currency: "usd", AmountCents: 100}
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: firstAttempt.Add(25 * time.Hour)}))
	rows = listAllocationFixtures(t, db, organizationID)
	require.Equal(t, "confirmed", rows[0].DeliveryState)
	require.Equal(t, "in_original", rows[0].DestinationInvoiceID.String)
	require.Equal(t, "ii_visible_later", rows[0].StripeInvoiceItemID.String)
	require.Equal(t, oldKey, stripe.itemInputs[0].IdempotencyKey)
	require.Len(t, stripe.itemInputs, 1, "visible old write must not be rebound or recreated")
}

func TestSettleStripeInvoiceAllocations_ProvenAbsentClosedItemRebindsWithRotatedKey(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_closed_rebind")
	start := time.Date(2026, time.November, 5, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "draft", 0)
	addStripeInvoice(t, db, stripe, organizationID, "in_future", end.AddDate(0, 0, 2), end.AddDate(0, 0, 3), "draft", 0)
	putDailySpend(t, db, organizationID, start, "1.000000")
	stripe.itemErrors = []error{errors.New("response lost"), errors.New("second response lost")}
	firstAttempt := end.Add(52 * time.Hour)
	require.Error(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: firstAttempt}))
	oldKey := stripe.itemInputs[0].IdempotencyKey

	state := stripe.invoices["in_original"]
	state.Status = "open"
	state.FinalizedAt = end.Add(time.Hour)
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: firstAttempt.Add(25 * time.Hour)}))
	rows := listAllocationFixtures(t, db, organizationID)
	require.False(t, rows[0].DestinationInvoiceID.Valid, "proven-absent closed destination is released")
	require.True(t, rows[0].ReconciledAt.Valid)
	require.LessOrEqual(t, len(rows[0].IdempotencyKey), 255)

	require.Error(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: firstAttempt.Add(25*time.Hour + 6*time.Minute)}))
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: firstAttempt.Add(51 * time.Hour)}))
	rows = listAllocationFixtures(t, db, organizationID)
	require.Equal(t, "in_future", rows[0].DestinationInvoiceID.String)
	require.Equal(t, "confirmed", rows[0].DeliveryState)
	require.Len(t, stripe.itemInputs, 3)
	require.NotEqual(t, oldKey, stripe.itemInputs[1].IdempotencyKey)
	require.NotEqual(t, stripe.itemInputs[1].IdempotencyKey, stripe.itemInputs[2].IdempotencyKey)
	require.LessOrEqual(t, len(stripe.itemInputs[2].IdempotencyKey), 255)
	require.Equal(t, 1, strings.Count(stripe.itemInputs[2].IdempotencyKey, ":retry:"))
}

func TestSettleStripeInvoiceAllocations_RefusesVoidCreditDestination(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_void_credit")
	start := time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "void", 0)
	addTUMCarry(t, db, organizationID, start, end, -10, "void")

	err := activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: end.Add(24 * time.Hour)})
	require.ErrorContains(t, err, `status "void" does not support credit notes`)
	require.Empty(t, stripe.creditInputs)
}
