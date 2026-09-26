package usage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

const extraPlatformHostURL = "https://ai.example.test"

func withPlatformOrigin(ctx context.Context, baseURL string) context.Context {
	return requestorigin.WithContext(ctx, requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: baseURL})
}

func TestPolarCheckoutsReturnToRequestPlatformHost(t *testing.T) {
	t.Parallel()

	const serverURL = "https://app.example.test"
	for _, tc := range []struct {
		name     string
		origin   string
		wantBase string
	}{
		{name: "no origin", origin: "", wantBase: serverURL},
		{name: "server host", origin: serverURL, wantBase: serverURL},
		{name: "extra platform host", origin: extraPlatformHostURL, wantBase: extraPlatformHostURL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			orgID := "org-polar-return"
			successURL := tc.wantBase + "/polar-org/billing"
			billingMock := &mockBillingRepo{}
			billingMock.On("CreateCheckout", mock.Anything, orgID, tc.wantBase, successURL).Return("https://polar.test/checkout", nil)
			billingMock.On("CreateTopUpCheckout", mock.Anything, orgID, tc.wantBase, successURL).Return("https://polar.test/topup", nil)

			svc := newTestService(t, billingMock, orgID, 0)
			svc.serverURL = mustParseURL(t, serverURL)

			ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
				ActiveOrganizationID: orgID,
				OrganizationSlug:     "polar-org",
				AccountType:          "free",
			})
			if tc.origin != "" {
				ctx = withPlatformOrigin(ctx, tc.origin)
			}

			_, err := svc.CreateCheckout(ctx, &gen.CreateCheckoutPayload{})
			require.NoError(t, err)
			_, err = svc.CreateTopUpCheckout(ctx, &gen.CreateTopUpCheckoutPayload{})
			require.NoError(t, err)
			billingMock.AssertExpectations(t)
		})
	}
}

// Stripe Checkout keeps the site URL on every host: a live checkout intent
// replays its idempotency key with byte-identical input for up to a day, so a
// per-host return URL would fail the replay when a user switches hosts.
func TestCreateStripeCheckoutKeepsSiteURLOnExtraPlatformHost(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	ctx := withPlatformOrigin(ti.adminContext(t), extraPlatformHostURL)

	_, err := ti.service.CreateStripeCheckout(ctx, &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)

	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 1)
	require.Equal(t, "https://app.example.test/"+ti.orgSlug+"/billing", checkouts[0].SuccessURL)
	require.Equal(t, checkouts[0].SuccessURL, checkouts[0].CancelURL)
}

func TestCreateStripePortalSessionReturnsToRequestPlatformHost(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	configureStripeSubscription(t, ti, false)
	ctx := withPlatformOrigin(ti.adminContext(t), extraPlatformHostURL)

	_, err := ti.service.CreateStripePortalSession(ctx, &gen.CreateStripePortalSessionPayload{})
	require.NoError(t, err)
	require.Len(t, ti.stripe.portalInputs, 1)
	require.Equal(t, extraPlatformHostURL+"/"+ti.orgSlug+"/billing", ti.stripe.portalInputs[0].ReturnURL)
}
