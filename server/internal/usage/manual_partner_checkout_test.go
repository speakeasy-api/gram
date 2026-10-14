package usage

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	adminrepo "github.com/speakeasy-api/gram/server/internal/admin/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type manualPartnerStripe struct {
	scopeError    error
	scopeEligible bool
	*fakeStripeWebhookClient
	mock.Mock
}

func (c *manualPartnerStripe) VerifyCheckoutGatewayScope(_ context.Context, _ *stripeclient.CheckoutPartnerState) (bool, error) {
	return c.scopeEligible, c.scopeError
}

func (c *manualPartnerStripe) EndPartnerCheckoutTrial(ctx context.Context, subscriptionID, key string) error {
	return c.Called(ctx, subscriptionID, key).Error(0)
}

func newManualPartnerCheckout(t *testing.T, enabled bool) (*Service, *pgxpool.Pool, *manualPartnerStripe, adminrepo.SignupOffer) {
	t.Helper()
	s, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, s, "event_manual_partner", "subscription_partner", "trialing")
	base, ok := s.stripeClient.(*fakeStripeWebhookClient)
	require.True(t, ok)
	client := &manualPartnerStripe{fakeStripeWebhookClient: base, scopeEligible: true}
	client.Test(t)
	t.Cleanup(func() { client.AssertExpectations(t) })
	s.stripeClient = client
	base.checkout.Partner = &stripeclient.CheckoutPartnerState{PromotionCodeID: "promo_manual", CouponID: "coupon_manual", GatewayProductID: "prod_gateway", PaymentMethodID: "pm_synthetic", Monthly: true, TrialEnd: time.Now().UTC().Add(7 * 24 * time.Hour)}
	q := adminrepo.New(db)
	offer, err := q.CreateSignupOffer(t.Context(), adminrepo.CreateSignupOfferParams{Name: "Synthetic partner offer", StripePromotionCodeID: "promo_manual", SignupFlow: "tailscale_partner_gateway"})
	require.NoError(t, err)
	offer, err = q.UpdateSignupOffer(t.Context(), adminrepo.UpdateSignupOfferParams{ID: offer.ID, Name: offer.Name, StripePromotionCodeID: offer.StripePromotionCodeID, SignupFlow: offer.SignupFlow, Enabled: enabled})
	require.NoError(t, err)
	now := time.Now().UTC()
	end := now.Add(7 * 24 * time.Hour)
	prepared, err := s.prepareStripeCheckoutIntent(t.Context(), stripeWebhookOrganizationID, "customer_placeholder", now, newStripeCheckoutIntent(stripeWebhookOrganizationID, now, &end), pgtype.Text{}, pgtype.Text{})
	require.NoError(t, err)
	_, err = repo.New(db).FinalizeStripeCheckoutIntent(t.Context(), repo.FinalizeStripeCheckoutIntentParams{OrganizationID: stripeWebhookOrganizationID, StripeCustomerID: "customer_placeholder", StripeCheckoutSessionID: base.checkout.ID, StripeCheckoutIdempotencyKey: prepared.idempotencyKey, StripeCheckoutBillingCycleAnchor: finiteTimestamptz(prepared.billingCycleAnchor), StripeCheckoutTrialEnd: optionalTimestamptz(prepared.trialEnd), StripeCheckoutExpiresAt: finiteTimestamptz(prepared.expiresAt)})
	require.NoError(t, err)
	return s, db, client, offer
}

func TestManualPartnerTrialEndsBeforeAtomicActivation(t *testing.T) {
	t.Parallel()
	s, db, client, _ := newManualPartnerCheckout(t, true)
	anchor := time.Date(2028, time.January, 31, 13, 17, 0, 0, time.UTC)
	client.On("EndPartnerCheckoutTrial", mock.Anything, "subscription_partner", "partner-trial-end:"+client.checkout.ID).Run(func(mock.Arguments) {
		app, err := orgrepo.New(db).GetOrganizationPromotionApplication(t.Context(), stripeWebhookOrganizationID)
		require.NoError(t, err)
		require.False(t, app.RedeemedAt.Valid)
		require.Zero(t, stripeWebhookReceiptCount(t, db))
		client.checkout.SubscriptionStatus = "active"
		client.checkout.BillingCycleAnchor = anchor
		client.checkout.Partner.TrialEnd = anchor
	}).Return(nil).Once()
	require.Equal(t, http.StatusOK, serveStripeWebhook(s, "paid conversion").Code)
	app, err := orgrepo.New(db).GetOrganizationPromotionApplication(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.True(t, app.RedeemedAt.Valid)
	metadata, err := repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.True(t, metadata.StripeBillingCycleAnchor.Time.Equal(anchor))
	require.False(t, metadata.StripeCheckoutSessionID.Valid)
	enabled, err := featurerepo.New(db).IsFeatureEnabled(t.Context(), featurerepo.IsFeatureEnabledParams{OrganizationID: stripeWebhookOrganizationID, FeatureName: "network_ingress"})
	require.NoError(t, err)
	require.True(t, enabled)
}

func TestManualPartnerPendingInvoiceRetriesUntilActivation(t *testing.T) {
	t.Parallel()
	s, db, client, _ := newManualPartnerCheckout(t, true)
	anchor := time.Date(2028, time.January, 31, 13, 17, 0, 0, time.UTC)
	client.On("EndPartnerCheckoutTrial", mock.Anything, "subscription_partner", "partner-trial-end:"+client.checkout.ID).Return(errors.New("Stripe unavailable")).Once()
	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(s, "interrupted").Code)
	completion := client.verify
	invoice := &stripeclient.InvoiceState{ID: "invoice_partner_pending", CustomerID: "customer_placeholder", SubscriptionID: "subscription_partner", Currency: "usd", BillingReason: "subscription_cycle", Status: "draft", ServicePeriodStart: anchor, ServicePeriodEnd: time.Date(2028, time.February, 29, 13, 17, 0, 0, time.UTC), FinalizedAt: time.Time{}, AmountRemaining: 0}
	s.stripeClient = client.fakeStripeWebhookClient
	configureInvoiceWebhook(t, s, "event_pending_invoice", invoice)
	s.stripeClient = client
	invoiceEvent := client.verify
	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(s, "invoice before activation").Code)
	require.Empty(t, stripeInvoices(t, db))
	require.Zero(t, stripeWebhookReceiptCount(t, db))
	client.verify = completion
	client.On("EndPartnerCheckoutTrial", mock.Anything, "subscription_partner", "partner-trial-end:"+client.checkout.ID).Run(func(mock.Arguments) {
		client.checkout.SubscriptionStatus = "active"
		client.checkout.BillingCycleAnchor = anchor
		client.checkout.Partner.TrialEnd = anchor
	}).Return(nil).Once()
	require.Equal(t, http.StatusOK, serveStripeWebhook(s, "recovered").Code)
	client.verify = invoiceEvent
	require.Equal(t, http.StatusOK, serveStripeWebhook(s, "invoice after activation").Code)
	require.Len(t, stripeInvoices(t, db), 1)
}

func TestManualPartnerExistingSubscriptionCannotAcquireAttribution(t *testing.T) {
	t.Parallel()
	s, db, _, _ := newManualPartnerCheckout(t, true)
	require.NoError(t, repo.New(db).SetStripeSubscriptionFixture(t.Context(), repo.SetStripeSubscriptionFixtureParams{OrganizationID: stripeWebhookOrganizationID, StripeSubscriptionID: pgtype.Text{String: "sub_existing", Valid: true}}))
	require.Equal(t, http.StatusServiceUnavailable, serveStripeWebhook(s, "existing subscriber").Code)
	_, err := orgrepo.New(db).GetOrganizationPromotionApplication(t.Context(), stripeWebhookOrganizationID)
	require.Error(t, err)
}

func TestManualPartnerCheckoutRecoversLostTrialEndResponseAfterOfferDisabled(t *testing.T) {
	t.Parallel()
	s, db, client, offer := newManualPartnerCheckout(t, true)
	anchor := time.Date(2028, time.January, 31, 13, 17, 0, 0, time.UTC)
	client.On("EndPartnerCheckoutTrial", mock.Anything, "subscription_partner", "partner-trial-end:"+client.checkout.ID).Run(func(mock.Arguments) {
		client.checkout.SubscriptionStatus = "active"
		client.checkout.BillingCycleAnchor = anchor
		client.checkout.Partner.TrialEnd = anchor
	}).Return(errors.New("response interrupted")).Once()
	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(s, "first").Code)
	application, err := orgrepo.New(db).GetOrganizationPromotionApplication(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.False(t, application.RedeemedAt.Valid)
	require.Zero(t, stripeWebhookReceiptCount(t, db))
	_, err = adminrepo.New(db).UpdateSignupOffer(t.Context(), adminrepo.UpdateSignupOfferParams{ID: offer.ID, Name: offer.Name, StripePromotionCodeID: offer.StripePromotionCodeID, SignupFlow: offer.SignupFlow, Enabled: false})
	require.NoError(t, err)
	client.scopeError = errors.New("coupon scope unavailable after acceptance")
	require.Equal(t, http.StatusOK, serveStripeWebhook(s, "retry").Code)
	require.Equal(t, http.StatusOK, serveStripeWebhook(s, "duplicate").Code)
	application, err = orgrepo.New(db).GetOrganizationPromotionApplication(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.True(t, application.RedeemedAt.Valid)
	require.Equal(t, "subscription_partner", application.StripeSubscriptionID.String)
	metadata, err := repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.False(t, metadata.StripeCheckoutSessionID.Valid)
	require.True(t, metadata.StripeBillingCycleAnchor.Time.Equal(anchor))
	enabled, err := featurerepo.New(db).IsFeatureEnabled(t.Context(), featurerepo.IsFeatureEnabledParams{OrganizationID: stripeWebhookOrganizationID, FeatureName: "network_ingress"})
	require.NoError(t, err)
	require.True(t, enabled)
}

func TestManualPartnerDisabledOfferLeavesOrdinaryStripeTrial(t *testing.T) {
	t.Parallel()
	s, db, client, _ := newManualPartnerCheckout(t, false)
	require.Equal(t, http.StatusOK, serveStripeWebhook(s, "ordinary").Code)
	require.Equal(t, "trialing", client.checkout.SubscriptionStatus)
	_, err := orgrepo.New(db).GetOrganizationPromotionApplication(t.Context(), stripeWebhookOrganizationID)
	require.Error(t, err)
	client.AssertNotCalled(t, "EndPartnerCheckoutTrial", mock.Anything, mock.Anything, mock.Anything)
}

func TestManualPartnerUnsupportedScopeLeavesOrdinaryStripeTrial(t *testing.T) {
	t.Parallel()
	s, db, client, _ := newManualPartnerCheckout(t, true)
	client.scopeEligible = false
	require.Equal(t, http.StatusOK, serveStripeWebhook(s, "ordinary discount").Code)
	require.Equal(t, "trialing", client.checkout.SubscriptionStatus)
	_, err := orgrepo.New(db).GetOrganizationPromotionApplication(t.Context(), stripeWebhookOrganizationID)
	require.Error(t, err)
	client.AssertNotCalled(t, "EndPartnerCheckoutTrial", mock.Anything, mock.Anything, mock.Anything)
}

func TestManualPartnerNoCodeLeavesOrdinaryTrial(t *testing.T) {
	t.Parallel()
	s, db, client, _ := newManualPartnerCheckout(t, true)
	client.checkout.Partner.PromotionCodeID = ""
	require.Equal(t, http.StatusOK, serveStripeWebhook(s, "no code").Code)
	require.Equal(t, "trialing", client.checkout.SubscriptionStatus)
	_, err := orgrepo.New(db).GetOrganizationPromotionApplication(t.Context(), stripeWebhookOrganizationID)
	require.Error(t, err)
}

func TestManualPartnerUnmappedCodeLeavesOrdinaryTrial(t *testing.T) {
	t.Parallel()
	s, db, client, _ := newManualPartnerCheckout(t, true)
	client.checkout.Partner.PromotionCodeID = "promo_unmapped"
	require.Equal(t, http.StatusOK, serveStripeWebhook(s, "unmapped").Code)
	require.Equal(t, "trialing", client.checkout.SubscriptionStatus)
	_, err := orgrepo.New(db).GetOrganizationPromotionApplication(t.Context(), stripeWebhookOrganizationID)
	require.Error(t, err)
}

func TestManualPartnerRequiresCollectedPaymentMethod(t *testing.T) {
	t.Parallel()
	s, db, client, _ := newManualPartnerCheckout(t, true)
	client.checkout.Partner.PaymentMethodID = ""
	require.Equal(t, http.StatusServiceUnavailable, serveStripeWebhook(s, "missing method").Code)
	_, err := orgrepo.New(db).GetOrganizationPromotionApplication(t.Context(), stripeWebhookOrganizationID)
	require.Error(t, err)
}

func TestManualPartnerPaidActivationPreservesStaffDisable(t *testing.T) {
	t.Parallel()
	s, db, client, _ := newManualPartnerCheckout(t, true)
	client.checkout.SubscriptionStatus = "active"
	q := featurerepo.New(db)
	_, err := q.EnableFeature(t.Context(), featurerepo.EnableFeatureParams{OrganizationID: stripeWebhookOrganizationID, FeatureName: "network_ingress"})
	require.NoError(t, err)
	_, err = q.DeleteFeature(t.Context(), featurerepo.DeleteFeatureParams{OrganizationID: stripeWebhookOrganizationID, FeatureName: "network_ingress"})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, serveStripeWebhook(s, "active paid").Code)
	app, err := orgrepo.New(db).GetOrganizationPromotionApplication(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.True(t, app.RedeemedAt.Valid)
	enabled, err := q.IsFeatureEnabled(t.Context(), featurerepo.IsFeatureEnabledParams{OrganizationID: stripeWebhookOrganizationID, FeatureName: "network_ingress"})
	require.NoError(t, err)
	require.False(t, enabled)
}

func TestManualPartnerCancellationRetainsPendingBindingWithoutRedemption(t *testing.T) {
	t.Parallel()
	s, db, client, _ := newManualPartnerCheckout(t, true)
	client.On("EndPartnerCheckoutTrial", mock.Anything, "subscription_partner", "partner-trial-end:"+client.checkout.ID).Return(errors.New("Stripe unavailable")).Once()
	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(s, "first").Code)
	client.checkout.SubscriptionStatus = "canceled"
	client.verify = func([]byte, string) (*stripeclient.WebhookEvent, error) {
		return &stripeclient.WebhookEvent{ID: "event_partner_deleted", Type: "customer.subscription.deleted", Created: time.Now().UTC(), ObjectID: "subscription_partner", CustomerID: "customer_placeholder", SubscriptionID: "subscription_partner"}, nil
	}
	require.Equal(t, http.StatusOK, serveStripeWebhook(s, "canceled").Code)
	app, err := orgrepo.New(db).GetOrganizationPromotionApplication(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.False(t, app.RedeemedAt.Valid)
	metadata, err := repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, client.checkout.ID, metadata.StripeCheckoutSessionID.String)
	now := time.Now().UTC()
	_, err = s.prepareStripeCheckoutIntent(t.Context(), stripeWebhookOrganizationID, "customer_placeholder", now, newStripeCheckoutIntent(stripeWebhookOrganizationID, now, nil), pgtype.Text{}, pgtype.Text{})
	require.ErrorContains(t, err, "still being reconciled")
}

func TestManualPartnerConcurrentCompletionRedeemsOnlyOnce(t *testing.T) {
	t.Parallel()
	s, db, client, _ := newManualPartnerCheckout(t, true)
	client.checkout.SubscriptionStatus = "active"
	results := make(chan int, 2)
	for range 2 {
		go func() { results <- serveStripeWebhook(s, "concurrent completion").Code }()
	}
	require.Equal(t, http.StatusOK, <-results)
	require.Equal(t, http.StatusOK, <-results)
	redemptions, err := repo.New(db).ListOrganizationPromotionRedemptions(t.Context(), pgtype.Text{String: stripeWebhookOrganizationID, Valid: true})
	require.NoError(t, err)
	require.Len(t, redemptions, 1)
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
}

func TestManualPartnerCheckoutRejectsUnboundSession(t *testing.T) {
	t.Parallel()
	s, db, client, _ := newManualPartnerCheckout(t, true)
	require.NoError(t, repo.New(db).SetStripeCheckoutSessionFixture(t.Context(), repo.SetStripeCheckoutSessionFixtureParams{OrganizationID: stripeWebhookOrganizationID, StripeCheckoutSessionID: pgtype.Text{String: "other_session", Valid: true}}))
	require.Equal(t, http.StatusServiceUnavailable, serveStripeWebhook(s, "mismatch").Code)
	_, err := orgrepo.New(db).GetOrganizationPromotionApplication(t.Context(), stripeWebhookOrganizationID)
	require.Error(t, err)
	client.AssertNotCalled(t, "EndPartnerCheckoutTrial", mock.Anything, mock.Anything, mock.Anything)
}
