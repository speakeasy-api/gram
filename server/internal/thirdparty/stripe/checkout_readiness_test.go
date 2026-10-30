package stripe

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCheckoutPaymentReadyMissingSession(t *testing.T) {
	t.Parallel()
	var missing *CheckoutSessionState
	require.False(t, missing.PaymentReady())
}

// PaymentReady checks only the payment gate; activation also checks lifecycle state.
func TestCheckoutPaymentReadyPaid(t *testing.T) {
	t.Parallel()
	state := &CheckoutSessionState{PaymentStatus: "paid", SubscriptionStatus: "active"}
	require.True(t, state.PaymentReady())
}

func TestCheckoutPaymentReadyNoPaymentRequired(t *testing.T) {
	t.Parallel()
	state := &CheckoutSessionState{PaymentStatus: "no_payment_required", SubscriptionStatus: "active"}
	require.True(t, state.PaymentReady())
}

func TestCheckoutPaymentReadyUnpaidTrialing(t *testing.T) {
	t.Parallel()
	state := &CheckoutSessionState{PaymentStatus: "unpaid", SubscriptionStatus: "trialing"}
	require.True(t, state.PaymentReady())
}

func TestCheckoutPaymentReadyUnpaidActive(t *testing.T) {
	t.Parallel()
	state := &CheckoutSessionState{PaymentStatus: "unpaid", SubscriptionStatus: "active"}
	require.False(t, state.PaymentReady())
}

func TestCheckoutPaymentReadyUnpaidIncomplete(t *testing.T) {
	t.Parallel()
	state := &CheckoutSessionState{PaymentStatus: "unpaid", SubscriptionStatus: "incomplete"}
	require.False(t, state.PaymentReady())
}

func TestCheckoutPaymentReadyUnpaidMissingSubscription(t *testing.T) {
	t.Parallel()
	state := &CheckoutSessionState{PaymentStatus: "unpaid", SubscriptionStatus: ""}
	require.False(t, state.PaymentReady())
}

func TestCheckoutPaymentReadyEmptyPaymentTrialing(t *testing.T) {
	t.Parallel()
	state := &CheckoutSessionState{PaymentStatus: "", SubscriptionStatus: "trialing"}
	require.False(t, state.PaymentReady())
}

func TestCheckoutPaymentReadyUnknownPaymentTrialing(t *testing.T) {
	t.Parallel()
	state := &CheckoutSessionState{PaymentStatus: "unknown", SubscriptionStatus: "trialing"}
	require.False(t, state.PaymentReady())
}
