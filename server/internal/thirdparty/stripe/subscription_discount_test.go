package stripe

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	stripesdk "github.com/stripe/stripe-go/v85"
)

func discountSubscription(t *testing.T) *stripesdk.Subscription {
	t.Helper()
	sub := testSubscription()
	// Deserialize the actual Stripe shape, including expandable product IDs and
	// the new discount.source.coupon nesting, rather than an application DTO.
	var gateway stripesdk.SubscriptionItem
	require.NoError(t, json.Unmarshal([]byte(`{"price":{"id":"price_gateway","currency":"usd","billing_scheme":"per_unit","unit_amount_decimal":"0.000001862645","product":"prod_gateway","recurring":{"interval":"month","interval_count":1,"usage_type":"metered"}}}`), &gateway))
	sub.Items.Data = append(sub.Items.Data, &gateway)
	var discount stripesdk.Discount
	require.NoError(t, json.Unmarshal([]byte(`{"id":"di_test","subscription":"sub_test","start":1,"end":4102444800,"source":{"type":"coupon","coupon":{"id":"coupon_test","amount_off":10000,"currency":"usd","duration":"repeating","duration_in_months":12,"applies_to":{"products":["prod_gateway"]}}}}`), &discount))
	sub.Discounts = []*stripesdk.Discount{&discount}
	return sub
}

func TestSubscriptionDiscountVerifiedGateway(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(*stripesdk.Subscription) {}, "5 GiB of gateway usage free each month ($100 discount).", true)
}

func TestSubscriptionDiscountOnceCannotVerifyRemainingBenefit(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(s *stripesdk.Subscription) { s.Discounts[0].Source.Coupon.Duration = stripesdk.CouponDurationOnce }, "", false)
}

func TestSubscriptionDiscountRepeatingWithoutEnd(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(s *stripesdk.Subscription) { s.Discounts[0].End = 0 }, "", false)
}

func TestSubscriptionDiscountExpired(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(s *stripesdk.Subscription) { s.Discounts[0].End = 2 }, "", false)
}

func TestSubscriptionDiscountRemoved(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(s *stripesdk.Subscription) { s.Discounts = nil }, "", false)
}

func TestSubscriptionDiscountDeleted(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(s *stripesdk.Subscription) { s.Discounts[0].Deleted = true }, "", false)
}

func TestSubscriptionDiscountFuture(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(s *stripesdk.Subscription) { s.Discounts[0].Start = 4102444800 }, "", false)
}

func TestSubscriptionDiscountCanceled(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(s *stripesdk.Subscription) { s.Status = stripesdk.SubscriptionStatusCanceled }, "", false)
}

func TestSubscriptionDiscountTrialing(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(s *stripesdk.Subscription) { s.Status = stripesdk.SubscriptionStatusTrialing }, "", false)
}

func TestSubscriptionDiscountUnexpanded(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(s *stripesdk.Subscription) { s.Discounts[0].Source = nil }, "", false)
}

func TestSubscriptionDiscountStacked(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(s *stripesdk.Subscription) { s.Discounts = append(s.Discounts, s.Discounts[0]) }, "", false)
}

func TestSubscriptionDiscountAllProducts(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(s *stripesdk.Subscription) { s.Discounts[0].Source.Coupon.AppliesTo = nil }, "$100 discount on eligible charges.", false)
}

func TestSubscriptionDiscountOtherProduct(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(s *stripesdk.Subscription) {
		s.Discounts[0].Source.Coupon.AppliesTo.Products = []string{"prod_other"}
	}, "$100 discount on eligible charges.", false)
}

func TestSubscriptionDiscountMultiProduct(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(s *stripesdk.Subscription) {
		s.Discounts[0].Source.Coupon.AppliesTo.Products = []string{"prod_gateway", "prod_other"}
	}, "$100 discount on eligible charges.", false)
}

func TestSubscriptionDiscountDifferentPrice(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(s *stripesdk.Subscription) { s.Items.Data[1].Price.UnitAmountDecimal = 0.000003 }, "$100 discount on gateway usage.", false)
}

func TestSubscriptionDiscountAnnual(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(s *stripesdk.Subscription) {
		s.Items.Data[1].Price.Recurring.Interval = stripesdk.PriceRecurringIntervalYear
	}, "$100 discount on gateway usage.", false)
}

func TestSubscriptionDiscountDifferentAmount(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(s *stripesdk.Subscription) { s.Discounts[0].Source.Coupon.AmountOff = 5000 }, "$50 discount on gateway usage.", false)
}

func TestSubscriptionDiscountPercentage(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(s *stripesdk.Subscription) { c := s.Discounts[0].Source.Coupon; c.AmountOff = 0; c.PercentOff = 25 }, "25% discount on gateway usage.", false)
}

func TestSubscriptionDiscountForeignCurrency(t *testing.T) {
	t.Parallel()
	testSubscriptionDiscount(t, func(s *stripesdk.Subscription) { s.Discounts[0].Source.Coupon.Currency = stripesdk.CurrencyEUR }, "", false)
}

func testSubscriptionDiscount(t *testing.T, change func(*stripesdk.Subscription), want string, wantKnown bool) {
	t.Helper()
	s := discountSubscription(t)
	change(s)
	c := &client{catalog: Catalog{PriceIDMCPEgress: "price_gateway"}}
	description, end, known := c.subscriptionDiscount(s, time.Unix(100, 0))
	if want == "" {
		require.Nil(t, description)
		require.True(t, end.IsZero())
	} else {
		require.Equal(t, want, *description)
		require.Equal(t, time.Unix(4102444800, 0).UTC(), end)
	}
	require.Equal(t, wantKnown, known)
}

func TestSubscriptionDiscountStripeTransport(t *testing.T) {
	t.Parallel()
	sub := discountSubscription(t)
	type handlerResult struct {
		request   *http.Request
		parseErr  error
		encodeErr error
	}
	results := make(chan handlerResult, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := handlerResult{request: r, parseErr: r.ParseForm(), encodeErr: nil}
		w.Header().Set("Content-Type", "application/json")
		result.encodeErr = json.NewEncoder(w).Encode(sub)
		results <- result
	}))
	assertRequest := func() {
		t.Helper()
		result := <-results
		require.NoError(t, result.parseErr)
		require.NoError(t, result.encodeErr)
		require.Equal(t, "/v1/subscriptions/sub_test", result.request.URL.Path)
		require.ElementsMatch(t, []string{"latest_invoice", "discounts.source.coupon.applies_to"}, []string{result.request.Form.Get("expand[0]"), result.request.Form.Get("expand[1]")})
	}
	defer server.Close()
	backend := stripesdk.GetBackendWithConfig(stripesdk.APIBackend, &stripesdk.BackendConfig{URL: stripesdk.String(server.URL), HTTPClient: server.Client()})
	c := &client{api: &sdkAPI{client: stripesdk.NewClient("sk_test_placeholder", stripesdk.WithBackends(&stripesdk.Backends{API: backend}))}, catalog: Catalog{PriceIDTUM: "price_tum", PriceIDMCPEgress: "price_gateway"}}
	state, err := c.GetSubscription(t.Context(), "sub_test")
	require.NoError(t, err)
	assertRequest()
	require.Equal(t, "5 GiB of gateway usage free each month ($100 discount).", *state.DiscountDescription)
	require.True(t, state.GatewayPromotionApplied)
	require.Equal(t, time.Unix(4102444800, 0).UTC(), state.DiscountEndsAt)
	state, err = c.SetSubscriptionCancelAtPeriodEnd(t.Context(), SetSubscriptionCancelAtPeriodEndInput{SubscriptionID: "sub_test", CancelAtPeriodEnd: true})
	require.NoError(t, err)
	assertRequest()
	require.True(t, state.GatewayPromotionApplied)
}
