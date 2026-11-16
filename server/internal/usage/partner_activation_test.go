package usage

import (
	"net/http"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
	"github.com/stretchr/testify/require"
)

func TestStripeCheckoutActivatesPartnerPrivateNetworking(t *testing.T) {
	t.Parallel()
	testPartnerPrivateNetworkingActivation(t, true, false, true)
}

func TestStripeCheckoutActivatesPartnerPrivateNetworkingSkipsOrdinarySignup(t *testing.T) {
	t.Parallel()
	testPartnerPrivateNetworkingActivation(t, false, false, false)
}

func TestStripeCheckoutActivatesPartnerPrivateNetworkingRespectsStaffDisabled(t *testing.T) {
	t.Parallel()
	testPartnerPrivateNetworkingActivation(t, true, true, false)
}

func testPartnerPrivateNetworkingActivation(t *testing.T, partner, staffDisabled, wantEnabled bool) {
	t.Helper()
	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_partner_activation", "subscription_partner", "active")
	if partner {
		attributePartnerSignup(t, db, stripeWebhookOrganizationID)
	}
	if staffDisabled {
		q := featurerepo.New(db)
		_, err := q.EnableFeature(t.Context(), featurerepo.EnableFeatureParams{OrganizationID: stripeWebhookOrganizationID, FeatureName: string(productfeatures.FeatureNetworkIngress)})
		require.NoError(t, err)
		_, err = q.DeleteFeature(t.Context(), featurerepo.DeleteFeatureParams{OrganizationID: stripeWebhookOrganizationID, FeatureName: string(productfeatures.FeatureNetworkIngress)})
		require.NoError(t, err)
	}
	redisClient := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })
	features := productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), db, redisClient)
	service.productFeatures = features
	enabled, err := features.IsFeatureEnabled(t.Context(), stripeWebhookOrganizationID, productfeatures.FeatureNetworkIngress)
	require.NoError(t, err)
	require.False(t, enabled, "attribution alone must not grant private networking before checkout")
	assertNetworkingDisabled := func() {
		t.Helper()
		enabled, err := features.IsFeatureEnabled(t.Context(), stripeWebhookOrganizationID, productfeatures.FeatureNetworkIngress)
		require.NoError(t, err)
		require.False(t, enabled, "rejected checkout must not grant private networking")
		count, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionOrganizationProductFeatureEnabled)
		require.NoError(t, err)
		require.Zero(t, count, "rejected checkout must not emit a feature enable audit")
	}
	if partner {
		preparedAt := time.Now().UTC()
		prepared, err := service.prepareStripeCheckoutIntent(t.Context(), stripeWebhookOrganizationID, "customer_placeholder", preparedAt, newStripeCheckoutIntent(stripeWebhookOrganizationID, preparedAt, nil), pgtype.Text{}, pgtype.Text{})
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "before finalization").Code)
		assertNetworkingDisabled()
		pending, err := repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
		require.NoError(t, err)
		require.False(t, pending.StripeSubscriptionID.Valid)
		require.Equal(t, prepared.idempotencyKey, pending.StripeCheckoutIdempotencyKey.String)
		require.False(t, pending.StripeCheckoutSessionID.Valid)
		redemptions, err := repo.New(db).ListOrganizationPromotionRedemptions(t.Context(), pgtype.Text{String: stripeWebhookOrganizationID, Valid: true})
		require.NoError(t, err)
		require.Empty(t, redemptions)
		application, err := orgrepo.New(db).GetOrganizationPromotionApplication(t.Context(), stripeWebhookOrganizationID)
		require.NoError(t, err)
		require.Equal(t, "promo_partner", application.StripePromotionCodeID)
		require.False(t, application.RedeemedAt.Valid)
		require.False(t, application.StripeSubscriptionID.Valid)
		// An unrelated persisted session must fail closed too.
		require.NoError(t, repo.New(db).SetStripeCheckoutSessionFixture(t.Context(), repo.SetStripeCheckoutSessionFixtureParams{OrganizationID: stripeWebhookOrganizationID, StripeCheckoutSessionID: pgtype.Text{String: "unrelated_checkout", Valid: true}}))
		require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "unmatched session").Code)
		assertNetworkingDisabled()
		pending, err = repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
		require.NoError(t, err)
		require.False(t, pending.StripeSubscriptionID.Valid)
		require.Equal(t, prepared.idempotencyKey, pending.StripeCheckoutIdempotencyKey.String)
		require.Equal(t, "unrelated_checkout", pending.StripeCheckoutSessionID.String)
		organization, err := orgrepo.New(db).GetOrganizationMetadata(t.Context(), stripeWebhookOrganizationID)
		require.NoError(t, err)
		require.NotEqual(t, "payg", organization.GramAccountType)

		stripe, ok := service.stripeClient.(*fakeStripeWebhookClient)
		require.True(t, ok)
		checkout := stripe.checkout
		// Clear the deliberately mismatched fixture before real finalization.
		require.NoError(t, repo.New(db).SetStripeCheckoutSessionFixture(t.Context(), repo.SetStripeCheckoutSessionFixtureParams{OrganizationID: stripeWebhookOrganizationID, StripeCheckoutSessionID: pgtype.Text{}}))
		_, err = repo.New(db).FinalizeStripeCheckoutIntent(t.Context(), repo.FinalizeStripeCheckoutIntentParams{
			OrganizationID: stripeWebhookOrganizationID, StripeCustomerID: "customer_placeholder", StripeCheckoutSessionID: checkout.ID,
			StripeCheckoutIdempotencyKey: prepared.idempotencyKey, StripeCheckoutBillingCycleAnchor: finiteTimestamptz(prepared.billingCycleAnchor),
			StripeCheckoutTrialEnd: optionalTimestamptz(prepared.trialEnd), StripeCheckoutExpiresAt: finiteTimestamptz(prepared.expiresAt),
		})
		require.NoError(t, err)
		// A matching session still cannot grant networking before payment succeeds.
		checkout.SubscriptionStatus = "incomplete"
		require.Equal(t, http.StatusServiceUnavailable, serveStripeWebhook(service, "payment incomplete").Code)
		assertNetworkingDisabled()
		checkout.SubscriptionStatus = "active"
	}
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "activation").Code)
	redemptions, err := repo.New(db).ListOrganizationPromotionRedemptions(t.Context(), pgtype.Text{String: stripeWebhookOrganizationID, Valid: true})
	require.NoError(t, err)
	if partner {
		require.Len(t, redemptions, 1)
	} else {
		require.Empty(t, redemptions)
	}

	enabled, err = features.IsFeatureEnabled(t.Context(), stripeWebhookOrganizationID, productfeatures.FeatureNetworkIngress)
	require.NoError(t, err)
	require.Equal(t, wantEnabled, enabled)
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "duplicate activation").Code)
	enabled, err = features.IsFeatureEnabled(t.Context(), stripeWebhookOrganizationID, productfeatures.FeatureNetworkIngress)
	require.NoError(t, err)
	require.Equal(t, wantEnabled, enabled)

	enableCount, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionOrganizationProductFeatureEnabled)
	require.NoError(t, err)
	if wantEnabled {
		require.Equal(t, int64(1), enableCount, "duplicate checkout must not emit another feature enable audit")
		record, err := audittest.LatestAuditLogByAction(t.Context(), db, audit.ActionOrganizationProductFeatureEnabled)
		require.NoError(t, err)
		require.Equal(t, stripeWebhookOrganizationID, record.OrganizationID)
		metadata, err := audittest.DecodeAuditData(record.Metadata)
		require.NoError(t, err)
		require.Equal(t, "network_ingress", metadata["feature_name"])
	} else {
		require.Zero(t, enableCount)
	}
}
