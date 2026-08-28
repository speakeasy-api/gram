package gram

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
)

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

func TestNewStripeMeterEventClientLocalWithoutAPIKeyUsesNoop(t *testing.T) {
	t.Parallel()

	client, err := newStripeMeterEventClient(
		guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)),
		newStripeCLIContext(t, map[string]string{
			"environment":    "local",
			"stripe-api-key": "unset",
		}),
	)
	require.NoError(t, err)
	require.NoError(t, client.CreateMeterEvent(t.Context(), stripeclient.V2MeterEventInput{}))
}

func TestNewStripeMeterEventClientLocalWithAPIKeyUsesRealClient(t *testing.T) {
	t.Parallel()

	client, err := newStripeMeterEventClient(
		guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)),
		newStripeCLIContext(t, map[string]string{
			"environment":    "local",
			"stripe-api-key": "sk_test_placeholder",
		}),
	)
	require.NoError(t, err)
	require.ErrorContains(t, client.CreateMeterEvent(t.Context(), stripeclient.V2MeterEventInput{}), "identifier is required")
}

func TestNewStripeMeterEventClientNonLocalWithoutAPIKeyFails(t *testing.T) {
	t.Parallel()

	client, err := newStripeMeterEventClient(
		guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)),
		newStripeCLIContext(t, map[string]string{
			"environment":    "prod",
			"stripe-api-key": "unset",
		}),
	)
	require.Nil(t, client)
	require.ErrorContains(t, err, "stripe API key is required")
}

func TestNewStripeCatalogMapsTUMMeter(t *testing.T) {
	t.Parallel()

	catalog := newStripeCatalog(newStripeCLIContext(t, map[string]string{
		"stripe-meter-event-name":       "tum",
		stripeTUMMeterStreamingFlagName: "true",
	}))

	eventName, err := catalog.MeterEventName(metering.AgentSessionStorage())
	require.NoError(t, err)
	require.Equal(t, "tum", eventName)
}

func TestNewStripeCatalogDropsTUMMeterWhenStreamingDisabled(t *testing.T) {
	t.Parallel()

	catalog := newStripeCatalog(newStripeCLIContext(t, map[string]string{
		"stripe-meter-event-name": "tum",
	}))

	eventName, err := catalog.MeterEventName(metering.AgentSessionStorage())
	require.NoError(t, err)
	require.Empty(t, eventName)
}

func TestNewStripeCatalogRejectsUnmappedMeter(t *testing.T) {
	t.Parallel()

	catalog := newStripeCatalog(newStripeCLIContext(t, map[string]string{
		"stripe-meter-event-name": "tum",
	}))

	eventName, err := catalog.MeterEventName(metering.Definition{})
	require.Empty(t, eventName)
	require.ErrorContains(t, err, "meter definition is not mapped to Stripe")
}

func TestNewStripeCatalogRejectsMissingTUMEventName(t *testing.T) {
	t.Parallel()

	catalog := newStripeCatalog(newStripeCLIContext(t, map[string]string{
		"stripe-meter-event-name":       "unset",
		stripeTUMMeterStreamingFlagName: "true",
	}))

	eventName, err := catalog.MeterEventName(metering.AgentSessionStorage())
	require.Empty(t, eventName)
	require.ErrorContains(t, err, "stripe TUM meter event name is not configured")
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
		stripeclient.NewStubClient(logger),
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
	set.Bool(stripeTUMMeterStreamingFlagName, false, "")
	set.String("polar-api-key", "", "")
	for key, value := range values {
		require.NoError(t, set.Set(key, value))
	}

	return cli.NewContext(cli.NewApp(), set, nil)
}
