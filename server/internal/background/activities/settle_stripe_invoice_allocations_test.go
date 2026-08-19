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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	backgroundrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
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
	return setupAllocationTestWithChatKey(t, name, true)
}

func setupAllocationTestWithChatKey(
	t *testing.T,
	name string,
	withChatKey bool,
) (*pgxpool.Pool, string, *allocationStripeMock, *activities.SettleStripeInvoiceAllocations) {
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
	if withChatKey {
		createChatKeyAt(t, db, organizationID, time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC))
	}
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
	putDailySpendForKey(t, db, organizationID, openrouter.KeyTypeChat, day, amount)
}

func putDailySpendForKey(t *testing.T, db *pgxpool.Pool, organizationID string, keyType openrouter.KeyType, day time.Time, amount string) {
	t.Helper()
	var spend pgtype.Numeric
	require.NoError(t, spend.Scan(amount))
	require.NoError(t, backgroundrepo.New(db).UpsertOpenRouterDailySpend(t.Context(), backgroundrepo.UpsertOpenRouterDailySpendParams{
		TargetOrganizationID: organizationID,
		TargetKeyType:        string(keyType),
		TargetDay:            pgtype.Date{Time: day.UTC(), InfinityModifier: pgtype.Finite, Valid: true},
		TargetSpendUsd:       spend,
	}))
}

func createChatKeyAt(t *testing.T, db *pgxpool.Pool, organizationID string, createdAt time.Time) {
	t.Helper()
	createInferenceKeyAt(t, db, organizationID, openrouter.KeyTypeChat, createdAt)
}

func createInferenceKeyAt(t *testing.T, db *pgxpool.Pool, organizationID string, keyType openrouter.KeyType, createdAt time.Time) {
	t.Helper()
	hashRune := 'a'
	if keyType == openrouter.KeyTypeInternal {
		hashRune = 'b'
	}
	createOpenRouterSpendTarget(t, db, organizationID, keyType, hashRune)
	require.NoError(t, backgroundrepo.New(db).SetOpenRouterAPIKeyCreatedAtFixture(
		t.Context(), backgroundrepo.SetOpenRouterAPIKeyCreatedAtFixtureParams{
			CreatedAt:      pgtype.Timestamptz{Time: createdAt.UTC(), InfinityModifier: pgtype.Finite, Valid: true},
			OrganizationID: organizationID,
			KeyType:        string(keyType),
		},
	))
}

func setChatKeyCreatedAt(t *testing.T, db *pgxpool.Pool, organizationID string, createdAt time.Time) {
	t.Helper()
	require.NoError(t, backgroundrepo.New(db).SetOpenRouterAPIKeyCreatedAtFixture(
		t.Context(), backgroundrepo.SetOpenRouterAPIKeyCreatedAtFixtureParams{
			CreatedAt:      pgtype.Timestamptz{Time: createdAt.UTC(), InfinityModifier: pgtype.Finite, Valid: true},
			OrganizationID: organizationID,
			KeyType:        "chat",
		},
	))
}

func setChatKeyDeletedAt(t *testing.T, db *pgxpool.Pool, organizationID string, deletedAt time.Time) {
	t.Helper()
	require.NoError(t, backgroundrepo.New(db).SetOpenRouterAPIKeyDeletedAtFixture(
		t.Context(), backgroundrepo.SetOpenRouterAPIKeyDeletedAtFixtureParams{
			DeletedAt:      pgtype.Timestamptz{Time: deletedAt.UTC(), InfinityModifier: pgtype.Finite, Valid: true},
			OrganizationID: organizationID,
			KeyType:        "chat",
		},
	))
}

func addOpenRouterCarryFixture(
	t *testing.T,
	db *pgxpool.Pool,
	organizationID string,
	day time.Time,
	snapshot string,
	amount string,
	invoiceID string,
) {
	t.Helper()
	var snapshotNumeric pgtype.Numeric
	require.NoError(t, snapshotNumeric.Scan(snapshot))
	var amountNumeric pgtype.Numeric
	require.NoError(t, amountNumeric.Scan(amount))
	now := time.Now().UTC()
	_, err := backgroundrepo.New(db).CreateOpenRouterInvoiceAllocation(
		t.Context(), backgroundrepo.CreateOpenRouterInvoiceAllocationParams{
			OrganizationID:       pgtype.Text{String: organizationID, Valid: true},
			SourceKey:            day.UTC().Format(time.DateOnly) + ":chat",
			Seq:                  2,
			SourceDay:            pgtype.Date{Time: day.UTC(), InfinityModifier: pgtype.Finite, Valid: true},
			SourceSnapshotUsd:    snapshotNumeric,
			AmountUsd:            amountNumeric,
			OriginalInvoiceID:    pgtype.Text{String: invoiceID, Valid: true},
			DestinationInvoiceID: pgtype.Text{String: invoiceID, Valid: true},
			IdempotencyKey:       "openrouter:" + organizationID + ":" + day.UTC().Format(time.DateOnly) + ":chat:2",
			DeliveryState:        "confirmed",
			ConfirmedAt:          pgtype.Timestamptz{Time: now, InfinityModifier: pgtype.Finite, Valid: true},
		},
	)
	require.NoError(t, err)
}

func listAllAllocationFixtures(t *testing.T, db *pgxpool.Pool, organizationID string) []backgroundrepo.ListStripeInvoiceAllocationsFixtureRow {
	t.Helper()
	rows, err := backgroundrepo.New(db).ListStripeInvoiceAllocationsFixture(
		t.Context(), pgtype.Text{String: organizationID, Valid: true})
	require.NoError(t, err)
	return rows
}

func listAllocationFixturesForKeyType(t *testing.T, db *pgxpool.Pool, organizationID string, keyType openrouter.KeyType) []backgroundrepo.ListStripeInvoiceAllocationsFixtureRow {
	t.Helper()
	rows := listAllAllocationFixtures(t, db, organizationID)
	filtered := make([]backgroundrepo.ListStripeInvoiceAllocationsFixtureRow, 0, len(rows))
	for _, row := range rows {
		if row.SourceKind != "openrouter_daily_spend" || strings.HasSuffix(row.SourceKey, ":"+string(keyType)) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// Existing allocation tests predate per-key billing and intentionally assert
// only the chat rounding chain. Multi-key edge tests use the all/key helpers.
func listChatAllocationFixtures(t *testing.T, db *pgxpool.Pool, organizationID string) []backgroundrepo.ListStripeInvoiceAllocationsFixtureRow {
	t.Helper()
	return listAllocationFixturesForKeyType(t, db, organizationID, openrouter.KeyTypeChat)
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

func TestSettleStripeInvoiceAllocations_BillsEveryCanonicalKeyTypeExactlyOnce(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_billable_key_types")
	start := time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	createInferenceKeyAt(t, db, organizationID, openrouter.KeyTypeInternal, start.Add(-time.Hour))
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "draft", 0)
	putDailySpendForKey(t, db, organizationID, openrouter.KeyTypeChat, start, "1.000000")
	putDailySpendForKey(t, db, organizationID, openrouter.KeyTypeInternal, start, "2.000000")
	now := end.Add(48 * time.Hour)

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: now}))
	rows := listAllAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 2)
	require.Equal(t, start.Format(time.DateOnly)+":chat", rows[0].SourceKey)
	require.Equal(t, start.Format(time.DateOnly)+":internal", rows[1].SourceKey)
	require.NotEqual(t, rows[0].IdempotencyKey, rows[1].IdempotencyKey)
	require.ElementsMatch(t, []int64{100, 200}, []int64{stripe.itemInputs[0].AmountCents, stripe.itemInputs[1].AmountCents})

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: now.Add(time.Hour)}))
	require.Len(t, listAllAllocationFixtures(t, db, organizationID), 2)
	require.Len(t, stripe.itemInputs, 2)

	addStripeInvoice(t, db, stripe, organizationID, "in_future", end.AddDate(0, 0, 2), end.AddDate(0, 0, 3), "draft", 0)
	stripe.invoices["in_original"].Status = "open"
	stripe.invoices["in_original"].FinalizedAt = end.Add(time.Hour)
	putDailySpendForKey(t, db, organizationID, openrouter.KeyTypeChat, start, "1.500000")
	putDailySpendForKey(t, db, organizationID, openrouter.KeyTypeInternal, start, "2.500000")
	reconcileAt := end.Add(72 * time.Hour)
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: reconcileAt}))
	rows = listAllAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 4)
	for _, keyType := range openrouter.BillableKeyTypeStrings() {
		sourceKey := start.Format(time.DateOnly) + ":" + keyType
		count := 0
		for _, row := range rows {
			if row.SourceKey == sourceKey {
				count++
			}
		}
		require.Equal(t, 2, count, "each billable key has one baseline and one carry")
	}
	require.Len(t, stripe.itemInputs, 4)

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: reconcileAt.Add(time.Hour)}))
	require.Len(t, listAllAllocationFixtures(t, db, organizationID), 4)
	require.Len(t, stripe.itemInputs, 4)
}

func TestSettleStripeInvoiceAllocations_RequiresMatchingBillablePolicyForReadyOrganizations(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_policy_version")
	start := time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "draft", 0)
	putDailySpend(t, db, organizationID, start, "1.000000")

	args := activities.SettleStripeInvoiceAllocationsArgs{
		Now:                                    end.Add(48 * time.Hour),
		RestrictOpenRouterToReadyOrganizations: true,
		OpenRouterReadyOrganizationIDs:         []string{organizationID},
		OpenRouterBillableKeyPolicyFingerprint: "internal\x00chat",
	}
	require.NoError(t, activity.Do(t.Context(), args))
	require.Empty(t, listAllAllocationFixtures(t, db, organizationID))

	args.OpenRouterBillableKeyPolicyFingerprint = openrouter.BillableKeyPolicyFingerprint()
	require.NoError(t, activity.Do(t.Context(), args))
	require.Len(t, listAllAllocationFixtures(t, db, organizationID), len(openrouter.BillableKeyTypes()))
	require.Len(t, stripe.itemInputs, 1)
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
	require.Empty(t, listChatAllocationFixtures(t, db, organizationID))

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{
		Now: end.Add(48 * time.Hour),
	}))
	rows := listChatAllocationFixtures(t, db, organizationID)
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
	rows = listChatAllocationFixtures(t, db, organizationID)
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
	rows := listChatAllocationFixtures(t, db, organizationID)
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
	rows := listChatAllocationFixtures(t, db, organizationID)
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

	rows := listChatAllocationFixtures(t, db, organizationID)
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

	rows := listChatAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 1)
	stripe.foundItem = &stripeclient.InvoiceItem{
		ID:          "ii_reconciled",
		InvoiceID:   "in_original",
		Currency:    "usd",
		AmountCents: 125,
	}
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: firstAttempt.Add(25 * time.Hour)}))
	rows = listChatAllocationFixtures(t, db, organizationID)
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
	rows := listChatAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 1)
	require.Equal(t, "0", numericString(t, rows[0].SourceSnapshotUsd))
	require.Equal(t, "0", numericString(t, rows[0].AmountUsd))
	require.Equal(t, "confirmed", rows[0].DeliveryState)

	putDailySpend(t, db, organizationID, start, "0.015000")
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: end.Add(76 * time.Hour)}))
	rows = listChatAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 2)
	require.Equal(t, "0.020000", numericString(t, rows[1].AmountUsd))
	require.Equal(t, "in_future", rows[1].DestinationInvoiceID.String)
	require.NotEqual(t, "awaiting_source", rows[0].DeliveryState)
}

func TestSettleStripeInvoiceAllocations_MissingFinalSourceRemainsRecoverable(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_missing_final")
	start := time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "open", 0)
	addStripeInvoice(t, db, stripe, organizationID, "in_future", end.AddDate(0, 0, 2), end.AddDate(0, 0, 3), "draft", 0)

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: end.Add(76 * time.Hour)}))
	rows := listChatAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 1, "missing durable spend must not be finalized as a zero carry")
	require.Equal(t, int32(1), rows[0].Seq)
	require.Equal(t, "0", numericString(t, rows[0].SourceSnapshotUsd))

	putDailySpend(t, db, organizationID, start, "0.015000")
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: end.Add(77 * time.Hour)}))
	rows = listChatAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 2)
	require.Equal(t, int32(2), rows[1].Seq)
	require.Equal(t, "0.020000", numericString(t, rows[1].AmountUsd))
	require.Equal(t, "confirmed", rows[1].DeliveryState)
	require.Len(t, stripe.itemInputs, 1)
}

func TestSettleStripeInvoiceAllocations_PreKeyDaysFinalizeAsDurableZero(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_pre_key_zero")
	start := time.Date(2026, time.August, 7, 0, 0, 0, 0, time.UTC)
	keyDay := start.AddDate(0, 0, 2)
	end := start.AddDate(0, 0, 3)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "draft", 0)
	addStripeInvoice(t, db, stripe, organizationID, "in_future", end.AddDate(0, 0, 2), end.AddDate(0, 0, 3), "draft", 0)
	setChatKeyCreatedAt(t, db, organizationID, keyDay.Add(12*time.Hour))
	putDailySpend(t, db, organizationID, keyDay, "0.015000")
	now := end.Add(76 * time.Hour)

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: now}))
	rows := listChatAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 6)
	require.Equal(t, int32(2), rows[1].Seq)
	require.Equal(t, "0", numericString(t, rows[1].SourceSnapshotUsd))
	require.Equal(t, "0", numericString(t, rows[1].AmountUsd))
	require.Equal(t, int32(2), rows[3].Seq)
	require.Equal(t, "0", numericString(t, rows[3].SourceSnapshotUsd))
	require.Equal(t, "0", numericString(t, rows[3].AmountUsd))
	require.Equal(t, int32(2), rows[5].Seq)
	require.Equal(t, "0.015000", numericString(t, rows[5].SourceSnapshotUsd))
	require.Equal(t, "0.020000", numericString(t, rows[5].AmountUsd))
	require.Len(t, stripe.itemInputs, 1)

	candidates, err := backgroundrepo.New(db).ListStripeInvoiceBillingOrganizations(
		t.Context(), backgroundrepo.ListStripeInvoiceBillingOrganizationsParams{
			BillableKeyTypes: openrouter.BillableKeyTypeStrings(),
			Now:              pgtype.Timestamptz{Time: now.Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		})
	require.NoError(t, err)
	require.Empty(t, candidates, "authoritative pre-key zeroes must finish the invoice candidate")
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: now.Add(time.Hour)}))
	require.Len(t, listChatAllocationFixtures(t, db, organizationID), 6)
	require.Len(t, stripe.itemInputs, 1)
}

func TestSettleStripeInvoiceAllocations_NoKeyDaysFinalizeAsDurableZero(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTestWithChatKey(t, "stripe_alloc_no_key_zero", false)
	start := time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 2)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "draft", 0)
	now := end.Add(76 * time.Hour)

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: now}))
	rows := listChatAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 4)
	for _, row := range rows {
		require.Equal(t, "0", numericString(t, row.SourceSnapshotUsd))
		require.Equal(t, "0", numericString(t, row.AmountUsd))
		require.Equal(t, "confirmed", row.DeliveryState)
	}
	require.Empty(t, stripe.itemInputs)

	candidates, err := backgroundrepo.New(db).ListStripeInvoiceBillingOrganizations(
		t.Context(), backgroundrepo.ListStripeInvoiceBillingOrganizationsParams{
			BillableKeyTypes: openrouter.BillableKeyTypeStrings(),
			Now:              pgtype.Timestamptz{Time: now.Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		})
	require.NoError(t, err)
	require.Empty(t, candidates, "an organization without a chat key cannot have chat spend")
}

func TestSettleStripeInvoiceAllocations_PostDeletionDaysFinalizeAsDurableZero(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_deleted_key_zero")
	start := time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC)
	deletionDay := start.AddDate(0, 0, 1)
	end := start.AddDate(0, 0, 3)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "open", 0)
	addStripeInvoice(t, db, stripe, organizationID, "in_future", end.AddDate(0, 0, 2), end.AddDate(0, 0, 3), "draft", 0)
	setChatKeyCreatedAt(t, db, organizationID, start.Add(-time.Hour))
	setChatKeyDeletedAt(t, db, organizationID, deletionDay.Add(12*time.Hour))
	putDailySpend(t, db, organizationID, start, "0.010000")
	putDailySpend(t, db, organizationID, deletionDay, "0.020000")
	now := end.Add(76 * time.Hour)

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: now}))
	rows := listChatAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 6)
	require.Equal(t, "0.010000", numericString(t, rows[1].SourceSnapshotUsd))
	require.Equal(t, "0.020000", numericString(t, rows[3].SourceSnapshotUsd), "deletion-day spend remains billable")
	require.Equal(t, "0", numericString(t, rows[5].SourceSnapshotUsd), "post-deletion day is an authoritative zero")
	require.Equal(t, int32(2), rows[5].Seq)

	candidates, err := backgroundrepo.New(db).ListStripeInvoiceBillingOrganizations(
		t.Context(), backgroundrepo.ListStripeInvoiceBillingOrganizationsParams{
			BillableKeyTypes: openrouter.BillableKeyTypeStrings(),
			Now:              pgtype.Timestamptz{Time: now.Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		})
	require.NoError(t, err)
	require.Empty(t, candidates, "post-deletion zeroes must finish the invoice candidate")
}

func TestSettleStripeInvoiceAllocations_UnresolvedKeyDoesNotBlockAnotherKeyCarry(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_independent_key_readiness")
	start := time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 2)
	createInferenceKeyAt(t, db, organizationID, openrouter.KeyTypeInternal, start.Add(-time.Hour))
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "open", 0)
	addStripeInvoice(t, db, stripe, organizationID, "in_future", end.AddDate(0, 0, 2), end.AddDate(0, 0, 3), "draft", 0)
	putDailySpend(t, db, organizationID, start, "0.010000")
	putDailySpend(t, db, organizationID, start.AddDate(0, 0, 1), "0.020000")

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: end.Add(76 * time.Hour)}))
	rows := listAllAllocationFixtures(t, db, organizationID)
	chatCarries := 0
	internalCarries := 0
	for _, row := range rows {
		if row.Seq != 2 {
			continue
		}
		if strings.HasSuffix(row.SourceKey, ":chat") {
			chatCarries++
		}
		if strings.HasSuffix(row.SourceKey, ":internal") {
			internalCarries++
		}
	}
	require.Equal(t, 2, chatCarries, "complete chat chain must reconcile")
	require.Zero(t, internalCarries, "unresolved internal chain must remain recoverable")
}

func TestSettleStripeInvoiceAllocations_LateMiddleDayReconcilesExistingLaterCarry(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_late_middle_day")
	start := time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)
	middleDay := start.AddDate(0, 0, 1)
	lastDay := start.AddDate(0, 0, 2)
	end := start.AddDate(0, 0, 3)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "draft", 0)
	putDailySpend(t, db, organizationID, start, "0.004000")
	putDailySpend(t, db, organizationID, lastDay, "0.004000")
	now := end.Add(76 * time.Hour)

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: now}))
	rows := listChatAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 4)
	require.Equal(t, start.Format(time.DateOnly)+":chat", rows[1].SourceKey)
	require.Equal(t, int32(2), rows[1].Seq)
	require.Equal(t, "0", numericString(t, rows[1].AmountUsd))
	addOpenRouterCarryFixture(t, db, organizationID, lastDay, "0.004000", "0.010000", "in_original")

	putDailySpend(t, db, organizationID, middleDay, "0.004000")
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: now.Add(time.Hour)}))
	rows = listChatAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 6)
	require.Equal(t, middleDay.Format(time.DateOnly)+":chat", rows[3].SourceKey)
	require.Equal(t, int32(2), rows[3].Seq)
	require.Equal(t, "0.004000", numericString(t, rows[3].SourceSnapshotUsd))
	require.Equal(t, "0", numericString(t, rows[3].AmountUsd))
	require.Equal(t, lastDay.Format(time.DateOnly)+":chat", rows[5].SourceKey)
	require.Equal(t, "0.010000", numericString(t, rows[5].AmountUsd), "the existing later carry stays immutable")

	require.Equal(t, "0", numericString(t, rows[1].AmountUsd))
	require.Equal(t, "0", numericString(t, rows[3].AmountUsd))
	require.Equal(t, "0.010000", numericString(t, rows[5].AmountUsd), "three 0.004 USD days round once to one cycle cent")
	require.Empty(t, stripe.itemInputs)

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: now.Add(2 * time.Hour)}))
	require.Len(t, listChatAllocationFixtures(t, db, organizationID), 6)
}

func TestSettleStripeInvoiceAllocations_RoundsCumulativeCycleTotalsAndDrainsBatch(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_cumulative_rounding")
	start := time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC)
	secondDay := start.AddDate(0, 0, 1)
	end := start.AddDate(0, 0, 2)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "draft", 0)
	addStripeInvoice(t, db, stripe, organizationID, "in_future", end.AddDate(0, 0, 2), end.AddDate(0, 0, 3), "draft", 0)
	putDailySpend(t, db, organizationID, start, "0.004000")
	putDailySpend(t, db, organizationID, secondDay, "0.004000")

	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: end.Add(48 * time.Hour)}))
	rows := listChatAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 2)
	require.Equal(t, "0", numericString(t, rows[0].AmountUsd))
	require.Equal(t, "0.010000", numericString(t, rows[1].AmountUsd))
	require.Len(t, stripe.itemInputs, 1, "fractional daily rows must round once at the cumulative cycle boundary")
	require.Equal(t, int64(1), stripe.itemInputs[0].AmountCents)

	putDailySpend(t, db, organizationID, start, "0.006000")
	putDailySpend(t, db, organizationID, secondDay, "0.006000")
	state := stripe.invoices["in_original"]
	state.Status = "open"
	state.FinalizedAt = end.Add(time.Hour)
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: end.Add(72 * time.Hour)}))

	rows = listChatAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 4)
	require.Equal(t, "0.010000", numericString(t, rows[1].AmountUsd))
	require.Equal(t, "0.010000", numericString(t, rows[2].AmountUsd))
	require.Equal(t, "-0.010000", numericString(t, rows[3].AmountUsd))
	require.Len(t, stripe.itemInputs, 2, "the bounded batch must drain both pending carry allocations")
	require.Len(t, stripe.creditInputs, 1)
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
	rows := listChatAllocationFixtures(t, db, organizationID)
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
	rows := listChatAllocationFixtures(t, db, organizationID)
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
	rows := listChatAllocationFixtures(t, db, organizationID)
	require.Equal(t, "in_original", rows[0].DestinationInvoiceID.String)

	stripe.foundItem = &stripeclient.InvoiceItem{ID: "ii_visible_later", InvoiceID: "in_original", Currency: "usd", AmountCents: 100}
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: firstAttempt.Add(25 * time.Hour)}))
	rows = listChatAllocationFixtures(t, db, organizationID)
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
	rows := listChatAllocationFixtures(t, db, organizationID)
	require.False(t, rows[0].DestinationInvoiceID.Valid, "proven-absent closed destination is released")
	require.True(t, rows[0].ReconciledAt.Valid)
	require.LessOrEqual(t, len(rows[0].IdempotencyKey), 255)

	require.Error(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: firstAttempt.Add(25*time.Hour + 6*time.Minute)}))
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: firstAttempt.Add(51 * time.Hour)}))
	rows = listChatAllocationFixtures(t, db, organizationID)
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

func TestM3FreezeObservationAndCarrySettlementLifecycle(t *testing.T) {
	t.Parallel()

	anchor := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	reporter, meterStripe, db, organizationID := setupTUMStripeReporter(t, "m3_freeze_carry_lifecycle", "payg", anchor)
	cycleEnd := anchor.AddDate(0, 1, 0)
	upsertTUMCycle(t, db, organizationID, anchor, cycleEnd, 2_000_000, nil)

	allocationStripe := &allocationStripeMock{invoices: make(map[string]*stripeclient.InvoiceState)}
	allocator := activities.NewSettleStripeInvoiceAllocations(testenv.NewLogger(t), db, allocationStripe)
	addStripeInvoice(t, db, allocationStripe, organizationID, "invoice_original", anchor, cycleEnd, "draft", 0)
	addStripeInvoice(t, db, allocationStripe, organizationID, "invoice_carry", cycleEnd, cycleEnd.AddDate(0, 1, 0), "draft", 0)
	createChatKeyAt(t, db, organizationID, anchor.Add(-time.Hour))
	putDailySpend(t, db, organizationID, anchor, "1.005000")

	meterStripe.On("CreateMeterEvent", mock.Anything, mock.MatchedBy(func(input stripeclient.CreateMeterEventInput) bool {
		return input.Value == 2_000_000 && input.Timestamp.Equal(cycleEnd.Add(-time.Second)) && input.IdempotencyKey != ""
	})).Return(nil).Once()
	freezeAt := cycleEnd.Add(48 * time.Hour)
	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             freezeAt,
	}))
	require.NoError(t, allocator.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: freezeAt}))

	finalizedAt := cycleEnd.Add(72 * time.Hour)
	upsertTUMCycle(t, db, organizationID, anchor, cycleEnd, 2_100_000, &finalizedAt)
	putDailySpend(t, db, organizationID, anchor, "1.255000")
	_, err := usagerepo.New(db).UpsertStripeInvoice(t.Context(), usagerepo.UpsertStripeInvoiceParams{
		StripeInvoiceID:      "invoice_original",
		OrganizationID:       pgtype.Text{String: organizationID, Valid: true},
		StripeCustomerID:     "cus_" + organizationID,
		StripeSubscriptionID: "sub_" + organizationID,
		ServicePeriodStart:   pgtype.Timestamptz{Time: anchor, InfinityModifier: pgtype.Finite, Valid: true},
		ServicePeriodEnd:     pgtype.Timestamptz{Time: cycleEnd, InfinityModifier: pgtype.Finite, Valid: true},
		InvoiceState:         "open",
		FinalizedAt:          pgtype.Timestamptz{Time: cycleEnd.Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)
	allocationStripe.invoices["invoice_original"].Status = "open"
	allocationStripe.invoices["invoice_original"].FinalizedAt = cycleEnd.Add(time.Hour)

	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             finalizedAt,
	}))
	require.NoError(t, allocator.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: finalizedAt}))

	// Re-running both deterministic passes proves the immutable final source is
	// allocated once even after the observation window has closed.
	replayAt := finalizedAt.Add(24 * time.Hour)
	require.NoError(t, reporter.Do(t.Context(), activities.ReportTUMUsageToStripeInput{
		OrganizationIDs: []string{organizationID},
		Now:             replayAt,
	}))
	require.NoError(t, allocator.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: replayAt}))

	cycles, err := usagerepo.New(db).ListBillingCycleUsage(t.Context(), organizationID)
	require.NoError(t, err)
	require.Len(t, cycles, 1)
	require.Equal(t, int64(2_000_000), cycles[0].BilledTumTokens.Int64)
	require.Equal(t, int64(2_100_000), cycles[0].TumTokens)

	tumCarries, err := usagerepo.New(db).ListTUMCarryAllocationsFixture(t.Context(), pgtype.Text{String: organizationID, Valid: true})
	require.NoError(t, err)
	require.Len(t, tumCarries, 1)
	require.Equal(t, int64(100_000), tumCarries[0].DeltaTokens.Int64)
	require.Equal(t, "0.040000", numericString(t, tumCarries[0].AmountUsd))

	allocations := listChatAllocationFixtures(t, db, organizationID)
	amountsBySource := make(map[string][]string)
	for _, allocation := range allocations {
		require.Equal(t, "confirmed", allocation.DeliveryState)
		amount := numericString(t, allocation.AmountUsd)
		if amount == "0.250000" || amount == "0.040000" {
			require.Equal(t, "invoice_carry", allocation.DestinationInvoiceID.String)
		}
		if amount != "0" {
			amountsBySource[allocation.SourceKind] = append(amountsBySource[allocation.SourceKind], amount)
		}
	}
	require.ElementsMatch(t, []string{"1.010000", "0.250000"}, amountsBySource["openrouter_daily_spend"])
	require.Equal(t, []string{"0.040000"}, amountsBySource["tum_cycle"])

	require.Len(t, allocationStripe.itemInputs, 3)
	itemCents := make([]int64, 0, len(allocationStripe.itemInputs))
	for _, input := range allocationStripe.itemInputs {
		itemCents = append(itemCents, input.AmountCents)
		if input.AmountCents == 25 || input.AmountCents == 4 {
			require.Equal(t, "invoice_carry", input.InvoiceID)
		}
	}
	require.ElementsMatch(t, []int64{101, 25, 4}, itemCents)
	require.Empty(t, allocationStripe.creditInputs)
	meterStripe.AssertExpectations(t)
}

// freezeInvoice writes each day on the pool with no enclosing transaction, so a
// pass that dies partway leaves the days it already wrote. Those snapshots are
// what the carry pass reconciles against, so a later pass has to continue the
// cumulative chain from them rather than from spend backfilled since.
func TestSettleStripeInvoiceAllocations_BackfilledSpendKeepsFrozenChain(t *testing.T) {
	t.Parallel()
	db, organizationID, stripe, activity := setupAllocationTest(t, "stripe_alloc_backfill_chain")
	start := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	secondDay := start.AddDate(0, 0, 1)
	end := start.AddDate(0, 0, 2)
	addStripeInvoice(t, db, stripe, organizationID, "in_original", start, end, "draft", 0)
	putDailySpend(t, db, organizationID, start, "0.004000")
	putDailySpend(t, db, organizationID, secondDay, "0.004000")

	now := end.Add(48 * time.Hour)
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: now}))

	// Stand in for a pass that died after day one.
	deleted, err := backgroundrepo.New(db).DeleteStripeInvoiceAllocationFixture(t.Context(),
		backgroundrepo.DeleteStripeInvoiceAllocationFixtureParams{
			OrganizationID: pgtype.Text{String: organizationID, Valid: true},
			SourceKind:     "openrouter_daily_spend",
			SourceKey:      secondDay.Format(time.DateOnly) + ":chat",
			Seq:            1,
		})
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted, "the partial-freeze simulation must remove exactly one day")

	// Day one's spend lands late, after its row was frozen.
	putDailySpend(t, db, organizationID, start, "0.006000")
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: now}))

	rows := listChatAllocationFixtures(t, db, organizationID)
	require.Len(t, rows, 2)
	require.Equal(t, "0.004000", numericString(t, rows[0].SourceSnapshotUsd),
		"the frozen day keeps the snapshot it was written with, not the backfill")
	require.Equal(t, "0", numericString(t, rows[0].AmountUsd))
	require.Equal(t, "0.004000", numericString(t, rows[1].SourceSnapshotUsd))
	require.Equal(t, "0.010000", numericString(t, rows[1].AmountUsd),
		"day two carries the cent the frozen chain rounds up to")
}
