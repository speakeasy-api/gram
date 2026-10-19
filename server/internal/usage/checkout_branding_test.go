package usage

import (
	"fmt"
	"net/http/httptest"

	httpusage "github.com/speakeasy-api/gram/server/gen/http/usage/server"
	goahttp "goa.design/goa/v3/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestCheckoutSessionInputBrandingReplay(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	original := newStripeCheckoutIntent("<ORG_ID>", now, nil)
	require.Contains(t, original.idempotencyKey, ":form-prototype:branding-v1:")
	// Freeze historical formats independently of the current UI-mode marker.
	legacyPrefix := fmt.Sprintf("checkout-session:<ORG_ID>:%d:%d:", original.billingCycleAnchor.Unix(), original.expiresAt.Unix())
	for _, tc := range []struct {
		name, key                      string
		embedded, branding, dark, form bool
	}{
		{"new form", original.idempotencyKey, true, true, false, true},
		{"dark form", withStripeCheckoutTheme(original, "dark").idempotencyKey, true, false, true, true},
		{"legacy light embedded replay", legacyPrefix + "embedded:branding-v1:none", true, true, false, false},
		{"legacy dark embedded replay", legacyPrefix + "embedded:branding-dark-v1:none", true, false, true, false},
		{"legacy embedded replay", legacyPrefix + "embedded:none", true, false, false, false},
		{"legacy hosted replay", legacyPrefix + "none", false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			intent := original
			intent.idempotencyKey = tc.key
			intent, ok := withStripeCheckoutReturnBase(intent, "https://app.example.test")
			require.True(t, ok)
			require.Equal(t, "none", checkoutIntentTrialFingerprint(intent.idempotencyKey))
			// Recover persisted fields exactly as a retry or stale-session recovery does.
			recovered, err := checkoutIntentFromFields(
				pgtype.Text{String: intent.idempotencyKey, Valid: true}, finiteTimestamptz(intent.billingCycleAnchor),
				optionalTimestamptz(intent.trialEnd), finiteTimestamptz(intent.expiresAt),
			)
			require.NoError(t, err)
			billingURL := "https://app.example.test/the-customer/billing"
			input := checkoutSessionInput("cus_test", "<ORG_ID>", "the-customer", recovered, billingURL)
			require.Equal(t, checkoutSessionInput("cus_test", "<ORG_ID>", "the-customer", intent, billingURL), input)
			require.Equal(t, intent.idempotencyKey, input.IdempotencyKey)
			require.Equal(t, tc.branding, input.EmbeddedBrandingV1)
			require.Equal(t, tc.dark, input.EmbeddedBrandingDarkV1)
			require.Equal(t, tc.form, input.EmbeddedFormPrototype)
			require.Equal(t, tc.embedded, input.ReturnURL != "")
			if !tc.embedded {
				require.Equal(t, billingURL, input.SuccessURL)
				require.Equal(t, billingURL, input.CancelURL)
			}
		})
	}
}

func TestCheckoutThemePayload(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, query, want string
		invalid           bool
	}{
		{"old bodyless request", "", "light", false},
		{"explicit light", "?theme=light", "light", false},
		{"dark", "?theme=dark", "dark", false},
		{"invalid", "?theme=system", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			decode := httpusage.DecodeCreateStripeCheckoutRequest(goahttp.NewMuxer(), goahttp.RequestDecoder)
			payload, err := decode(httptest.NewRequest("POST", "/rpc/usage.createStripeCheckout"+tc.query, nil))
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, payload.Theme)
			intent := withStripeCheckoutTheme(newStripeCheckoutIntent("<ORG_ID>", time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), nil), payload.Theme)
			input := checkoutSessionInput("cus_test", "<ORG_ID>", "the-customer", intent, "https://app.example.test/billing")
			require.Equal(t, tc.want == "dark", input.EmbeddedBrandingDarkV1)
			require.Equal(t, tc.want == "light", input.EmbeddedBrandingV1)
		})
	}
}
