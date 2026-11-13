package usage

import (
	"errors"
	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/oops"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestCreateStripeCheckoutRemoteStateOpenSessionStaysReusable(t *testing.T) {
	t.Parallel()
	testCreateStripeCheckoutRemoteState(t, "open", "cus_test", "", false, false, "")
}

func TestCreateStripeCheckoutRemoteStateOpenAfterIntendedExpiryFailsClosed(t *testing.T) {
	t.Parallel()
	testCreateStripeCheckoutRemoteState(t, "open", "cus_test", "", true, false, oops.CodeConflict)
}

func TestCreateStripeCheckoutRemoteStateCompleteBeforeIntendedExpiry(t *testing.T) {
	t.Parallel()
	testCreateStripeCheckoutRemoteState(t, "complete", "cus_test", "", false, false, oops.CodeConflict)
}

func TestCreateStripeCheckoutRemoteStateExpiredWithSubscription(t *testing.T) {
	t.Parallel()
	testCreateStripeCheckoutRemoteState(t, "expired", "cus_test", "sub_test", false, false, oops.CodeConflict)
}

func TestCreateStripeCheckoutRemoteStateOpenWithSubscription(t *testing.T) {
	t.Parallel()
	testCreateStripeCheckoutRemoteState(t, "open", "cus_test", "sub_test", false, false, oops.CodeConflict)
}

func TestCreateStripeCheckoutRemoteStateWrongCustomer(t *testing.T) {
	t.Parallel()
	testCreateStripeCheckoutRemoteState(t, "expired", "cus_other", "", false, false, oops.CodeUnavailable)
}

func TestCreateStripeCheckoutRemoteStateUnknownStatus(t *testing.T) {
	t.Parallel()
	testCreateStripeCheckoutRemoteState(t, "unknown", "cus_test", "", false, false, oops.CodeConflict)
}

func TestCreateStripeCheckoutRemoteStateReadFailure(t *testing.T) {
	t.Parallel()
	testCreateStripeCheckoutRemoteState(t, "", "", "", false, true, oops.CodeUnavailable)
}

func testCreateStripeCheckoutRemoteState(t *testing.T, status, customer, subscription string, localExpired, readError bool, wantCode oops.Code) {
	t.Helper()
	ti := newStripeCheckoutTestInstance(t)
	now := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	ti.service.now = func() time.Time { return now }
	first, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	_, _, initial := ti.stripe.snapshot()
	require.Len(t, initial, 1)
	ti.stripe.checkoutState = &stripeclient.CheckoutSessionState{
		ID: "cs_1", CustomerID: customer, Status: status, SubscriptionID: subscription,
	}
	if customer == "cus_test" {
		ti.stripe.checkoutState.CustomerID = initial[0].CustomerID
	}
	if localExpired {
		now = initial[0].ExpiresAt
	}
	if readError {
		ti.stripe.checkoutGetErr = errors.New("read failed")
	}
	result, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{Theme: "dark"})
	_, _, checkouts := ti.stripe.snapshot()
	require.Empty(t, ti.stripe.expiredCheckoutIDs)
	if wantCode != "" {
		require.Error(t, err)
		requireOopsCode(t, err, wantCode)
		require.Nil(t, result)
		// Reconciliation failures must not create a replacement at Stripe.
		require.Equal(t, initial, checkouts)
		return
	}
	require.NoError(t, err)
	require.Equal(t, first, result)
	require.Len(t, checkouts, 2)
	require.Equal(t, initial[0], checkouts[1])
}
