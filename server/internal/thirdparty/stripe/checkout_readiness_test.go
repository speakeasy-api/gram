package stripe

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCheckoutPaymentReady(t *testing.T) {
	t.Parallel()
	var missing *CheckoutSessionState
	require.False(t, missing.PaymentReady())
	for _, payment := range []string{"paid", "no_payment_required", "unpaid", "", "unknown"} {
		for _, subscription := range []string{"active", "trialing", "past_due", "canceled", "unpaid", "incomplete", "incomplete_expired", "paused", ""} {
			t.Run(payment+"/"+subscription, func(t *testing.T) {
				t.Parallel()
				state := &CheckoutSessionState{PaymentStatus: payment, SubscriptionStatus: subscription}
				want := payment == "paid" || payment == "no_payment_required" || (payment == "unpaid" && subscription == "trialing")
				require.Equal(t, want, state.PaymentReady())
			})
		}
	}
}
