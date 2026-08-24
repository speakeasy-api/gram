package stripe

import (
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestStubClientCreatesUniqueCustomersAndSessions(t *testing.T) {
	t.Parallel()

	c := NewStubClient(testenv.NewLogger(t), nil)
	first, err := c.CreateCustomer(t.Context(), CreateCustomerInput{IdempotencyKey: "customer:one"})
	require.NoError(t, err)
	second, err := c.CreateCustomer(t.Context(), CreateCustomerInput{IdempotencyKey: "customer:two"})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)

	replay, err := c.CreateCustomer(t.Context(), CreateCustomerInput{IdempotencyKey: "customer:one"})
	require.NoError(t, err)
	require.Equal(t, first.ID, replay.ID)
}

func TestStubClientCheckoutUsesPublicURLAndCompletesThroughStart(t *testing.T) {
	t.Parallel()

	publicURL, err := url.Parse("https://localhost:8000")
	require.NoError(t, err)
	c := NewStubClient(testenv.NewLogger(t), publicURL)
	local, ok := c.(LocalCheckout)
	require.True(t, ok)

	anchor := time.Date(2026, time.August, 25, 0, 0, 0, 0, time.UTC)
	checkout, err := c.CreateCheckoutSession(t.Context(), CreateCheckoutSessionInput{
		CustomerID:         "cus_local_org",
		OrganizationID:     "<ORG_ID>",
		OrganizationSlug:   "billing-test",
		SuccessURL:         "https://localhost:4000/billing-test/billing",
		CancelURL:          "https://localhost:4000/billing-test/billing",
		TrialEnd:           nil,
		BillingCycleAnchor: anchor,
		ExpiresAt:          time.Now().UTC().Add(time.Hour),
		IdempotencyKey:     "checkout-session:<ORG_ID>",
	})
	require.NoError(t, err)
	require.Equal(t, "https://localhost:8000/rpc/stripe.local-checkout?session="+checkout.ID, checkout.URL)

	open, err := c.GetCheckoutSession(t.Context(), checkout.ID)
	require.NoError(t, err)
	require.Equal(t, "open", open.Status)
	require.Equal(t, "cus_local_org", open.CustomerID)
	require.True(t, open.BillingCycleAnchor.Equal(anchor))

	result, err := local.CompleteCheckout(t.Context(), checkout.ID)
	require.NoError(t, err)
	require.Equal(t, "https://localhost:4000/billing-test/billing", result.SuccessURL)
	require.Equal(t, "checkout.session.completed", result.Event.Type)
	require.Equal(t, checkout.ID, result.Event.ObjectID)
	require.Equal(t, "cus_local_org", result.Event.CustomerID)
	require.NotEmpty(t, result.Event.SubscriptionID)

	complete, err := c.GetCheckoutSession(t.Context(), checkout.ID)
	require.NoError(t, err)
	require.Equal(t, "complete", complete.Status)
	require.Equal(t, result.Event.SubscriptionID, complete.SubscriptionID)
	require.Equal(t, "cus_local_org", complete.SubscriptionCustomerID)
	require.Equal(t, "active", complete.SubscriptionStatus)

	subscription, err := c.GetSubscription(t.Context(), result.Event.SubscriptionID)
	require.NoError(t, err)
	require.Equal(t, "cus_local_org", subscription.CustomerID)
	require.Equal(t, "active", subscription.Status)
	require.False(t, subscription.CancelAtPeriodEnd)

	replay, err := local.CompleteCheckout(t.Context(), checkout.ID)
	require.NoError(t, err)
	require.Equal(t, result.Event.ID, replay.Event.ID)
	require.Equal(t, result.Event.SubscriptionID, replay.Event.SubscriptionID)
}

func TestStubClientCheckoutIdempotencyAndExpiry(t *testing.T) {
	t.Parallel()

	c := NewStubClient(testenv.NewLogger(t), nil)
	input := CreateCheckoutSessionInput{
		CustomerID:         "cus_local_org",
		OrganizationID:     "<ORG_ID>",
		OrganizationSlug:   "billing-test",
		SuccessURL:         "https://app.example.test/billing-test/billing",
		CancelURL:          "https://app.example.test/billing-test/billing",
		TrialEnd:           nil,
		BillingCycleAnchor: time.Date(2026, time.August, 25, 0, 0, 0, 0, time.UTC),
		ExpiresAt:          time.Now().UTC().Add(-time.Minute),
		IdempotencyKey:     "checkout-session:<ORG_ID>:expired",
	}

	first, err := c.CreateCheckoutSession(t.Context(), input)
	require.NoError(t, err)
	replay, err := c.CreateCheckoutSession(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, first.ID, replay.ID)

	input.CustomerID = "cus_other"
	_, err = c.CreateCheckoutSession(t.Context(), input)
	require.ErrorContains(t, err, "reused with different checkout input")

	state, err := c.GetCheckoutSession(t.Context(), first.ID)
	require.NoError(t, err)
	require.Equal(t, "expired", state.Status)

	local, ok := c.(LocalCheckout)
	require.True(t, ok)
	_, err = local.CompleteCheckout(t.Context(), first.ID)
	require.ErrorIs(t, err, ErrLocalCheckoutSessionExpired)
}

func TestStubClientSubscriptionLifecycleAndPortal(t *testing.T) {
	t.Parallel()

	c := NewStubClient(testenv.NewLogger(t), nil)
	local, ok := c.(LocalCheckout)
	require.True(t, ok)

	trialEnd := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	checkout, err := c.CreateCheckoutSession(t.Context(), CreateCheckoutSessionInput{
		CustomerID:         "cus_local_org",
		SuccessURL:         "https://app.example.test/billing-test/billing",
		BillingCycleAnchor: trialEnd,
		ExpiresAt:          time.Now().UTC().Add(time.Hour),
		TrialEnd:           &trialEnd,
		IdempotencyKey:     "checkout-session:trial",
	})
	require.NoError(t, err)

	result, err := local.CompleteCheckout(t.Context(), checkout.ID)
	require.NoError(t, err)

	canceled, err := c.SetSubscriptionCancelAtPeriodEnd(t.Context(), SetSubscriptionCancelAtPeriodEndInput{
		SubscriptionID:    result.Event.SubscriptionID,
		CancelAtPeriodEnd: true,
	})
	require.NoError(t, err)
	require.True(t, canceled.CancelAtPeriodEnd)
	require.False(t, canceled.CancelAt.IsZero())
	require.Equal(t, "trialing", canceled.Status)

	resumed, err := c.SetSubscriptionCancelAtPeriodEnd(t.Context(), SetSubscriptionCancelAtPeriodEndInput{
		SubscriptionID:    result.Event.SubscriptionID,
		CancelAtPeriodEnd: false,
	})
	require.NoError(t, err)
	require.False(t, resumed.CancelAtPeriodEnd)
	require.True(t, resumed.CancelAt.IsZero())

	portal, err := c.CreatePortalSession(t.Context(), CreatePortalSessionInput{
		CustomerID: "cus_local_org",
		ReturnURL:  "https://app.example.test/billing-test/billing",
	})
	require.NoError(t, err)
	require.Equal(t, "cus_local_org", portal.CustomerID)
	require.Equal(t, "https://app.example.test/billing-test/billing", portal.URL)
}

func TestStubClientInvoiceAllocationsAndMeters(t *testing.T) {
	t.Parallel()

	c := NewStubClient(testenv.NewLogger(t), nil)
	item, err := c.CreateInvoiceItem(t.Context(), CreateInvoiceItemInput{
		InvoiceID:     "in_local",
		AllocationKey: "allocation",
		AmountCents:   1200,
	})
	require.NoError(t, err)
	foundItem, err := c.FindInvoiceItem(t.Context(), FindInvoiceAllocationInput{
		InvoiceID:     "in_local",
		AllocationKey: "allocation",
		AmountCents:   1200,
	})
	require.NoError(t, err)
	require.Equal(t, item.ID, foundItem.ID)

	note, err := c.CreateCreditNote(t.Context(), CreateCreditNoteInput{
		InvoiceID:     "in_local",
		AllocationKey: "credit",
		AmountCents:   400,
	})
	require.NoError(t, err)
	foundNote, err := c.FindCreditNote(t.Context(), FindInvoiceAllocationInput{
		InvoiceID:     "in_local",
		AllocationKey: "credit",
		AmountCents:   400,
	})
	require.NoError(t, err)
	require.Equal(t, note.ID, foundNote.ID)

	require.NoError(t, c.CreateMeterEvent(t.Context(), CreateMeterEventInput{
		CustomerID: "cus_local_org",
		Value:      12,
	}))
	total, err := c.GetMeterEventSummary(t.Context(), GetMeterEventSummaryInput{CustomerID: "cus_local_org"})
	require.NoError(t, err)
	require.Equal(t, 12.0, total)
}
