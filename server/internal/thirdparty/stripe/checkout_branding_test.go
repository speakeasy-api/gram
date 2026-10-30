package stripe

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	stripesdk "github.com/stripe/stripe-go/v85"
)

func TestCreateCheckoutSessionBrandingVersionLegacyHosted(t *testing.T) {
	t.Parallel()
	testCreateCheckoutSessionBrandingVersion(t, false, false, false, false)
}

func TestCreateCheckoutSessionBrandingVersionHostedIgnoresEmbeddedBranding(t *testing.T) {
	t.Parallel()
	testCreateCheckoutSessionBrandingVersion(t, false, true, false, false)
}

func TestCreateCheckoutSessionBrandingVersionLegacyEmbedded(t *testing.T) {
	t.Parallel()
	testCreateCheckoutSessionBrandingVersion(t, true, false, false, false)
}

func TestCreateCheckoutSessionBrandingVersionEmbeddedV1(t *testing.T) {
	t.Parallel()
	testCreateCheckoutSessionBrandingVersion(t, true, true, false, true)
}

func TestCreateCheckoutSessionBrandingVersionEmbeddedDarkV1(t *testing.T) {
	t.Parallel()
	testCreateCheckoutSessionBrandingVersion(t, true, false, true, true)
}

func testCreateCheckoutSessionBrandingVersion(t *testing.T, tcEmbedded bool, tcBranding bool, tcDark bool, tcWantBranding bool) {
	t.Helper()
	api := &fakeStripeAPI{}
	c := &client{api: api, catalog: Catalog{PriceIDMCPEgress: "price_mcp_egress", PriceIDRiskScans: "price_risk_scans", PriceIDTUM: "price_tum", PortalConfigurationID: "bpc_test"}}
	input := CreateCheckoutSessionInput{
		CustomerID: "cus_test", OrganizationID: "<ORG_ID>", OrganizationSlug: "the-customer",
		BillingCycleAnchor: time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC),
		ExpiresAt:          time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC),
		IdempotencyKey:     "checkout", EmbeddedBrandingV1: tcBranding, EmbeddedBrandingDarkV1: tcDark,
	}
	if tcEmbedded {
		input.ReturnURL = "https://app.example.test/billing?session_id={CHECKOUT_SESSION_ID}"
	} else {
		input.SuccessURL = "https://app.example.test/billing"
		input.CancelURL = input.SuccessURL
	}
	_, err := c.CreateCheckoutSession(t.Context(), input)
	require.NoError(t, err)
	branding := api.checkoutSessionParams.BrandingSettings
	if !tcWantBranding {
		require.Nil(t, branding)
		return
	}
	background, button := "#ffffff", "#303030"
	if tcDark {
		background, button = "#000000", "#ffffff"
	}
	require.Equal(t, &stripesdk.CheckoutSessionCreateBrandingSettingsParams{
		BackgroundColor: stripesdk.String(background), ButtonColor: stripesdk.String(button),
		BorderStyle: stripesdk.String("rectangular"), FontFamily: stripesdk.String("inter"),
	}, branding)
}
