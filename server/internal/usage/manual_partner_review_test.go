package usage

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
	"github.com/stretchr/testify/require"
)

func TestFirstManualPartnerAfterUnpaidOrdinaryCheckout(t *testing.T) {
	t.Parallel()
	testFirstManualPartnerAfterTerminalCheckout(t, "unpaid")
}
func TestFirstManualPartnerAfterExpiredOrdinaryCheckout(t *testing.T) {
	t.Parallel()
	testFirstManualPartnerAfterTerminalCheckout(t, "incomplete_expired")
}
func TestFirstManualPartnerAfterCanceledOrdinaryCheckout(t *testing.T) {
	t.Parallel()
	testFirstManualPartnerAfterTerminalCheckout(t, "canceled")
}

func testFirstManualPartnerAfterTerminalCheckout(t *testing.T, status string) {
	t.Helper()
	s, db, client, _ := newManualPartnerCheckout(t, true)
	original := *client.checkout
	originalVerify := client.verify
	failed := original
	failed.ID = "checkout_failed"
	failed.SubscriptionID = "subscription_failed"
	failed.SubscriptionStatus = status
	failed.Partner = nil
	client.checkout = &failed
	client.verify = func([]byte, string) (*stripeclient.WebhookEvent, error) {
		return &stripeclient.WebhookEvent{ID: "event_failed_ordinary", Type: "checkout.session.completed", Created: time.Now().UTC(), ObjectID: failed.ID, CustomerID: failed.CustomerID, SubscriptionID: failed.SubscriptionID}, nil
	}
	require.Equal(t, http.StatusOK, serveStripeWebhook(s, "failed ordinary").Code)
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
	original.SubscriptionStatus = "active"
	client.checkout = &original
	client.verify = originalVerify
	require.Equal(t, http.StatusOK, serveStripeWebhook(s, "first partner").Code)
	app, err := orgrepo.New(db).GetOrganizationPromotionApplication(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.True(t, app.RedeemedAt.Valid)
}

func TestPreviouslyActivatedSubscriberRemainsExcludedAfterCancellation(t *testing.T) {
	t.Parallel()
	s, _, client, _ := newManualPartnerCheckout(t, true)
	partner := client.checkout.Partner
	client.checkout.Partner = nil
	client.checkout.SubscriptionStatus = "active"
	completion := client.verify
	require.Equal(t, http.StatusOK, serveStripeWebhook(s, "ordinary activation").Code)
	client.verify = func([]byte, string) (*stripeclient.WebhookEvent, error) {
		return &stripeclient.WebhookEvent{ID: "event_old_subscription_deleted", Type: "customer.subscription.deleted", Created: time.Now().UTC(), ObjectID: "subscription_partner", CustomerID: "customer_placeholder", SubscriptionID: "subscription_partner"}, nil
	}
	require.Equal(t, http.StatusOK, serveStripeWebhook(s, "ordinary cancellation").Code)
	client.checkout.Partner = partner
	event, err := completion(nil, "")
	require.NoError(t, err)
	event.ID = "event_new_partner"
	require.ErrorContains(t, s.acceptManualPartnerCheckout(t.Context(), event, client.checkout), "existing subscriber")
}

func TestUnmappedDiscountDoesNotRequireCouponScopeLookup(t *testing.T) {
	t.Parallel()
	s, db, client, _ := newManualPartnerCheckout(t, true)
	client.scopeError = errors.New("coupon scope API unavailable")
	client.checkout.Partner.PromotionCodeID = "promo_unmapped"
	require.Equal(t, http.StatusOK, serveStripeWebhook(s, "ordinary").Code)
	require.Equal(t, "trialing", client.checkout.SubscriptionStatus)
	_, err := orgrepo.New(db).GetOrganizationPromotionApplication(t.Context(), stripeWebhookOrganizationID)
	require.Error(t, err)
}

func TestQualifyingPartnerScopeFailureCannotAcceptOrActivate(t *testing.T) {
	t.Parallel()
	s, db, client, _ := newManualPartnerCheckout(t, true)
	client.scopeError = errors.New("coupon scope API unavailable")
	require.Equal(t, http.StatusServiceUnavailable, serveStripeWebhook(s, "scope unavailable").Code)
	_, err := orgrepo.New(db).GetOrganizationPromotionApplication(t.Context(), stripeWebhookOrganizationID)
	require.Error(t, err)
	require.Zero(t, stripeWebhookReceiptCount(t, db))
}

// A remote-read hook acquiring the same lock would time out if preparation still
// retained its transaction/organization lock during the Stripe request.
func TestPartnerCheckoutRemoteReadDoesNotHoldOrganizationLock(t *testing.T) {
	t.Parallel()
	ti := newStripeCheckoutTestInstance(t)
	attributePartnerSignup(t, ti.db, ti.orgID)
	ctx := ti.adminContext(t)
	_, err := ti.service.CreateStripeCheckout(ctx, nil)
	require.NoError(t, err)
	ti.stripe.beforeCheckoutRead = func() error {
		lockCtx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		// Autocommit releases the transaction-scoped advisory lock immediately.
		return repo.New(ti.service.db).LockBillingMetadataOrganization(lockCtx, ti.orgID)
	}
	_, err = ti.service.CreateStripeCheckout(ctx, nil)
	require.NoError(t, err)
}

func TestPartnerCheckoutRejectsChangedBindingAfterRemoteRead(t *testing.T) {
	t.Parallel()
	ti := newStripeCheckoutTestInstance(t)
	attributePartnerSignup(t, ti.db, ti.orgID)
	ctx := ti.adminContext(t)
	_, err := ti.service.CreateStripeCheckout(ctx, nil)
	require.NoError(t, err)
	ti.stripe.beforeCheckoutRead = func() error {
		return repo.New(ti.db).SetStripeCheckoutSessionFixture(t.Context(), repo.SetStripeCheckoutSessionFixtureParams{OrganizationID: ti.orgID, StripeCheckoutSessionID: pgtype.Text{String: "checkout_changed", Valid: true}})
	}
	_, err = ti.service.CreateStripeCheckout(ctx, nil)
	require.ErrorContains(t, err, "binding changed")
}

func TestDisabledOfferDoesNotRequireCouponScopeLookup(t *testing.T) {
	t.Parallel()
	s, db, client, _ := newManualPartnerCheckout(t, false)
	client.scopeError = errors.New("coupon scope API unavailable")
	require.Equal(t, http.StatusOK, serveStripeWebhook(s, "ordinary disabled offer").Code)
	require.Equal(t, "trialing", client.checkout.SubscriptionStatus)
	_, err := orgrepo.New(db).GetOrganizationPromotionApplication(t.Context(), stripeWebhookOrganizationID)
	require.Error(t, err)
}
