package usage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
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

			sessionID := "session-polar-return"
			ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
				ActiveOrganizationID: orgID,
				OrganizationSlug:     "polar-org",
				AccountType:          "free",
				SessionID:            &sessionID,
			})
			ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, orgID))
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

func TestCreateStripeCheckoutReturnsToExtraPlatformHost(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	ctx := withPlatformOrigin(ti.adminContext(t), extraPlatformHostURL)

	_, err := ti.service.CreateStripeCheckout(ctx, &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)

	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 1)
	require.Equal(t, extraPlatformHostURL+"/"+ti.orgSlug+"/billing", checkouts[0].SuccessURL)
	require.Equal(t, checkouts[0].SuccessURL, checkouts[0].CancelURL)
	require.Contains(t, checkouts[0].IdempotencyKey, ":"+stripeCheckoutReturnBasePrefix)
	require.Equal(t, "none", checkoutIntentTrialFingerprint(checkouts[0].IdempotencyKey))
}

func TestCreateStripeCheckoutKeepsCanonicalIdempotencyKeyOnSiteHost(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	ctx := withPlatformOrigin(ti.adminContext(t), ti.service.siteURL.String())

	_, err := ti.service.CreateStripeCheckout(ctx, &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)

	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 1)
	require.Equal(t, "https://app.example.test/"+ti.orgSlug+"/billing", checkouts[0].SuccessURL)
	require.NotContains(t, checkouts[0].IdempotencyKey, stripeCheckoutReturnBasePrefix)
}

func TestCreateStripeCheckoutReplaysLiveIntentOnSameExtraHost(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	ctx := withPlatformOrigin(ti.adminContext(t), extraPlatformHostURL)

	first, err := ti.service.CreateStripeCheckout(ctx, &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	second, err := ti.service.CreateStripeCheckout(ctx, &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)

	require.Equal(t, first, second)
	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 2)
	require.Equal(t, checkouts[0], checkouts[1])
}

// A live intent keeps the return host it was created on: switching hosts must
// replay the original input, which the fake Stripe client rejects otherwise.
func TestCreateStripeCheckoutReplaysLiveIntentAfterHostSwitch(t *testing.T) {
	t.Parallel()

	siteBillingPath := "https://app.example.test/"
	for _, tc := range []struct {
		name       string
		firstHost  string
		secondHost string
		wantBase   string
	}{
		{name: "extra host then site host", firstHost: extraPlatformHostURL, secondHost: "", wantBase: extraPlatformHostURL + "/"},
		{name: "site host then extra host", firstHost: "", secondHost: extraPlatformHostURL, wantBase: siteBillingPath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ti := newStripeCheckoutTestInstance(t)
			contextFor := func(host string) context.Context {
				ctx := ti.adminContext(t)
				if host != "" {
					ctx = withPlatformOrigin(ctx, host)
				}
				return ctx
			}

			first, err := ti.service.CreateStripeCheckout(contextFor(tc.firstHost), &gen.CreateStripeCheckoutPayload{})
			require.NoError(t, err)
			second, err := ti.service.CreateStripeCheckout(contextFor(tc.secondHost), &gen.CreateStripeCheckoutPayload{})
			require.NoError(t, err)

			require.Equal(t, first, second)
			_, _, checkouts := ti.stripe.snapshot()
			require.Len(t, checkouts, 2)
			require.Equal(t, checkouts[0], checkouts[1])
			require.Equal(t, tc.wantBase+ti.orgSlug+"/billing", checkouts[1].SuccessURL)
			require.Empty(t, ti.stripe.expiredCheckoutIDs)
		})
	}
}

// Lifecycle recovery replays the stale intent's Create call with the URLs it
// was created with, then starts the replacement on the current request's host.
func TestCreateStripeCheckoutRecoversLifecycleStaleIntentWithOriginalHost(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	trialEnd := time.Now().UTC().Add(7 * 24 * time.Hour)
	require.NoError(t, trialsrepo.New(ti.db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{
		OrganizationID: ti.orgID, Tier: "enterprise",
		EndsAt: pgtype.Timestamptz{Time: trialEnd, InfinityModifier: pgtype.Finite, Valid: true},
	}))
	ti.stripe.afterCheckoutCreate = func() {
		ti.stripe.afterCheckoutCreate = nil
		_, err := trialsrepo.New(ti.db).ExtendTrial(t.Context(), trialsrepo.ExtendTrialParams{OrganizationID: ti.orgID, ExtendByDays: 1})
		require.NoError(t, err)
	}

	_, err := ti.service.CreateStripeCheckout(withPlatformOrigin(ti.adminContext(t), extraPlatformHostURL), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)

	checkoutURL, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	require.Equal(t, "https://checkout.stripe.test/2", checkoutURL)
	require.Equal(t, []string{"cs_1"}, ti.stripe.expiredCheckoutIDs)

	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 3)
	require.Equal(t, checkouts[0], checkouts[1])
	require.Equal(t, extraPlatformHostURL+"/"+ti.orgSlug+"/billing", checkouts[1].SuccessURL)
	require.Equal(t, "https://app.example.test/"+ti.orgSlug+"/billing", checkouts[2].SuccessURL)
}

func TestStripeCheckoutReturnBaseRoundTripsThroughIdempotencyKey(t *testing.T) {
	t.Parallel()

	siteURL := mustParseURL(t, "https://app.example.test")
	intent := newStripeCheckoutIntent("<ORG_ID>", time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), nil)
	fingerprint := checkoutIntentTrialFingerprint(intent.idempotencyKey)

	canonical, err := stripeCheckoutBillingURL(intent.idempotencyKey, siteURL, "acme")
	require.NoError(t, err)
	require.Equal(t, "https://app.example.test/acme/billing", canonical)

	withPort := withStripeCheckoutReturnBase(intent, "http://localhost:5173")
	require.Equal(t, fingerprint, checkoutIntentTrialFingerprint(withPort.idempotencyKey))
	returned, err := stripeCheckoutBillingURL(withPort.idempotencyKey, siteURL, "acme")
	require.NoError(t, err)
	require.Equal(t, "http://localhost:5173/acme/billing", returned)
}

func TestStripeCheckoutReturnBaseOmittedWhenKeyWouldExceedStripeLimit(t *testing.T) {
	t.Parallel()

	siteURL := mustParseURL(t, "https://app.example.test")
	intent := newStripeCheckoutIntent("<ORG_ID>", time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), nil)
	longHost := "https://" + strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + ".example.test"

	withLongHost := withStripeCheckoutReturnBase(intent, longHost)
	require.LessOrEqual(t, len(withLongHost.idempotencyKey), maxStripeIdempotencyKeyLength)
	require.Equal(t, intent.idempotencyKey, withLongHost.idempotencyKey)
	returned, err := stripeCheckoutBillingURL(withLongHost.idempotencyKey, siteURL, "acme")
	require.NoError(t, err)
	require.Equal(t, "https://app.example.test/acme/billing", returned)

	withExtraHost := withStripeCheckoutReturnBase(intent, extraPlatformHostURL)
	require.LessOrEqual(t, len(withExtraHost.idempotencyKey), maxStripeIdempotencyKeyLength)
	returned, err = stripeCheckoutBillingURL(withExtraHost.idempotencyKey, siteURL, "acme")
	require.NoError(t, err)
	require.Equal(t, extraPlatformHostURL+"/acme/billing", returned)
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
