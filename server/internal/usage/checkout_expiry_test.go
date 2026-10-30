package usage

import (
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
	"github.com/stretchr/testify/require"
)

func TestCreateStripeCheckoutRecoversEarlyRemoteExpiry(t *testing.T) {
	t.Parallel()
	ti := newStripeCheckoutTestInstance(t)
	now := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	ti.service.now = func() time.Time { return now }
	first, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	before, err := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.True(t, before.StripeCheckoutExpiresAt.Time.After(now))
	ti.stripe.checkoutState = &stripeclient.CheckoutSessionState{
		ID: before.StripeCheckoutSessionID.String, CustomerID: before.StripeCustomerID.String, Status: "expired",
	}
	// Do not advance time: the old and new anchors/expiration are identical.
	second, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{Theme: "dark"})
	require.NoError(t, err)
	require.NotEqual(t, first.ClientSecret, second.ClientSecret)
	after, err := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.NotEqual(t, before.StripeCheckoutIdempotencyKey.String, after.StripeCheckoutIdempotencyKey.String)
	require.NotEqual(t, before.StripeCheckoutSessionID.String, after.StripeCheckoutSessionID.String)
	require.Equal(t, before.StripeCheckoutExpiresAt.Time, after.StripeCheckoutExpiresAt.Time)
	_, _, inputs := ti.stripe.snapshot()
	require.True(t, inputs[len(inputs)-1].EmbeddedBrandingDarkV1)
	// A subsequent request must reuse the replacement instead of rotating again.
	ti.stripe.checkoutState = &stripeclient.CheckoutSessionState{
		ID: after.StripeCheckoutSessionID.String, CustomerID: after.StripeCustomerID.String, Status: "open",
	}
	third, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	require.Equal(t, second.ClientSecret, third.ClientSecret)
	_, _, replayInputs := ti.stripe.snapshot()
	require.True(t, replayInputs[len(replayInputs)-1].EmbeddedBrandingDarkV1)
}
