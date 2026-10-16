package stripe

import (
	"github.com/stretchr/testify/require"
	stripesdk "github.com/stripe/stripe-go/v85"
	"testing"
)

func TestEndPartnerCheckoutTrialUsesStableIdempotencyAndNoProration(t *testing.T) {
	t.Parallel()
	api := &fakeStripeAPI{subscription: &stripesdk.Subscription{ID: "sub_partner"}}
	c := &client{api: api}
	err := c.EndPartnerCheckoutTrial(t.Context(), "sub_partner", "checkout:synthetic:partner-conversion")
	require.NoError(t, err)
	require.True(t, stripesdk.BoolValue(api.subscriptionUpdateParams.TrialEndNow))
	require.Equal(t, "none", stripesdk.StringValue(api.subscriptionUpdateParams.ProrationBehavior))
	require.Equal(t, "checkout:synthetic:partner-conversion", stripesdk.StringValue(api.subscriptionUpdateParams.IdempotencyKey))
	require.Nil(t, api.subscriptionUpdateParams.BillingCycleAnchor)
	require.Nil(t, api.subscriptionUpdateParams.BillingCycleAnchorUnchanged)
}
