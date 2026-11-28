package stripe

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	stripesdk "github.com/stripe/stripe-go/v85"
)

func TestCreateTrialSetupCheckoutSessionSavesCardWithoutCharging(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{}
	c := &client{api: api}
	expiresAt := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)

	session, err := c.CreateTrialSetupCheckoutSession(t.Context(), CreateTrialSetupCheckoutSessionInput{
		CustomerID:       "cus_test",
		OrganizationID:   "<ORG_ID>",
		OrganizationSlug: "the-customer",
		UserID:           "<USER_ID>",
		ReturnURL:        "https://app.example.test/start-trial?session_id={CHECKOUT_SESSION_ID}",
		Dark:             true,
		ExpiresAt:        expiresAt,
		IdempotencyKey:   "trial-setup:<ORG_ID>:request",
	})
	require.NoError(t, err)
	require.Equal(t, "cs_test_secret", session.ClientSecret)

	params := api.checkoutSessionParams
	require.Equal(t, "setup", stripesdk.StringValue(params.Mode))
	require.Equal(t, "embedded_page", stripesdk.StringValue(params.UIMode))
	require.Equal(t, "usd", stripesdk.StringValue(params.Currency))
	require.Equal(t, []*string{stripesdk.String("card")}, params.PaymentMethodTypes)
	require.Empty(t, params.LineItems, "setup Checkout charges nothing")
	require.Nil(t, params.SubscriptionData)
	require.Equal(t, "cus_test", stripesdk.StringValue(params.Customer))
	require.Equal(t, expiresAt.Unix(), stripesdk.Int64Value(params.ExpiresAt))
	require.Equal(t, "trial-setup:<ORG_ID>:request", stripesdk.StringValue(params.IdempotencyKey))
	require.Equal(t, "https://app.example.test/start-trial?session_id={CHECKOUT_SESSION_ID}", stripesdk.StringValue(params.ReturnURL))
	require.Equal(t, "#000000", stripesdk.StringValue(params.BrandingSettings.BackgroundColor))
	for _, metadata := range []map[string]string{params.Metadata, params.SetupIntentData.Metadata} {
		require.Equal(t, "<ORG_ID>", metadata[organizationIDMetadataKey])
		require.Equal(t, checkoutPurposeTrialSetup, metadata[checkoutPurposeMetadataKey])
		require.Equal(t, "<USER_ID>", metadata[checkoutUserIDMetadataKey])
	}
}

func TestCreateTrialSetupCheckoutSessionRequiresIdentity(t *testing.T) {
	t.Parallel()

	c := &client{api: &fakeStripeAPI{}}
	_, err := c.CreateTrialSetupCheckoutSession(t.Context(), CreateTrialSetupCheckoutSessionInput{
		CustomerID:       "",
		OrganizationID:   "<ORG_ID>",
		OrganizationSlug: "the-customer",
		UserID:           "",
		ReturnURL:        "https://app.example.test/start-trial",
		Dark:             false,
		ExpiresAt:        time.Now().Add(time.Hour),
		IdempotencyKey:   "trial-setup:<ORG_ID>:request",
	})
	require.Error(t, err)
}

func TestGetTrialSetupCheckoutSessionReadsSavedCard(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{checkoutSession: &stripesdk.CheckoutSession{
		ID:       "cs_test",
		Mode:     stripesdk.CheckoutSessionModeSetup,
		Status:   stripesdk.CheckoutSessionStatusComplete,
		Customer: &stripesdk.Customer{ID: "cus_test"},
		Metadata: map[string]string{
			organizationIDMetadataKey:  "<ORG_ID>",
			checkoutPurposeMetadataKey: checkoutPurposeTrialSetup,
			checkoutUserIDMetadataKey:  "<USER_ID>",
		},
		SetupIntent: &stripesdk.SetupIntent{
			Status: stripesdk.SetupIntentStatusSucceeded,
			PaymentMethod: &stripesdk.PaymentMethod{
				ID:   "pm_test",
				Card: &stripesdk.PaymentMethodCard{Fingerprint: "fingerprint_test", Funding: stripesdk.CardFundingPrepaid},
			},
		},
	}}
	c := &client{api: api}

	state, err := c.GetTrialSetupCheckoutSession(t.Context(), "cs_test")
	require.NoError(t, err)
	require.Equal(t, []string{"setup_intent.payment_method"}, stringValues(api.checkoutRetrieveParams.Expand))
	require.True(t, state.TrialSetup)
	require.True(t, state.CardSaved())
	require.Equal(t, "<ORG_ID>", state.OrganizationID)
	require.Equal(t, "<USER_ID>", state.UserID)
	require.Equal(t, "cus_test", state.CustomerID)
	require.Equal(t, "pm_test", state.PaymentMethodID)
	require.Equal(t, "fingerprint_test", state.CardFingerprint)
	require.Equal(t, "prepaid", state.CardFunding)

	api.checkoutSession.Mode = stripesdk.CheckoutSessionModeSubscription
	state, err = c.GetTrialSetupCheckoutSession(t.Context(), "cs_test")
	require.NoError(t, err)
	require.False(t, state.TrialSetup, "only setup sessions Speakeasy created start trials")
}

func TestSetDefaultPaymentMethodUpdatesInvoiceSettings(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{}
	c := &client{api: api}
	require.NoError(t, c.SetDefaultPaymentMethod(t.Context(), "cus_test", "pm_test"))
	require.Equal(t, "cus_test", api.customerUpdateID)
	require.Equal(t, "pm_test", stripesdk.StringValue(api.customerUpdateParams.InvoiceSettings.DefaultPaymentMethod))
}

func TestDetachPaymentMethodDetachesTheCard(t *testing.T) {
	t.Parallel()

	api := &fakeStripeAPI{}
	c := &client{api: api}
	require.NoError(t, c.DetachPaymentMethod(t.Context(), "pm_test"))
	require.Equal(t, "pm_test", api.detachedPaymentMethodID)
	require.Error(t, c.DetachPaymentMethod(t.Context(), ""))
}

func TestCreateCheckoutSessionOffersSavedCardsWhenRequested(t *testing.T) {
	t.Parallel()

	for _, offer := range []bool{false, true} {
		api := &fakeStripeAPI{}
		c := &client{api: api}
		_, err := c.CreateCheckoutSession(t.Context(), CreateCheckoutSessionInput{
			CustomerID:                   "cus_test",
			RedisplaySavedPaymentMethods: offer,
			BillingCycleAnchor:           time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC),
			ExpiresAt:                    time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC),
			IdempotencyKey:               "checkout",
		})
		require.NoError(t, err)
		if !offer {
			require.Nil(t, api.checkoutSessionParams.SavedPaymentMethodOptions, "replayed intents keep their original parameters")
			continue
		}
		require.Equal(t,
			[]string{"always", "limited", "unspecified"},
			stringValues(api.checkoutSessionParams.SavedPaymentMethodOptions.AllowRedisplayFilters),
		)
	}
}

func stringValues(values []*string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, stripesdk.StringValue(value))
	}
	return out
}
