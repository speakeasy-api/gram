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
	customerParams   *stripesdk.CustomerCreateParams
	meterEventParams *stripesdk.BillingMeterEventCreateParams
	calls            int
	err              error
}

func (f *fakeStripeAPI) createCustomer(_ context.Context, params *stripesdk.CustomerCreateParams) (*stripesdk.Customer, error) {
	f.calls++
	f.customerParams = params
	return &stripesdk.Customer{ID: "cus_test"}, f.err
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
}

func TestVerifyWebhook(t *testing.T) {
	t.Parallel()

	const secret = "whsec_test"
	payload := webhookPayload(t, stripesdk.APIVersion)
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload:   payload,
		Secret:    secret,
		Timestamp: time.Now(),
	})
	c := &client{webhookSecret: secret}

	event, err := c.VerifyWebhook(signed.Payload, signed.Header)
	require.NoError(t, err)
	require.Equal(t, "evt_test", event.ID)
	require.Equal(t, "invoice.created", event.Type)
	require.JSONEq(t, `{"id":"in_test"}`, string(event.Data))
}

func TestVerifyWebhookRejectsInvalidSignature(t *testing.T) {
	t.Parallel()

	const secret = "whsec_test"
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload: webhookPayload(t, stripesdk.APIVersion),
		Secret:  secret,
	})
	c := &client{webhookSecret: secret}

	_, err := c.VerifyWebhook(signed.Payload, signed.Header+"bad")
	require.ErrorContains(t, err, "verify Stripe webhook")
}

func TestVerifyWebhookRejectsUnconfiguredSecret(t *testing.T) {
	t.Parallel()

	payload := webhookPayload(t, stripesdk.APIVersion)
	for _, secret := range []string{"", "unset"} {
		c := &client{webhookSecret: secret}
		_, err := c.VerifyWebhook(payload, "")
		require.ErrorContains(t, err, "webhook secret is not configured")
	}
}

func TestVerifyWebhookRejectsReplayedTimestamp(t *testing.T) {
	t.Parallel()

	const secret = "whsec_test"
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload:   webhookPayload(t, stripesdk.APIVersion),
		Secret:    secret,
		Timestamp: time.Now().Add(-stripewebhook.DefaultTolerance - time.Second),
	})
	c := &client{webhookSecret: secret}

	_, err := c.VerifyWebhook(signed.Payload, signed.Header)
	require.ErrorContains(t, err, "timestamp wasn't within tolerance")
}

func TestVerifyWebhookRejectsWrongAPIVersion(t *testing.T) {
	t.Parallel()

	const secret = "whsec_test"
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload: webhookPayload(t, "2026-05-27.dahlia"),
		Secret:  secret,
	})
	c := &client{webhookSecret: secret}

	_, err := c.VerifyWebhook(signed.Payload, signed.Header)
	require.ErrorContains(t, err, "expected API version "+stripesdk.APIVersion)
}

func TestStubClientIsSafeToCall(t *testing.T) {
	t.Parallel()

	c := NewStubClient(testenv.NewLogger(t))

	customer, err := c.CreateCustomer(t.Context(), CreateCustomerInput{})
	require.NoError(t, err)
	require.Equal(t, "cus_local_stub", customer.ID)
	require.NoError(t, c.CreateMeterEvent(t.Context(), CreateMeterEventInput{}))
	require.Equal(t, Catalog{}, c.Catalog())
	_, err = c.VerifyWebhook(nil, "")
	require.Error(t, err)
}

func webhookPayload(t *testing.T, apiVersion string) []byte {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		"id":          "evt_test",
		"object":      "event",
		"api_version": apiVersion,
		"created":     time.Now().Unix(),
		"type":        "invoice.created",
		"data": map[string]any{
			"object": map[string]any{"id": "in_test"},
		},
	})
	require.NoError(t, err)
	return payload
}
