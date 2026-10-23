package stripe

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	stripesdk "github.com/stripe/stripe-go/v85"
)

func TestCreateCheckoutSessionBrandingVersion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                                   string
		embedded, branding, dark, wantBranding bool
	}{
		{"legacy hosted", false, false, false, false},
		{"hosted ignores embedded branding", false, true, false, false},
		{"legacy embedded", true, false, false, false},
		{"embedded v1", true, true, false, true},
		{"embedded dark v1", true, false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := &fakeStripeAPI{}
			c := &client{api: api, catalog: Catalog{PriceIDMCPEgress: "price_mcp_egress", PriceIDRiskScans: "price_risk_scans", PriceIDTUM: "price_tum", PortalConfigurationID: "bpc_test"}}
			input := CreateCheckoutSessionInput{
				CustomerID: "cus_test", OrganizationID: "<ORG_ID>", OrganizationSlug: "the-customer",
				BillingCycleAnchor: time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC),
				ExpiresAt:          time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC),
				IdempotencyKey:     "checkout", EmbeddedBrandingV1: tc.branding, EmbeddedBrandingDarkV1: tc.dark,
			}
			if tc.embedded {
				input.ReturnURL = "https://app.example.test/billing?session_id={CHECKOUT_SESSION_ID}"
			} else {
				input.SuccessURL = "https://app.example.test/billing"
				input.CancelURL = input.SuccessURL
			}
			_, err := c.CreateCheckoutSession(t.Context(), input)
			require.NoError(t, err)
			branding := api.checkoutSessionParams.BrandingSettings
			if !tc.wantBranding {
				require.Nil(t, branding)
				return
			}
			background, button := "#ffffff", "#303030"
			if tc.dark {
				background, button = "#000000", "#ffffff"
			}
			require.Equal(t, &stripesdk.CheckoutSessionCreateBrandingSettingsParams{
				BackgroundColor: stripesdk.String(background), ButtonColor: stripesdk.String(button),
				BorderStyle: stripesdk.String("rectangular"), FontFamily: stripesdk.String("inter"),
			}, branding)
		})
	}
}
