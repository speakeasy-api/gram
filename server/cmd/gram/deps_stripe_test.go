package gram

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
)

func TestNewLocalFeatureFlagsEnablesPaygSelfServe(t *testing.T) {
	t.Parallel()

	flags := newLocalFeatureFlags(t.Context(), testenv.NewLogger(t), "")
	enabled, err := flags.IsFlagEnabled(t.Context(), feature.FlagPaygSelfServeBilling, "org-local", nil)
	require.NoError(t, err)
	require.True(t, enabled)
}

func TestNewStripeClientLocalWithoutAPIKeyUsesServerURLForCheckout(t *testing.T) {
	t.Parallel()

	ctx := newStripeCLIContext(t, map[string]string{
		"environment":    "local",
		"stripe-api-key": "unset",
		"server-url":     "https://localhost:8000",
	})

	client, err := newStripeClient(
		t.Context(),
		testenv.NewLogger(t),
		guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)),
		ctx,
	)
	require.NoError(t, err)
	require.NotNil(t, client)
	local, ok := client.(stripeclient.LocalCheckout)
	require.True(t, ok)

	checkout, err := client.CreateCheckoutSession(t.Context(), stripeclient.CreateCheckoutSessionInput{
		CustomerID:       "cus_local_org",
		OrganizationSlug: "billing-test",
		SuccessURL:       "https://localhost:4000/billing-test/billing",
	})
	require.NoError(t, err)
	require.Equal(t, "https://localhost:8000/rpc/stripe.local-checkout?session="+checkout.ID, checkout.URL)

	result, err := local.CompleteCheckout(t.Context(), checkout.ID)
	require.NoError(t, err)
	require.Equal(t, "https://localhost:4000/billing-test/billing", result.SuccessURL)
}

func TestNewStripeClientLocalWithoutAPIKeyUsesStubBeforeCatalogValidation(t *testing.T) {
	t.Parallel()

	ctx := newStripeCLIContext(t, map[string]string{
		"environment":                    "local",
		"stripe-api-key":                 "unset",
		"stripe-price-id-tum":            "partial-price",
		"stripe-meter-id-tum":            "",
		"stripe-meter-event-name":        "",
		"stripe-portal-configuration-id": "",
	})

	client, err := newStripeClient(
		t.Context(),
		testenv.NewLogger(t),
		guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)),
		ctx,
	)
	require.NoError(t, err)
	require.NotNil(t, client)
	require.Equal(t, stripeclient.Catalog{}, client.Catalog())
}

func TestNewAdminStripeClientDegradesInvalidCatalog(t *testing.T) {
	t.Parallel()

	ctx := newStripeCLIContext(t, map[string]string{
		"environment":                    "prod",
		"stripe-api-key":                 "sk_test_placeholder",
		"stripe-price-id-tum":            "partial-price",
		"stripe-meter-id-tum":            "",
		"stripe-meter-event-name":        "",
		"stripe-portal-configuration-id": "",
	})

	client := newAdminStripeClient(
		t.Context(),
		testenv.NewLogger(t),
		guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)),
		ctx,
	)
	require.Nil(t, client)
}

func TestNewStripeClientRealClientValidatesCatalog(t *testing.T) {
	t.Parallel()

	ctx := newStripeCLIContext(t, map[string]string{
		"environment":                    "local",
		"stripe-api-key":                 "sk_test_placeholder",
		"stripe-price-id-tum":            "partial-price",
		"stripe-meter-id-tum":            "mtr_placeholder",
		"stripe-meter-event-name":        "",
		"stripe-portal-configuration-id": "bpc_placeholder",
	})

	client, err := newStripeClient(
		t.Context(),
		testenv.NewLogger(t),
		guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)),
		ctx,
	)
	require.Nil(t, client)
	require.ErrorContains(t, err, "invalid Stripe catalog configuration: missing meter event name")
}

func TestNewStripeClientNonLocalWithoutAPIKeyIsOptional(t *testing.T) {
	t.Parallel()

	ctx := newStripeCLIContext(t, map[string]string{
		"environment":    "prod",
		"stripe-api-key": "unset",
	})

	client, err := newStripeClient(
		t.Context(),
		testenv.NewLogger(t),
		guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)),
		ctx,
	)
	require.NoError(t, err)
	require.Nil(t, client)
}

func TestNewStripeClientRealClientUsesCatalog(t *testing.T) {
	t.Parallel()

	ctx := newStripeCLIContext(t, map[string]string{
		"environment":                    "prod",
		"stripe-api-key":                 "sk_test_placeholder",
		"stripe-webhook-secret":          "whsec_placeholder",
		"stripe-price-id-tum":            "price_placeholder",
		"stripe-meter-id-tum":            "mtr_placeholder",
		"stripe-meter-event-name":        "tum",
		"stripe-portal-configuration-id": "bpc_placeholder",
	})

	client, err := newStripeClient(
		t.Context(),
		testenv.NewLogger(t),
		guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)),
		ctx,
	)
	require.NoError(t, err)
	require.NotNil(t, client)
	require.Equal(t, stripeclient.Catalog{
		PriceIDTUM:            "price_placeholder",
		MeterIDTUM:            "mtr_placeholder",
		MeterEventName:        "tum",
		PortalConfigurationID: "bpc_placeholder",
	}, client.Catalog())
}

func TestNewBillingProviderAcceptsStripeWithoutPolar(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	ctx := newStripeCLIContext(t, map[string]string{
		"environment": "prod",
	})

	repository, tracker, err := newBillingProvider(
		t.Context(),
		logger,
		tracerProvider,
		nil,
		nil,
		nil,
		stripeclient.NewStubClient(logger, nil),
		ctx,
	)
	require.NoError(t, err)
	require.NotNil(t, repository)
	require.NotNil(t, tracker)

	_, _, err = repository.GetCustomerTier(t.Context(), "org_placeholder")
	require.ErrorContains(t, err, "legacy billing operations are unavailable")
}

func TestNewBillingProviderRejectsNonLocalWithoutProvider(t *testing.T) {
	t.Parallel()

	_, _, err := newBillingProvider(
		t.Context(),
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		nil,
		nil,
		nil,
		nil,
		newStripeCLIContext(t, map[string]string{"environment": "prod"}),
	)
	require.ErrorContains(t, err, "billing provider is not configured")
}

func newStripeCLIContext(t *testing.T, values map[string]string) *cli.Context {
	t.Helper()

	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.String("environment", "", "")
	set.String("stripe-api-key", "", "")
	set.String("stripe-webhook-secret", "", "")
	set.String("stripe-price-id-tum", "", "")
	set.String("stripe-meter-id-tum", "", "")
	set.String("stripe-meter-event-name", "", "")
	set.String("stripe-portal-configuration-id", "", "")
	set.String("server-url", "", "")
	set.String("polar-api-key", "", "")
	for key, value := range values {
		require.NoError(t, set.Set(key, value))
	}

	return cli.NewContext(cli.NewApp(), set, nil)
}
