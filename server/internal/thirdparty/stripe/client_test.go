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
				PriceIDTUM:     "price_test",
				MeterEventName: "tum",
			},
		},
		{
			name:    "missing price",
			catalog: Catalog{MeterEventName: "tum"},
			wantErr: "missing TUM price id",
		},
		{
			name:    "missing meter event name",
			catalog: Catalog{PriceIDTUM: "price_test"},
			wantErr: "missing meter event name",
		},
		{
			name:    "unset price",
			catalog: Catalog{PriceIDTUM: "unset", MeterEventName: "tum"},
			wantErr: "missing TUM price id",
		},
		{
			name:    "unset meter event name",
			catalog: Catalog{PriceIDTUM: "price_test", MeterEventName: "unset"},
			wantErr: "missing meter event name",
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

func TestSDKPinsExpectedAPIVersion(t *testing.T) {
	t.Parallel()

	require.Equal(t, "2026-03-25.dahlia", stripesdk.APIVersion)
}

type fakeStripeAPI struct {
	customerParams         *stripesdk.CustomerCreateParams
	checkoutSessionParams  *stripesdk.CheckoutSessionCreateParams
	checkoutRetrieveParams *stripesdk.CheckoutSessionRetrieveParams
	checkoutSession        *stripesdk.CheckoutSession
	meterEventParams       *stripesdk.BillingMeterEventCreateParams
	calls                  int
	err                    error
}

func (f *fakeStripeAPI) createCustomer(_ context.Context, params *stripesdk.CustomerCreateParams) (*stripesdk.Customer, error) {
	f.calls++
	f.customerParams = params
	return &stripesdk.Customer{ID: "cus_test"}, f.err
}

func (f *fakeStripeAPI) createCheckoutSession(_ context.Context, params *stripesdk.CheckoutSessionCreateParams) (*stripesdk.CheckoutSession, error) {
	f.calls++
	f.checkoutSessionParams = params
	return &stripesdk.CheckoutSession{URL: "https://checkout.stripe.test/session"}, f.err
}

func (f *fakeStripeAPI) retrieveCheckoutSession(_ context.Context, _ string, params *stripesdk.CheckoutSessionRetrieveParams) (*stripesdk.CheckoutSession, error) {
	f.calls++
	f.checkoutRetrieveParams = params
	return f.checkoutSession, f.err
}

func (f *fakeStripeAPI) createMeterEvent(_ context.Context, params *stripesdk.BillingMeterEventCreateParams) (*stripesdk.BillingMeterEvent, error) {
	f.calls++
	f.meterEventParams = params
	return &stripesdk.BillingMeterEvent{}, f.err
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
			PriceIDTUM:     "price_tum",
			MeterEventName: "tum",
		},
	}
	trialEnd := time.Date(2026, time.August, 20, 10, 0, 0, 0, time.UTC)
	billingCycleAnchor := time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC)

	session, err := c.CreateCheckoutSession(t.Context(), CreateCheckoutSessionInput{
		CustomerID:         "cus_test",
		OrganizationID:     "<ORG_ID>",
		OrganizationSlug:   "the-customer",
		SuccessURL:         "https://app.example.test/the-customer/billing",
		CancelURL:          "https://app.example.test/the-customer/billing",
		TrialEnd:           &trialEnd,
		BillingCycleAnchor: billingCycleAnchor,
		IdempotencyKey:     "checkout:<ORG_ID>:request",
	})
	require.NoError(t, err)
	require.Equal(t, "https://checkout.stripe.test/session", session.URL)

	params := api.checkoutSessionParams
	require.Equal(t, "checkout:<ORG_ID>:request", stripesdk.StringValue(params.IdempotencyKey))
	require.Equal(t, "cus_test", stripesdk.StringValue(params.Customer))
	require.Equal(t, "<ORG_ID>", stripesdk.StringValue(params.ClientReferenceID))
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
	require.Equal(t, trialEnd.Unix(), stripesdk.Int64Value(params.SubscriptionData.TrialEnd))
	require.Equal(t, billingCycleAnchor.Unix(), stripesdk.Int64Value(params.SubscriptionData.BillingCycleAnchor))
	require.Equal(t, "none", stripesdk.StringValue(params.SubscriptionData.ProrationBehavior))
	require.Equal(t, "<ORG_ID>", params.SubscriptionData.Metadata[organizationIDMetadataKey])
	require.Equal(t, "the-customer", params.SubscriptionData.Metadata[organizationSlugMetadataKey])
}

func TestCreateCheckoutSessionWithoutTrialStartsImmediately(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{}
	c := &client{api: api, catalog: Catalog{PriceIDTUM: "price_tum", MeterEventName: "tum"}}

	_, err := c.CreateCheckoutSession(t.Context(), CreateCheckoutSessionInput{
		CustomerID:         "",
		OrganizationID:     "",
		OrganizationSlug:   "",
		SuccessURL:         "",
		CancelURL:          "",
		TrialEnd:           nil,
		BillingCycleAnchor: time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC),
		IdempotencyKey:     "checkout",
	})
	require.NoError(t, err)
	require.Nil(t, api.checkoutSessionParams.SubscriptionData.TrialEnd)
}

func TestCreateCheckoutSessionRejectsMissingBillingCycleAnchor(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{}
	c := &client{api: api}

	_, err := c.CreateCheckoutSession(t.Context(), CreateCheckoutSessionInput{IdempotencyKey: "checkout"})
	require.ErrorIs(t, err, errMissingBillingCycleAnchor)
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
		catalog: Catalog{MeterEventName: "tum"},
	}
	timestamp := time.Date(2026, time.August, 14, 10, 0, 0, 0, time.UTC)

	err := c.CreateMeterEvent(t.Context(), CreateMeterEventInput{
		CustomerID:     "cus_test",
		Value:          42,
		Timestamp:      timestamp,
		IdempotencyKey: "meter:<ORG_ID>:1",
	})
	require.NoError(t, err)
	require.Equal(t, "meter:<ORG_ID>:1", stripesdk.StringValue(api.meterEventParams.IdempotencyKey))
	require.Equal(t, "meter:<ORG_ID>:1", stripesdk.StringValue(api.meterEventParams.Identifier))
	require.Equal(t, "tum", stripesdk.StringValue(api.meterEventParams.EventName))
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

func TestWritesWrapStripeErrors(t *testing.T) {
	t.Parallel()

	apiErr := errors.New("request failed")
	api := &fakeStripeAPI{err: apiErr}
	c := &client{api: api, catalog: Catalog{MeterEventName: "tum"}}

	_, err := c.CreateCustomer(t.Context(), CreateCustomerInput{IdempotencyKey: "customer"})
	require.ErrorIs(t, err, apiErr)
	require.ErrorContains(t, err, "create Stripe customer")

	err = c.CreateMeterEvent(t.Context(), CreateMeterEventInput{IdempotencyKey: "meter"})
	require.ErrorIs(t, err, apiErr)
	require.ErrorContains(t, err, "create Stripe meter event")

	_, err = c.CreateCheckoutSession(t.Context(), CreateCheckoutSessionInput{
		BillingCycleAnchor: time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC),
		IdempotencyKey:     "checkout",
	})
	require.ErrorIs(t, err, apiErr)
	require.ErrorContains(t, err, "create Stripe Checkout session")
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

func TestVerifyWebhookRejectsWrongAPIVersion(t *testing.T) {
	t.Parallel()

	const secret = "whsec_test"
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload: webhookPayload(t, "2026-05-27.dahlia", time.Now(), map[string]any{"id": "in_test"}),
		Secret:  secret,
	})
	c := &client{webhookSecret: secret}

	_, err := c.VerifyWebhook(signed.Payload, signed.Header)
	require.ErrorContains(t, err, "expected API version "+stripesdk.APIVersion)
	require.NotErrorIs(t, err, ErrWebhookNotConfigured)
}

func TestStubClientIsSafeToCall(t *testing.T) {
	t.Parallel()

	c := NewStubClient(testenv.NewLogger(t))

	customer, err := c.CreateCustomer(t.Context(), CreateCustomerInput{})
	require.NoError(t, err)
	require.Equal(t, "cus_local_stub", customer.ID)
	checkout, err := c.CreateCheckoutSession(t.Context(), CreateCheckoutSessionInput{OrganizationSlug: "test/org"})
	require.NoError(t, err)
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
