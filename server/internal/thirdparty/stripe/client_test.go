package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	stripesdk "github.com/stripe/stripe-go/v85"
	stripewebhook "github.com/stripe/stripe-go/v85/webhook"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestCatalogValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		catalog Catalog
		wantErr string
	}{
		{
			name: "valid",
			catalog: Catalog{
				PriceIDTUM:            "price_test",
				MeterIDTUM:            "mtr_test",
				MeterEventName:        "tum",
				PortalConfigurationID: "bpc_test",
			},
		},
		{
			name:    "missing price",
			catalog: Catalog{MeterIDTUM: "mtr_test", MeterEventName: "tum", PortalConfigurationID: "bpc_test"},
			wantErr: "missing TUM price id",
		},
		{
			name:    "missing meter",
			catalog: Catalog{PriceIDTUM: "price_test", MeterEventName: "tum", PortalConfigurationID: "bpc_test"},
			wantErr: "missing TUM meter id",
		},
		{
			name:    "missing meter event name",
			catalog: Catalog{PriceIDTUM: "price_test", MeterIDTUM: "mtr_test", PortalConfigurationID: "bpc_test"},
			wantErr: "missing meter event name",
		},
		{
			name:    "unset price",
			catalog: Catalog{PriceIDTUM: "unset", MeterIDTUM: "mtr_test", MeterEventName: "tum", PortalConfigurationID: "bpc_test"},
			wantErr: "missing TUM price id",
		},
		{
			name:    "unset meter",
			catalog: Catalog{PriceIDTUM: "price_test", MeterIDTUM: "unset", MeterEventName: "tum", PortalConfigurationID: "bpc_test"},
			wantErr: "missing TUM meter id",
		},
		{
			name:    "unset meter event name",
			catalog: Catalog{PriceIDTUM: "price_test", MeterIDTUM: "mtr_test", MeterEventName: "unset", PortalConfigurationID: "bpc_test"},
			wantErr: "missing meter event name",
		},
		{
			name:    "missing portal configuration",
			catalog: Catalog{PriceIDTUM: "price_test", MeterIDTUM: "mtr_test", MeterEventName: "tum"},
			wantErr: "missing portal configuration id",
		},
		{
			name:    "unset portal configuration",
			catalog: Catalog{PriceIDTUM: "price_test", MeterIDTUM: "mtr_test", MeterEventName: "tum", PortalConfigurationID: "unset"},
			wantErr: "missing portal configuration id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.catalog.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestIsConfigured(t *testing.T) {
	t.Parallel()

	require.False(t, IsConfigured(""))
	require.False(t, IsConfigured("unset"))
	require.True(t, IsConfigured("sk_test_placeholder"))
}

type fakeStripeAPI struct {
	customerParams             *stripesdk.CustomerCreateParams
	checkoutSessionParams      *stripesdk.CheckoutSessionCreateParams
	checkoutRetrieveParams     *stripesdk.CheckoutSessionRetrieveParams
	checkoutSession            *stripesdk.CheckoutSession
	subscriptionRetrieveParams *stripesdk.SubscriptionRetrieveParams
	subscriptionUpdateParams   *stripesdk.SubscriptionUpdateParams
	subscription               *stripesdk.Subscription
	portalSessionParams        *stripesdk.BillingPortalSessionCreateParams
	portalSession              *stripesdk.BillingPortalSession
	meterEventParams           *stripesdk.BillingMeterEventCreateParams
	meterSummaryParams         *stripesdk.BillingMeterEventSummaryListParams
	meterSummaries             []*stripesdk.BillingMeterEventSummary
	invoiceRetrieveParams      *stripesdk.InvoiceRetrieveParams
	invoice                    *stripesdk.Invoice
	invoiceItemParams          *stripesdk.InvoiceItemCreateParams
	invoiceItemListParams      *stripesdk.InvoiceItemListParams
	invoiceItems               []*stripesdk.InvoiceItem
	creditNoteParams           *stripesdk.CreditNoteCreateParams
	creditNoteListParams       *stripesdk.CreditNoteListParams
	creditNotes                []*stripesdk.CreditNote
	calls                      int
	err                        error
}

func (f *fakeStripeAPI) createCustomer(_ context.Context, params *stripesdk.CustomerCreateParams) (*stripesdk.Customer, error) {
	f.calls++
	f.customerParams = params
	return &stripesdk.Customer{ID: "cus_test"}, f.err
}

func (f *fakeStripeAPI) createCheckoutSession(_ context.Context, params *stripesdk.CheckoutSessionCreateParams) (*stripesdk.CheckoutSession, error) {
	f.calls++
	f.checkoutSessionParams = params
	return &stripesdk.CheckoutSession{ID: "cs_test", URL: "https://checkout.stripe.test/session"}, f.err
}

func (f *fakeStripeAPI) retrieveCheckoutSession(_ context.Context, _ string, params *stripesdk.CheckoutSessionRetrieveParams) (*stripesdk.CheckoutSession, error) {
	f.calls++
	f.checkoutRetrieveParams = params
	return f.checkoutSession, f.err
}

func (f *fakeStripeAPI) retrieveSubscription(_ context.Context, _ string, params *stripesdk.SubscriptionRetrieveParams) (*stripesdk.Subscription, error) {
	f.calls++
	f.subscriptionRetrieveParams = params
	return f.subscription, f.err
}

func (f *fakeStripeAPI) updateSubscription(_ context.Context, _ string, params *stripesdk.SubscriptionUpdateParams) (*stripesdk.Subscription, error) {
	f.calls++
	f.subscriptionUpdateParams = params
	return f.subscription, f.err
}

func (f *fakeStripeAPI) createPortalSession(_ context.Context, params *stripesdk.BillingPortalSessionCreateParams) (*stripesdk.BillingPortalSession, error) {
	f.calls++
	f.portalSessionParams = params
	return f.portalSession, f.err
}

func (f *fakeStripeAPI) createMeterEvent(_ context.Context, params *stripesdk.BillingMeterEventCreateParams) (*stripesdk.BillingMeterEvent, error) {
	f.calls++
	f.meterEventParams = params
	return &stripesdk.BillingMeterEvent{}, f.err
}

func (f *fakeStripeAPI) listMeterEventSummaries(_ context.Context, params *stripesdk.BillingMeterEventSummaryListParams) stripesdk.Seq2[*stripesdk.BillingMeterEventSummary, error] {
	f.calls++
	f.meterSummaryParams = params
	return func(yield func(*stripesdk.BillingMeterEventSummary, error) bool) {
		for _, summary := range f.meterSummaries {
			if !yield(summary, nil) {
				return
			}
		}
		if f.err != nil {
			yield(nil, f.err)
		}
	}
}

func (f *fakeStripeAPI) retrieveInvoice(_ context.Context, _ string, params *stripesdk.InvoiceRetrieveParams) (*stripesdk.Invoice, error) {
	f.calls++
	f.invoiceRetrieveParams = params
	return f.invoice, f.err
}

func (f *fakeStripeAPI) createInvoiceItem(_ context.Context, params *stripesdk.InvoiceItemCreateParams) (*stripesdk.InvoiceItem, error) {
	f.calls++
	f.invoiceItemParams = params
	if f.err != nil {
		return nil, f.err
	}
	return &stripesdk.InvoiceItem{ID: "ii_test"}, nil
}

func (f *fakeStripeAPI) listInvoiceItems(_ context.Context, params *stripesdk.InvoiceItemListParams) stripesdk.Seq2[*stripesdk.InvoiceItem, error] {
	f.calls++
	f.invoiceItemListParams = params
	return func(yield func(*stripesdk.InvoiceItem, error) bool) {
		for _, item := range f.invoiceItems {
			if !yield(item, nil) {
				return
			}
		}
		if f.err != nil {
			yield(nil, f.err)
		}
	}
}

func (f *fakeStripeAPI) createCreditNote(_ context.Context, params *stripesdk.CreditNoteCreateParams) (*stripesdk.CreditNote, error) {
	f.calls++
	f.creditNoteParams = params
	if f.err != nil {
		return nil, f.err
	}
	return &stripesdk.CreditNote{ID: "cn_test"}, nil
}

func (f *fakeStripeAPI) listCreditNotes(_ context.Context, params *stripesdk.CreditNoteListParams) stripesdk.Seq2[*stripesdk.CreditNote, error] {
	f.calls++
	f.creditNoteListParams = params
	return func(yield func(*stripesdk.CreditNote, error) bool) {
		for _, note := range f.creditNotes {
			if !yield(note, nil) {
				return
			}
		}
		if f.err != nil {
			yield(nil, f.err)
		}
	}
}

func testSubscription() *stripesdk.Subscription {
	return &stripesdk.Subscription{
		ID:                "sub_test",
		Customer:          &stripesdk.Customer{ID: "cus_test"},
		Status:            stripesdk.SubscriptionStatusPastDue,
		CancelAtPeriodEnd: true,
		CancelAt:          time.Date(2026, time.September, 15, 0, 0, 0, 0, time.UTC).Unix(),
		CanceledAt:        time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC).Unix(),
		TrialStart:        time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC).Unix(),
		TrialEnd:          time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC).Unix(),
		Items: &stripesdk.SubscriptionItemList{Data: []*stripesdk.SubscriptionItem{
			{
				Price:              &stripesdk.Price{ID: "price_tum"},
				CurrentPeriodStart: time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC).Unix(),
				CurrentPeriodEnd:   time.Date(2026, time.September, 15, 0, 0, 0, 0, time.UTC).Unix(),
			},
		}},
		LatestInvoice: &stripesdk.Invoice{
			ID:              "in_test",
			Status:          stripesdk.InvoiceStatusOpen,
			AmountRemaining: 4200,
		},
	}
}

func TestGetSubscriptionReturnsLiveLifecycleState(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{subscription: testSubscription()}
	c := &client{api: api, catalog: Catalog{PriceIDTUM: "price_tum", MeterIDTUM: "mtr_tum", MeterEventName: "tum", PortalConfigurationID: "bpc_test"}}

	state, err := c.GetSubscription(t.Context(), "sub_test")
	require.NoError(t, err)
	require.Equal(t, "sub_test", state.ID)
	require.Equal(t, "cus_test", state.CustomerID)
	require.Equal(t, "past_due", state.Status)
	require.Equal(t, time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC), state.CurrentPeriodStart)
	require.Equal(t, time.Date(2026, time.September, 15, 0, 0, 0, 0, time.UTC), state.CurrentPeriodEnd)
	require.True(t, state.CancelAtPeriodEnd)
	require.True(t, state.PaymentFailed)
	require.Equal(t, "in_test", state.LatestInvoiceID)
	require.Equal(t, "open", state.LatestInvoiceStatus)
	require.EqualValues(t, 4200, state.LatestInvoiceAmountRemaining)
	require.Len(t, api.subscriptionRetrieveParams.Expand, 1)
	require.Equal(t, "latest_invoice", stripesdk.StringValue(api.subscriptionRetrieveParams.Expand[0]))
}

func TestGetSubscriptionPaymentFailed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		subscription    stripesdk.SubscriptionStatus
		invoice         stripesdk.InvoiceStatus
		amountRemaining int64
		want            bool
	}{
		{name: "past due with unpaid open invoice", subscription: stripesdk.SubscriptionStatusPastDue, invoice: stripesdk.InvoiceStatusOpen, amountRemaining: 1, want: true},
		{name: "past due with paid invoice", subscription: stripesdk.SubscriptionStatusPastDue, invoice: stripesdk.InvoiceStatusPaid, amountRemaining: 0},
		{name: "past due with zero remaining", subscription: stripesdk.SubscriptionStatusPastDue, invoice: stripesdk.InvoiceStatusOpen, amountRemaining: 0},
		{name: "active while invoice is open", subscription: stripesdk.SubscriptionStatusActive, invoice: stripesdk.InvoiceStatusOpen, amountRemaining: 1},
		{name: "incomplete while invoice is open", subscription: stripesdk.SubscriptionStatusIncomplete, invoice: stripesdk.InvoiceStatusOpen, amountRemaining: 1},
		{name: "unpaid while invoice is open", subscription: stripesdk.SubscriptionStatusUnpaid, invoice: stripesdk.InvoiceStatusOpen, amountRemaining: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			subscription := testSubscription()
			subscription.Status = tt.subscription
			subscription.LatestInvoice.Status = tt.invoice
			subscription.LatestInvoice.AmountRemaining = tt.amountRemaining
			api := &fakeStripeAPI{subscription: subscription}
			client := &client{api: api, catalog: Catalog{PriceIDTUM: "price_tum"}}

			state, err := client.GetSubscription(t.Context(), "sub_test")
			require.NoError(t, err)
			require.Equal(t, tt.want, state.PaymentFailed)
		})
	}
}

func TestGetSubscriptionRequiresConfiguredTUMItem(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{subscription: testSubscription()}
	c := &client{api: api, catalog: Catalog{PriceIDTUM: "price_other", MeterIDTUM: "mtr_tum", MeterEventName: "tum", PortalConfigurationID: "bpc_test"}}

	_, err := c.GetSubscription(t.Context(), "sub_test")
	require.ErrorContains(t, err, "missing the configured TUM service period")
}

func TestSetSubscriptionCancelAtPeriodEndReturnsUpdatedState(t *testing.T) {
	t.Parallel()

	subscription := testSubscription()
	subscription.CancelAtPeriodEnd = false
	api := &fakeStripeAPI{subscription: subscription}
	c := &client{api: api, catalog: Catalog{PriceIDTUM: "price_tum", MeterIDTUM: "mtr_tum", MeterEventName: "tum", PortalConfigurationID: "bpc_test"}}

	state, err := c.SetSubscriptionCancelAtPeriodEnd(t.Context(), SetSubscriptionCancelAtPeriodEndInput{
		SubscriptionID:    "sub_test",
		CancelAtPeriodEnd: false,
	})
	require.NoError(t, err)
	require.False(t, state.CancelAtPeriodEnd)
	require.NotNil(t, api.subscriptionUpdateParams.CancelAtPeriodEnd)
	require.False(t, stripesdk.BoolValue(api.subscriptionUpdateParams.CancelAtPeriodEnd))
}

func TestCreatePortalSessionUsesControlledConfiguration(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{portalSession: &stripesdk.BillingPortalSession{
		ID:       "bps_test",
		Customer: "cus_test",
		URL:      "https://billing.stripe.test/session",
	}}
	c := &client{api: api, catalog: Catalog{PriceIDTUM: "price_tum", MeterIDTUM: "mtr_tum", MeterEventName: "tum", PortalConfigurationID: "bpc_test"}}

	session, err := c.CreatePortalSession(t.Context(), CreatePortalSessionInput{
		CustomerID: "cus_test",
		ReturnURL:  "https://app.example.test/customer/billing",
	})
	require.NoError(t, err)
	require.Equal(t, "bps_test", session.ID)
	require.Equal(t, "bpc_test", stripesdk.StringValue(api.portalSessionParams.Configuration))
	require.Equal(t, "cus_test", stripesdk.StringValue(api.portalSessionParams.Customer))
	require.Equal(t, "https://app.example.test/customer/billing", stripesdk.StringValue(api.portalSessionParams.ReturnURL))
}

func TestCreatePortalSessionRequiresControlledConfiguration(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{}
	c := &client{api: api, catalog: Catalog{}}

	_, err := c.CreatePortalSession(t.Context(), CreatePortalSessionInput{
		CustomerID: "cus_test",
		ReturnURL:  "https://app.example.test/customer/billing",
	})
	require.ErrorContains(t, err, "portal configuration id is required")
	require.Zero(t, api.calls)
}

func TestCreateCustomerPropagatesIdempotencyKey(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{}
	c := &client{api: api}

	customer, err := c.CreateCustomer(t.Context(), CreateCustomerInput{
		OrganizationID:   "<ORG_ID>",
		OrganizationSlug: "the-customer",
		IdempotencyKey:   "customer:<ORG_ID>",
	})
	require.NoError(t, err)
	require.Equal(t, "cus_test", customer.ID)
	require.Equal(t, "customer:<ORG_ID>", stripesdk.StringValue(api.customerParams.IdempotencyKey))
	require.Equal(t, "<ORG_ID>", api.customerParams.Metadata[organizationIDMetadataKey])
	require.Equal(t, "the-customer", api.customerParams.Metadata[organizationSlugMetadataKey])
}

func TestCreateCustomerRejectsMissingIdempotencyKey(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{}
	c := &client{api: api}

	_, err := c.CreateCustomer(t.Context(), CreateCustomerInput{})
	require.ErrorIs(t, err, errMissingIdempotencyKey)
	require.Zero(t, api.calls)
}

func TestCreateCheckoutSessionBuildsMeteredSubscription(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{}
	c := &client{
		api: api,
		catalog: Catalog{
			PriceIDTUM:            "price_tum",
			MeterIDTUM:            "mtr_tum",
			MeterEventName:        "tum",
			PortalConfigurationID: "bpc_test",
		},
	}
	billingCycleAnchor := time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC)
	expiresAt := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)

	session, err := c.CreateCheckoutSession(t.Context(), CreateCheckoutSessionInput{
		CustomerID:         "cus_test",
		OrganizationID:     "<ORG_ID>",
		OrganizationSlug:   "the-customer",
		SuccessURL:         "https://app.example.test/the-customer/billing",
		CancelURL:          "https://app.example.test/the-customer/billing",
		TrialEnd:           &billingCycleAnchor,
		BillingCycleAnchor: billingCycleAnchor,
		ExpiresAt:          expiresAt,
		IdempotencyKey:     "checkout:<ORG_ID>:request",
	})
	require.NoError(t, err)
	require.Equal(t, "cs_test", session.ID)
	require.Equal(t, "https://checkout.stripe.test/session", session.URL)

	params := api.checkoutSessionParams
	require.Equal(t, "checkout:<ORG_ID>:request", stripesdk.StringValue(params.IdempotencyKey))
	require.Equal(t, "cus_test", stripesdk.StringValue(params.Customer))
	require.Equal(t, expiresAt.Unix(), stripesdk.Int64Value(params.ExpiresAt))
	require.Equal(t, "<ORG_ID>", stripesdk.StringValue(params.ClientReferenceID))
	require.True(t, stripesdk.BoolValue(params.AllowPromotionCodes))
	require.Equal(t, "subscription", stripesdk.StringValue(params.Mode))
	require.Equal(t, "always", stripesdk.StringValue(params.PaymentMethodCollection))
	require.Equal(t, "https://app.example.test/the-customer/billing", stripesdk.StringValue(params.SuccessURL))
	require.Equal(t, "https://app.example.test/the-customer/billing", stripesdk.StringValue(params.CancelURL))
	require.Len(t, params.LineItems, 1)
	require.Equal(t, "price_tum", stripesdk.StringValue(params.LineItems[0].Price))
	require.Nil(t, params.LineItems[0].Quantity)
	require.Equal(t, "<ORG_ID>", params.Metadata[organizationIDMetadataKey])
	require.Equal(t, "the-customer", params.Metadata[organizationSlugMetadataKey])
	require.NotNil(t, params.SubscriptionData)
	require.Equal(t, billingCycleAnchor.Unix(), stripesdk.Int64Value(params.SubscriptionData.TrialEnd))
	require.Nil(t, params.SubscriptionData.BillingCycleAnchor)
	require.Nil(t, params.SubscriptionData.ProrationBehavior)
	require.Equal(t, "<ORG_ID>", params.SubscriptionData.Metadata[organizationIDMetadataKey])
	require.Equal(t, "the-customer", params.SubscriptionData.Metadata[organizationSlugMetadataKey])
}

func TestCreateCheckoutSessionWithoutTrialStartsImmediately(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{}
	c := &client{api: api, catalog: Catalog{PriceIDTUM: "price_tum", MeterIDTUM: "mtr_tum", MeterEventName: "tum", PortalConfigurationID: "bpc_test"}}

	_, err := c.CreateCheckoutSession(t.Context(), CreateCheckoutSessionInput{
		CustomerID:         "",
		OrganizationID:     "",
		OrganizationSlug:   "",
		SuccessURL:         "",
		CancelURL:          "",
		TrialEnd:           nil,
		BillingCycleAnchor: time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC),
		ExpiresAt:          time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC),
		IdempotencyKey:     "checkout",
	})
	require.NoError(t, err)
	require.Nil(t, api.checkoutSessionParams.SubscriptionData.TrialEnd)
	require.Equal(t, time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC).Unix(), stripesdk.Int64Value(api.checkoutSessionParams.SubscriptionData.BillingCycleAnchor))
	require.Equal(t, "none", stripesdk.StringValue(api.checkoutSessionParams.SubscriptionData.ProrationBehavior))
}

func TestCreateCheckoutSessionRejectsMissingBillingCycleAnchor(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{}
	c := &client{api: api}

	_, err := c.CreateCheckoutSession(t.Context(), CreateCheckoutSessionInput{IdempotencyKey: "checkout"})
	require.ErrorIs(t, err, errMissingBillingCycleAnchor)
	require.Zero(t, api.calls)
}

func TestCreateCheckoutSessionRejectsMissingExpiration(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{}
	c := &client{api: api}

	_, err := c.CreateCheckoutSession(t.Context(), CreateCheckoutSessionInput{
		BillingCycleAnchor: time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC),
		IdempotencyKey:     "checkout",
	})
	require.ErrorIs(t, err, errMissingCheckoutExpiration)
	require.Zero(t, api.calls)
}

func TestCreateCheckoutSessionRejectsMissingIdempotencyKey(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{}
	c := &client{api: api}

	_, err := c.CreateCheckoutSession(t.Context(), CreateCheckoutSessionInput{})
	require.ErrorIs(t, err, errMissingIdempotencyKey)
	require.Zero(t, api.calls)
}

func TestGetCheckoutSessionExpandsSubscription(t *testing.T) {
	t.Parallel()

	anchor := time.Date(2026, time.August, 21, 9, 30, 0, 0, time.UTC)
	api := &fakeStripeAPI{checkoutSession: &stripesdk.CheckoutSession{
		ID:       "cs_test",
		Status:   stripesdk.CheckoutSessionStatusComplete,
		Customer: &stripesdk.Customer{ID: "cus_test"},
		Subscription: &stripesdk.Subscription{
			ID:                 "sub_test",
			Customer:           &stripesdk.Customer{ID: "cus_test"},
			Status:             stripesdk.SubscriptionStatusTrialing,
			BillingCycleAnchor: anchor.Unix(),
		},
	}}

	state, err := (&client{api: api}).GetCheckoutSession(t.Context(), "cs_test")
	require.NoError(t, err)
	require.Equal(t, "cs_test", state.ID)
	require.Equal(t, "complete", state.Status)
	require.Equal(t, "cus_test", state.CustomerID)
	require.Equal(t, "sub_test", state.SubscriptionID)
	require.Equal(t, "cus_test", state.SubscriptionCustomerID)
	require.Equal(t, "trialing", state.SubscriptionStatus)
	require.Equal(t, anchor, state.BillingCycleAnchor)
	require.Len(t, api.checkoutRetrieveParams.Expand, 1)
	require.Equal(t, "subscription", stripesdk.StringValue(api.checkoutRetrieveParams.Expand[0]))
}

func TestCreateMeterEventPropagatesIdempotencyKey(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{}
	c := &client{
		api:     api,
		catalog: Catalog{PriceIDTUM: "", MeterIDTUM: "", MeterEventName: "tum", PortalConfigurationID: ""},
	}
	timestamp := time.Date(2026, time.August, 14, 10, 0, 0, 0, time.UTC)

	err := c.CreateMeterEvent(t.Context(), CreateMeterEventInput{
		CustomerID:     "cus_test",
		EventName:      "tum_replay",
		Value:          42,
		Timestamp:      timestamp,
		IdempotencyKey: "meter:<ORG_ID>:1",
	})
	require.NoError(t, err)
	require.Equal(t, "meter:<ORG_ID>:1", stripesdk.StringValue(api.meterEventParams.IdempotencyKey))
	require.Equal(t, "meter:<ORG_ID>:1", stripesdk.StringValue(api.meterEventParams.Identifier))
	require.Equal(t, "tum_replay", stripesdk.StringValue(api.meterEventParams.EventName))
	require.Equal(t, "cus_test", api.meterEventParams.Payload[meterCustomerPayloadKey])
	require.Equal(t, "42", api.meterEventParams.Payload[meterValuePayloadKey])
	require.Equal(t, timestamp.Unix(), stripesdk.Int64Value(api.meterEventParams.Timestamp))
}

func TestCreateMeterEventRejectsMissingIdempotencyKey(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{}
	c := &client{api: api}

	err := c.CreateMeterEvent(t.Context(), CreateMeterEventInput{})
	require.ErrorIs(t, err, errMissingIdempotencyKey)
	require.Zero(t, api.calls)
}

func TestCreateMeterEventRejectsMissingEventName(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{}
	c := &client{api: api}

	err := c.CreateMeterEvent(t.Context(), CreateMeterEventInput{IdempotencyKey: "meter:<ORG_ID>:1"})
	require.ErrorIs(t, err, errMissingMeterEventName)
	require.Zero(t, api.calls)
}

func TestGetMeterEventSummaryAggregatesSDKSequence(t *testing.T) {
	t.Parallel()
	// stripe-go's All method owns HTTP pagination and exposes the resulting
	// items as Seq2. This fake starts at that SDK boundary and verifies that
	// the client aggregates every item in the sequence.

	api := &fakeStripeAPI{meterSummaries: []*stripesdk.BillingMeterEventSummary{
		{AggregatedValue: 10.5},
		{AggregatedValue: -2},
		{AggregatedValue: 4.25},
	}}
	c := &client{
		api: api,
		catalog: Catalog{
			PriceIDTUM:            "",
			MeterIDTUM:            "mtr_tum",
			MeterEventName:        "",
			PortalConfigurationID: "",
		},
	}
	start := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)

	total, err := c.GetMeterEventSummary(t.Context(), GetMeterEventSummaryInput{
		CustomerID: "cus_test",
		Start:      start,
		End:        end,
	})
	require.NoError(t, err)
	require.InDelta(t, 12.75, total, 0.000001)
	require.Equal(t, "mtr_tum", stripesdk.StringValue(api.meterSummaryParams.ID))
	require.Equal(t, "cus_test", stripesdk.StringValue(api.meterSummaryParams.Customer))
	require.Equal(t, start.Unix(), stripesdk.Int64Value(api.meterSummaryParams.StartTime))
	require.Equal(t, end.Unix(), stripesdk.Int64Value(api.meterSummaryParams.EndTime))
	require.Equal(t, int64(100), stripesdk.Int64Value(api.meterSummaryParams.Limit))
}

func TestGetMeterEventSummaryValidatesBounds(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		start   time.Time
		end     time.Time
		wantErr string
	}{
		{
			name:    "start not minute aligned",
			start:   start.Add(time.Second),
			end:     start.Add(time.Hour),
			wantErr: "minute-aligned",
		},
		{
			name:    "end not minute aligned",
			start:   start,
			end:     start.Add(time.Hour + time.Second),
			wantErr: "minute-aligned",
		},
		{
			name:    "empty interval",
			start:   start,
			end:     start,
			wantErr: "end must be after start",
		},
		{
			name:    "reversed interval",
			start:   start,
			end:     start.Add(-time.Minute),
			wantErr: "end must be after start",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			api := &fakeStripeAPI{}
			c := &client{api: api}
			_, err := c.GetMeterEventSummary(t.Context(), GetMeterEventSummaryInput{
				CustomerID: "cus_test",
				Start:      tt.start,
				End:        tt.end,
			})
			require.ErrorContains(t, err, tt.wantErr)
			require.Zero(t, api.calls)
		})
	}
}

func TestGetMeterEventSummaryWrapsListError(t *testing.T) {
	t.Parallel()

	apiErr := errors.New("request failed")
	api := &fakeStripeAPI{err: apiErr}
	c := &client{api: api}
	start := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)

	_, err := c.GetMeterEventSummary(t.Context(), GetMeterEventSummaryInput{
		CustomerID: "cus_test",
		Start:      start,
		End:        start.Add(time.Hour),
	})
	require.ErrorIs(t, err, apiErr)
	require.ErrorContains(t, err, "list Stripe meter event summaries")
}

func TestWritesWrapStripeErrors(t *testing.T) {
	t.Parallel()

	apiErr := errors.New("request failed")
	api := &fakeStripeAPI{err: apiErr}
	c := &client{api: api, catalog: Catalog{PriceIDTUM: "", MeterIDTUM: "", MeterEventName: "tum", PortalConfigurationID: ""}}

	_, err := c.CreateCustomer(t.Context(), CreateCustomerInput{IdempotencyKey: "customer"})
	require.ErrorIs(t, err, apiErr)
	require.ErrorContains(t, err, "create Stripe customer")

	err = c.CreateMeterEvent(t.Context(), CreateMeterEventInput{EventName: "tum", IdempotencyKey: "meter"})
	require.ErrorIs(t, err, apiErr)
	require.ErrorContains(t, err, "create Stripe meter event")

	_, err = c.CreateCheckoutSession(t.Context(), CreateCheckoutSessionInput{
		BillingCycleAnchor: time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC),
		ExpiresAt:          time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC),
		IdempotencyKey:     "checkout",
	})
	require.ErrorIs(t, err, apiErr)
	require.ErrorContains(t, err, "create Stripe Checkout session")
}

func TestGetInvoiceMapsSubscriptionPeriodAndState(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	finalized := end.Add(72 * time.Hour)
	api := &fakeStripeAPI{invoice: &stripesdk.Invoice{
		ID:              "in_placeholder",
		Customer:        &stripesdk.Customer{ID: "cus_placeholder"},
		Currency:        stripesdk.CurrencyUSD,
		BillingReason:   stripesdk.InvoiceBillingReasonSubscriptionCycle,
		Status:          stripesdk.InvoiceStatusOpen,
		PeriodStart:     start.Unix(),
		PeriodEnd:       end.Unix(),
		AmountRemaining: 1234,
		Parent: &stripesdk.InvoiceParent{SubscriptionDetails: &stripesdk.InvoiceParentSubscriptionDetails{
			Subscription: &stripesdk.Subscription{ID: "sub_placeholder"},
		}},
		StatusTransitions: &stripesdk.InvoiceStatusTransitions{FinalizedAt: finalized.Unix()},
	}}

	state, err := (&client{api: api}).GetInvoice(t.Context(), "in_placeholder")
	require.NoError(t, err)
	require.Equal(t, &InvoiceState{
		ID:                 "in_placeholder",
		CustomerID:         "cus_placeholder",
		SubscriptionID:     "sub_placeholder",
		Currency:           "usd",
		BillingReason:      "subscription_cycle",
		Status:             "open",
		ServicePeriodStart: start,
		ServicePeriodEnd:   end,
		FinalizedAt:        finalized,
		AmountRemaining:    1234,
	}, state)
	require.Len(t, api.invoiceRetrieveParams.Expand, 1)
	require.Equal(t, "parent.subscription_details.subscription", stripesdk.StringValue(api.invoiceRetrieveParams.Expand[0]))
}

func TestCreateInvoiceItemUsesExactFinancialIdentity(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	api := &fakeStripeAPI{}
	item, err := (&client{api: api}).CreateInvoiceItem(t.Context(), CreateInvoiceItemInput{
		CustomerID:     "cus_placeholder",
		SubscriptionID: "sub_placeholder",
		InvoiceID:      "in_placeholder",
		Description:    "OpenRouter chat usage",
		AmountCents:    1001,
		PeriodStart:    start,
		PeriodEnd:      end,
		AllocationKey:  "allocation_placeholder",
		IdempotencyKey: "openrouter:<ORG_ID>:2026-07-01:1",
	})
	require.NoError(t, err)
	require.Equal(t, &InvoiceItem{ID: "ii_test", InvoiceID: "in_placeholder", Currency: "usd", AmountCents: 1001}, item)
	params := api.invoiceItemParams
	require.EqualValues(t, 1001, stripesdk.Int64Value(params.Amount))
	require.Equal(t, "usd", stripesdk.StringValue(params.Currency))
	require.Equal(t, "cus_placeholder", stripesdk.StringValue(params.Customer))
	require.Equal(t, "sub_placeholder", stripesdk.StringValue(params.Subscription))
	require.Equal(t, "in_placeholder", stripesdk.StringValue(params.Invoice))
	require.Equal(t, "OpenRouter chat usage", stripesdk.StringValue(params.Description))
	require.False(t, stripesdk.BoolValue(params.Discountable))
	require.Equal(t, start.Unix(), stripesdk.Int64Value(params.Period.Start))
	require.Equal(t, end.Add(-time.Second).Unix(), stripesdk.Int64Value(params.Period.End))
	require.Equal(t, "allocation_placeholder", params.Metadata[allocationMetadataKey])
	require.Equal(t, "openrouter:<ORG_ID>:2026-07-01:1", stripesdk.StringValue(params.IdempotencyKey))
}

func TestCreateCreditNoteUsesCustomerBalanceForPostPaymentCredit(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{}
	note, err := (&client{api: api}).CreateCreditNote(t.Context(), CreateCreditNoteInput{
		InvoiceID:         "in_placeholder",
		Description:       "OpenRouter usage correction",
		AmountCents:       250,
		CreditAmountCents: 200,
		AllocationKey:     "allocation_placeholder",
		IdempotencyKey:    "openrouter:<ORG_ID>:2026-07-01:2",
	})
	require.NoError(t, err)
	require.Equal(t, &CreditNote{ID: "cn_test", InvoiceID: "in_placeholder", Currency: "usd", AmountCents: 250}, note)
	params := api.creditNoteParams
	require.Equal(t, "in_placeholder", stripesdk.StringValue(params.Invoice))
	require.EqualValues(t, 250, stripesdk.Int64Value(params.Amount))
	require.EqualValues(t, 200, stripesdk.Int64Value(params.CreditAmount))
	require.Equal(t, "none", stripesdk.StringValue(params.EmailType))
	require.Equal(t, "OpenRouter usage correction", stripesdk.StringValue(params.Memo))
	require.Equal(t, "order_change", stripesdk.StringValue(params.Reason))
	require.Equal(t, "allocation_placeholder", params.Metadata[allocationMetadataKey])
	require.Equal(t, "openrouter:<ORG_ID>:2026-07-01:2", stripesdk.StringValue(params.IdempotencyKey))
}

func TestFindInvoiceAllocationValidatesFinancialData(t *testing.T) {
	t.Parallel()

	invoice := &stripesdk.Invoice{ID: "in_placeholder"}
	tests := []struct {
		name      string
		item      *stripesdk.InvoiceItem
		note      *stripesdk.CreditNote
		findNote  bool
		wantID    string
		wantError string
	}{
		{
			name:   "invoice item match",
			item:   &stripesdk.InvoiceItem{ID: "ii_placeholder", Invoice: invoice, Currency: stripesdk.CurrencyUSD, Amount: 125, Metadata: map[string]string{allocationMetadataKey: "allocation_placeholder"}},
			wantID: "ii_placeholder",
		},
		{
			name:      "invoice item amount mismatch",
			item:      &stripesdk.InvoiceItem{ID: "ii_placeholder", Invoice: invoice, Currency: stripesdk.CurrencyUSD, Amount: 124, Metadata: map[string]string{allocationMetadataKey: "allocation_placeholder"}},
			wantError: "different financial data",
		},
		{
			name:     "credit note match",
			note:     &stripesdk.CreditNote{ID: "cn_placeholder", Invoice: invoice, Currency: stripesdk.CurrencyUSD, Amount: 125, Metadata: map[string]string{allocationMetadataKey: "allocation_placeholder"}},
			findNote: true,
			wantID:   "cn_placeholder",
		},
		{
			name:      "credit note amount mismatch",
			note:      &stripesdk.CreditNote{ID: "cn_placeholder", Invoice: invoice, Currency: stripesdk.CurrencyUSD, Amount: 124, Metadata: map[string]string{allocationMetadataKey: "allocation_placeholder"}},
			findNote:  true,
			wantError: "different financial data",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			api := &fakeStripeAPI{invoiceItems: []*stripesdk.InvoiceItem{test.item}, creditNotes: []*stripesdk.CreditNote{test.note}}
			input := FindInvoiceAllocationInput{InvoiceID: "in_placeholder", AllocationKey: "allocation_placeholder", AmountCents: 125}
			var id string
			if test.findNote {
				found, err := (&client{api: api}).FindCreditNote(t.Context(), input)
				if found != nil {
					id = found.ID
				}
				if test.wantError != "" {
					require.ErrorContains(t, err, test.wantError)
					return
				}
				require.NoError(t, err)
				require.Equal(t, "in_placeholder", stripesdk.StringValue(api.creditNoteListParams.Invoice))
			} else {
				found, err := (&client{api: api}).FindInvoiceItem(t.Context(), input)
				if found != nil {
					id = found.ID
				}
				if test.wantError != "" {
					require.ErrorContains(t, err, test.wantError)
					return
				}
				require.NoError(t, err)
				require.Equal(t, "in_placeholder", stripesdk.StringValue(api.invoiceItemListParams.Invoice))
			}
			require.Equal(t, test.wantID, id)
		})
	}
}

func TestVerifyWebhook(t *testing.T) {
	t.Parallel()

	const secret = "whsec_test"
	created := time.Now().Truncate(time.Second)
	payload := webhookPayload(t, stripesdk.APIVersion, created, map[string]any{
		"id":       "in_test",
		"customer": "cus_test",
	})
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload:   payload,
		Secret:    secret,
		Timestamp: created,
	})
	c := &client{webhookSecret: secret}

	event, err := c.VerifyWebhook(signed.Payload, signed.Header)
	require.NoError(t, err)
	require.Equal(t, "evt_test", event.ID)
	require.Equal(t, "invoice.created", event.Type)
	require.Equal(t, created, event.Created)
	require.Equal(t, "in_test", event.ObjectID)
	require.Equal(t, "cus_test", event.CustomerID)
	require.Empty(t, event.SubscriptionID)
}

func TestVerifyWebhookExtractsExpandedCustomer(t *testing.T) {
	t.Parallel()

	const secret = "whsec_test"
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload: webhookPayload(t, stripesdk.APIVersion, time.Now(), map[string]any{
			"id": "in_test",
			"customer": map[string]any{
				"id":     "cus_expanded",
				"object": "customer",
			},
		}),
		Secret: secret,
	})

	event, err := (&client{webhookSecret: secret}).VerifyWebhook(signed.Payload, signed.Header)
	require.NoError(t, err)
	require.Equal(t, "in_test", event.ObjectID)
	require.Equal(t, "cus_expanded", event.CustomerID)
}

func TestVerifyWebhookAllowsMissingCustomer(t *testing.T) {
	t.Parallel()

	const secret = "whsec_test"
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload: webhookPayload(t, stripesdk.APIVersion, time.Now(), map[string]any{
			"id": "in_test",
		}),
		Secret: secret,
	})

	event, err := (&client{webhookSecret: secret}).VerifyWebhook(signed.Payload, signed.Header)
	require.NoError(t, err)
	require.Equal(t, "in_test", event.ObjectID)
	require.Empty(t, event.CustomerID)
}

func TestVerifyWebhookIgnoresUnsupportedCustomerShape(t *testing.T) {
	t.Parallel()

	const secret = "whsec_test"
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload: webhookPayload(t, stripesdk.APIVersion, time.Now(), map[string]any{
			"id":       "in_test",
			"customer": 123,
		}),
		Secret: secret,
	})

	event, err := (&client{webhookSecret: secret}).VerifyWebhook(signed.Payload, signed.Header)
	require.NoError(t, err)
	require.Equal(t, "in_test", event.ObjectID)
	require.Empty(t, event.CustomerID)
}

func TestVerifyWebhookExtractsCustomersForAcceptedEventTypes(t *testing.T) {
	t.Parallel()

	const secret = "whsec_test"
	tests := []struct {
		name       string
		eventType  string
		customer   any
		customerID string
	}{
		{
			name:       "checkout session with string customer",
			eventType:  "checkout.session.completed",
			customer:   "cus_checkout",
			customerID: "cus_checkout",
		},
		{
			name:      "invoice created with expanded customer",
			eventType: "invoice.created",
			customer: map[string]any{
				"id":     "cus_invoice_created",
				"object": "customer",
			},
			customerID: "cus_invoice_created",
		},
		{
			name:       "invoice payment failure with string customer",
			eventType:  "invoice.payment_failed",
			customer:   "cus_invoice_failed",
			customerID: "cus_invoice_failed",
		},
		{
			name:      "subscription deletion with expanded customer",
			eventType: "customer.subscription.deleted",
			customer: map[string]any{
				"id":     "cus_subscription",
				"object": "customer",
			},
			customerID: "cus_subscription",
		},
	}

	for _, tt := range tests {
		signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
			Payload: webhookPayloadForType(t, stripesdk.APIVersion, time.Now(), tt.eventType, map[string]any{
				"id":       "obj_test",
				"customer": tt.customer,
			}),
			Secret: secret,
		})

		event, err := (&client{webhookSecret: secret}).VerifyWebhook(signed.Payload, signed.Header)
		require.NoErrorf(t, err, "%s: verification", tt.name)
		require.Equalf(t, tt.eventType, event.Type, "%s: event type", tt.name)
		require.Equalf(t, "obj_test", event.ObjectID, "%s: object id", tt.name)
		require.Equalf(t, tt.customerID, event.CustomerID, "%s: customer id", tt.name)
	}
}

func TestVerifyWebhookExtractsCheckoutSubscription(t *testing.T) {
	t.Parallel()

	const secret = "whsec_test"
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload: webhookPayloadForType(t, stripesdk.APIVersion, time.Now(), "checkout.session.completed", map[string]any{
			"id":           "cs_test",
			"customer":     "cus_test",
			"subscription": "sub_test",
		}),
		Secret: secret,
	})

	event, err := (&client{webhookSecret: secret}).VerifyWebhook(signed.Payload, signed.Header)
	require.NoError(t, err)
	require.Equal(t, "sub_test", event.SubscriptionID)
}

func TestVerifyWebhookRejectsMalformedDataObject(t *testing.T) {
	t.Parallel()

	const secret = "whsec_test"
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload: webhookPayloadForType(t, stripesdk.APIVersion, time.Now(), "invoice.created", nil),
		Secret:  secret,
	})

	_, err := (&client{webhookSecret: secret}).VerifyWebhook(signed.Payload, signed.Header)
	require.ErrorContains(t, err, "parse Stripe webhook data object")
	require.NotErrorIs(t, err, ErrWebhookNotConfigured)
}

func TestVerifyWebhookRejectsInvalidSignature(t *testing.T) {
	t.Parallel()

	const secret = "whsec_test"
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload: webhookPayload(t, stripesdk.APIVersion, time.Now(), map[string]any{"id": "in_test"}),
		Secret:  secret,
	})
	c := &client{webhookSecret: secret}

	_, err := c.VerifyWebhook(signed.Payload, signed.Header+"bad")
	require.ErrorContains(t, err, "verify Stripe webhook")
	require.NotErrorIs(t, err, ErrWebhookNotConfigured)
}

func TestVerifyWebhookRejectsUnconfiguredSecret(t *testing.T) {
	t.Parallel()

	payload := webhookPayload(t, stripesdk.APIVersion, time.Now(), map[string]any{"id": "in_test"})
	for _, secret := range []string{"", "unset"} {
		c := &client{webhookSecret: secret}
		_, err := c.VerifyWebhook(payload, "")
		require.ErrorIs(t, err, ErrWebhookNotConfigured)
	}
}

func TestVerifyWebhookRejectsReplayedTimestamp(t *testing.T) {
	t.Parallel()

	const secret = "whsec_test"
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload:   webhookPayload(t, stripesdk.APIVersion, time.Now(), map[string]any{"id": "in_test"}),
		Secret:    secret,
		Timestamp: time.Now().Add(-stripewebhook.DefaultTolerance - time.Second),
	})
	c := &client{webhookSecret: secret}

	_, err := c.VerifyWebhook(signed.Payload, signed.Header)
	require.ErrorContains(t, err, "timestamp wasn't within tolerance")
	require.NotErrorIs(t, err, ErrWebhookNotConfigured)
}

func TestVerifyWebhookAcceptsSignedEventsAcrossAPIVersions(t *testing.T) {
	t.Parallel()

	for _, apiVersion := range []string{"2025-09-30.clover", "2026-07-29.dahlia", "2099-01-01.future"} {
		t.Run(apiVersion, func(t *testing.T) {
			t.Parallel()

			const secret = "whsec_test"
			signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
				Payload: webhookPayload(t, apiVersion, time.Now(), map[string]any{"id": "in_test"}),
				Secret:  secret,
			})
			c := &client{webhookSecret: secret}

			event, err := c.VerifyWebhook(signed.Payload, signed.Header)
			require.NoError(t, err)
			require.Equal(t, "in_test", event.ObjectID)
		})
	}
}

func TestStubClientIsSafeToCall(t *testing.T) {
	t.Parallel()

	c := NewStubClient(testenv.NewLogger(t))

	customer, err := c.CreateCustomer(t.Context(), CreateCustomerInput{})
	require.NoError(t, err)
	require.Equal(t, "cus_local_stub", customer.ID)
	checkout, err := c.CreateCheckoutSession(t.Context(), CreateCheckoutSessionInput{OrganizationSlug: "test/org"})
	require.NoError(t, err)
	require.Equal(t, "cs_local_stub", checkout.ID)
	require.Equal(t, "http://localhost:3000/test%2Forg/billing", checkout.URL)
	require.NoError(t, c.CreateMeterEvent(t.Context(), CreateMeterEventInput{}))
	require.Equal(t, Catalog{}, c.Catalog())
	_, err = c.VerifyWebhook(nil, "")
	require.ErrorIs(t, err, ErrWebhookNotConfigured)
}

func webhookPayload(t *testing.T, apiVersion string, created time.Time, object map[string]any) []byte {
	t.Helper()
	return webhookPayloadForType(t, apiVersion, created, "invoice.created", object)
}

func webhookPayloadForType(t *testing.T, apiVersion string, created time.Time, eventType string, object any) []byte {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		"id":          "evt_test",
		"object":      "event",
		"api_version": apiVersion,
		"created":     created.Unix(),
		"type":        eventType,
		"data": map[string]any{
			"object": object,
		},
	})
	require.NoError(t, err)
	return payload
}
