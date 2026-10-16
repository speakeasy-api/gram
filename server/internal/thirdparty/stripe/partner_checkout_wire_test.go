package stripe

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	stripesdk "github.com/stripe/stripe-go/v85"
)

func TestPartnerCheckoutWireAppliedPromotionAndTrialEnding(t *testing.T) {
	t.Parallel()
	c := partnerWireClient(t, []string{"prod_gateway"})
	state, err := c.GetCheckoutSession(t.Context(), "cs_test")
	require.NoError(t, err)
	require.Equal(t, "promo_applied", state.Partner.PromotionCodeID)
	eligible, err := c.VerifyCheckoutGatewayScope(t.Context(), state.Partner)
	require.NoError(t, err)
	require.True(t, eligible)
	require.True(t, state.Partner.Monthly)
	require.Equal(t, "pm_collected", state.Partner.PaymentMethodID)
	require.Equal(t, "sub_test", state.SubscriptionID)
	require.NoError(t, c.EndPartnerCheckoutTrial(t.Context(), state.SubscriptionID, "partner-trial-end:cs_test"))
}

func TestPartnerCheckoutWireRejectsUnrelatedCouponScope(t *testing.T) {
	t.Parallel()
	c := partnerWireClient(t, []string{"prod_other"})
	state, err := c.GetCheckoutSession(t.Context(), "cs_test")
	require.NoError(t, err)
	require.Equal(t, "promo_applied", state.Partner.PromotionCodeID)
	eligible, err := c.VerifyCheckoutGatewayScope(t.Context(), state.Partner)
	require.NoError(t, err)
	require.False(t, eligible)
}

func partnerWireClient(t *testing.T, products []string) *client {
	t.Helper()
	type request struct {
		method, path, version, key string
		form                       url.Values
		err                        error
	}
	requests := make(chan request, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		requests <- request{method: r.Method, path: r.URL.Path, version: r.Header.Get("Stripe-Version"), key: r.Header.Get("Idempotency-Key"), form: r.Form, err: err}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/checkout/sessions/cs_test":
			_, _ = w.Write([]byte(`{"id":"cs_test","status":"complete","customer":"cus_test","subscription":{"id":"sub_test","customer":"cus_test","status":"trialing","billing_cycle_anchor":1906502400,"trial_end":1906502400,"default_payment_method":"pm_collected","discounts":[{"id":"di_test","subscription":"sub_test","promotion_code":"promo_applied","source":{"type":"coupon","coupon":"coupon_gateway"}}],"items":{"data":[{"id":"si_gateway","price":{"id":"price_gateway","product":"prod_gateway","recurring":{"interval":"month","interval_count":1,"usage_type":"metered"}}}]}}}`))
		case "GET /v1/coupons/coupon_gateway":
			if products == nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":{"type":"api_error","message":"coupon scope unavailable"}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "coupon_gateway", "valid": false, "max_redemptions": 1, "times_redeemed": 1, "applies_to": map[string]any{"products": products}})
		case "POST /v1/subscriptions/sub_test":
			_, _ = w.Write([]byte(`{"id":"sub_test","status":"active"}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(func() {
		server.Close()
		close(requests)
		for r := range requests {
			require.NoError(t, r.err)
			require.Equal(t, "2026-03-25.dahlia", r.version)
			switch r.method + " " + r.path {
			case "GET /v1/checkout/sessions/cs_test":
				require.Equal(t, "subscription", r.form.Get("expand[0]"))
				require.Equal(t, "subscription.discounts", r.form.Get("expand[1]"))
			case "GET /v1/coupons/coupon_gateway":
				require.Equal(t, "applies_to", r.form.Get("expand[0]"))
			case "POST /v1/subscriptions/sub_test":
				require.Equal(t, "partner-trial-end:cs_test", r.key)
				require.Equal(t, "now", r.form.Get("trial_end"))
				require.Equal(t, "none", r.form.Get("proration_behavior"))
				require.Len(t, r.form, 2)
			default:
				require.Fail(t, "unexpected Stripe request", r.method+" "+r.path)
			}
		}
	})
	backend := stripesdk.GetBackendWithConfig(stripesdk.APIBackend, &stripesdk.BackendConfig{URL: stripesdk.String(server.URL), HTTPClient: server.Client()})
	return &client{api: &sdkAPI{client: stripesdk.NewClient("sk_test_placeholder", stripesdk.WithBackends(&stripesdk.Backends{API: backend}))}, catalog: Catalog{PriceIDMCPEgress: "price_gateway"}}
}

func TestCheckoutIdentityReadSurvivesCouponScopeOutage(t *testing.T) {
	t.Parallel()
	c := partnerWireClient(t, nil)
	state, err := c.GetCheckoutSession(t.Context(), "cs_test")
	require.NoError(t, err)
	require.Equal(t, "promo_applied", state.Partner.PromotionCodeID)
	require.Equal(t, "coupon_gateway", state.Partner.CouponID)
	eligible, err := c.VerifyCheckoutGatewayScope(t.Context(), state.Partner)
	require.ErrorContains(t, err, "coupon scope unavailable")
	require.False(t, eligible)
}
