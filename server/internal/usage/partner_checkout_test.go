package usage

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

func TestPartnerPromotionCodeSurvivesCheckoutAPIOutage(t *testing.T) {
	t.Parallel()
	ti := newStripeCheckoutTestInstance(t)
	attributePartnerSignup(t, ti.db, ti.orgID)
	ti.stripe.checkoutError = errors.New("Stripe temporarily unavailable")
	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	ti.stripe.checkoutError = nil
	checkoutURL, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	require.NotEmpty(t, checkoutURL)
	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 2)
	for _, checkout := range checkouts {
		require.Equal(t, "promo_partner", checkout.PromotionCodeID)
		require.Nil(t, checkout.TrialEnd)
	}
	require.Equal(t, checkouts[0], checkouts[1], "retry keeps the promotion and original idempotency parameters")
}

func TestPartnerPromotionCodeSurvivesCheckoutLifecycleRecovery(t *testing.T) {
	t.Parallel()
	ti := newStripeCheckoutTestInstance(t)
	attributePartnerSignup(t, ti.db, ti.orgID)
	require.NoError(t, trialsrepo.New(ti.db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{
		OrganizationID: ti.orgID, Tier: "enterprise",
		EndsAt: pgtype.Timestamptz{Time: time.Now().Add(7 * 24 * time.Hour), Valid: true},
	}))
	ti.stripe.afterCheckoutCreate = func() {
		ti.stripe.afterCheckoutCreate = nil
		_, err := trialsrepo.New(ti.db).ExtendTrial(t.Context(), trialsrepo.ExtendTrialParams{OrganizationID: ti.orgID, ExtendByDays: 1})
		require.NoError(t, err)
	}
	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	checkoutURL, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	require.Equal(t, "cs_test_secret_2", checkoutURL.ClientSecret)
	require.Equal(t, []string{"cs_1"}, ti.stripe.expiredCheckoutIDs)
	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 3)
	for _, checkout := range checkouts {
		require.Equal(t, "promo_partner", checkout.PromotionCodeID)
		require.Nil(t, checkout.TrialEnd)
	}
	require.Equal(t, checkouts[0], checkouts[1], "recover the old session with exactly its original promotion code")
	require.NotEqual(t, checkouts[0].IdempotencyKey, checkouts[2].IdempotencyKey)
}

func TestCreateStripeCheckoutAppliesPartnerPromotionCodeWithoutTrial(t *testing.T) {
	t.Parallel()
	testPartnerPromotionCodeCheckout(t, false)
}

func TestCreateStripeCheckoutAppliesPartnerPromotionCodeWithSubsequentlyAssignedTrial(t *testing.T) {
	t.Parallel()
	testPartnerPromotionCodeCheckout(t, true)
}

func testPartnerPromotionCodeCheckout(t *testing.T, withTrial bool) {
	t.Helper()
	ti := newStripeCheckoutTestInstance(t)
	attributePartnerSignup(t, ti.db, ti.orgID)
	if withTrial {
		require.NoError(t, trialsrepo.New(ti.db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{
			OrganizationID: ti.orgID,
			Tier:           "enterprise",
			EndsAt:         pgtype.Timestamptz{Time: time.Now().Add(7 * 24 * time.Hour), Valid: true},
		}))
	}
	firstURL, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	secondURL, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	require.Equal(t, firstURL, secondURL)
	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 2)
	for _, checkout := range checkouts {
		require.Equal(t, "promo_partner", checkout.PromotionCodeID)
		require.Nil(t, checkout.TrialEnd)
	}
	require.Equal(t, checkouts[0], checkouts[1])
}

func attributePartnerSignup(t *testing.T, db orgrepo.DBTX, orgID string) {
	t.Helper()
	rows, err := orgrepo.New(db).AttributePartnerSignup(t.Context(), orgrepo.AttributePartnerSignupParams{
		OrganizationID:        pgtype.Text{String: orgID, Valid: true},
		StripePromotionCodeID: "promo_partner",
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
}

func TestPartnerFailedInvoiceDoesNotActivateAndCheckoutRetryKeepsPromotion(t *testing.T) {
	t.Parallel()
	testPartnerPaymentFailureCheckoutRetry(t, "invoice.payment_failed", "incomplete", "unpaid", http.StatusOK)
}

func TestPartnerUnpaidCompletionDoesNotActivateAndCheckoutRetryKeepsPromotion(t *testing.T) {
	t.Parallel()
	testPartnerPaymentFailureCheckoutRetry(t, "checkout.session.completed", "unpaid", "unpaid", http.StatusOK)
}

func TestPartnerIncompleteSubscriptionDoesNotActivateAndCheckoutRetryKeepsPromotion(t *testing.T) {
	t.Parallel()
	testPartnerPaymentFailureCheckoutRetry(t, "checkout.session.completed", "incomplete", "paid", http.StatusServiceUnavailable)
}

func testPartnerPaymentFailureCheckoutRetry(t *testing.T, eventType, status, paymentStatus string, wantStatus int) {
	t.Helper()
	ti := newStripeCheckoutTestInstance(t)
	attributePartnerSignup(t, ti.db, ti.orgID)
	firstURL, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	ti.service.stripeClient = &fakeStripeWebhookClient{
		verify: func([]byte, string) (*stripeclient.WebhookEvent, error) {
			return &stripeclient.WebhookEvent{ID: "evt_payment_failure", Type: eventType, Created: time.Now().UTC(), ObjectID: "cs_1", CustomerID: "cus_1", SubscriptionID: "sub_unpaid"}, nil
		},
		checkout: &stripeclient.CheckoutSessionState{ID: "cs_1", Status: "complete", PaymentStatus: paymentStatus, CustomerID: "cus_1", SubscriptionID: "sub_unpaid", SubscriptionCustomerID: "cus_1", SubscriptionStatus: status, BillingCycleAnchor: time.Date(2026, time.August, 23, 9, 0, 0, 0, time.UTC)},
	}
	ti.service.stripeHandler = ti.service.serviceStripeWebhookHandler
	require.Equal(t, wantStatus, serveStripeWebhook(ti.service, "payment failure").Code)
	organization, err := orgrepo.New(ti.db).GetOrganizationMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.NotEqual(t, "payg", organization.GramAccountType)
	metadata, err := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.False(t, metadata.StripeSubscriptionID.Valid)
	redemptions, err := repo.New(ti.db).ListOrganizationPromotionRedemptions(t.Context(), pgtype.Text{String: ti.orgID, Valid: true})
	require.NoError(t, err)
	require.Empty(t, redemptions)
	ti.service.stripeClient = ti.stripe
	retryURL, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	require.Equal(t, firstURL, retryURL)
	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 2)
	require.Equal(t, "promo_partner", checkouts[1].PromotionCodeID)
	require.Equal(t, checkouts[0], checkouts[1])
}

func TestPartnerCompletedCheckoutWaitsForWebhookWithoutSecondRedemption(t *testing.T) {
	t.Parallel()
	ti := newStripeCheckoutTestInstance(t)
	attributePartnerSignup(t, ti.db, ti.orgID)
	oldIntent := seedExpiredCheckoutSession(t, ti, "cs_completed")
	ti.stripe.checkoutState = &stripeclient.CheckoutSessionState{ID: "cs_completed", Status: "complete", CustomerID: "cus_test", SubscriptionID: "sub_delayed_webhook"}
	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
	_, _, checkouts := ti.stripe.snapshot()
	require.Empty(t, checkouts)
	stored, err := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, oldIntent.idempotencyKey, stored.StripeCheckoutIdempotencyKey.String)
	require.Equal(t, "cs_completed", stored.StripeCheckoutSessionID.String)
}

func TestPartnerActivatedOrganizationCannotStartAnotherDiscountedCheckout(t *testing.T) {
	t.Parallel()
	ti := newStripeCheckoutTestInstance(t)
	attributePartnerSignup(t, ti.db, ti.orgID)
	require.NoError(t, repo.New(ti.db).CreateStripeSubscriptionBillingMetadataFixture(t.Context(), repo.CreateStripeSubscriptionBillingMetadataFixtureParams{
		OrganizationID: ti.orgID, StripeCustomerID: pgtype.Text{String: "cus_test", Valid: true}, StripeSubscriptionID: pgtype.Text{String: "sub_existing", Valid: true},
	}))
	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
	_, _, checkouts := ti.stripe.snapshot()
	require.Empty(t, checkouts)
}

func TestPartnerAbandonedExpiredCheckoutCanRetryStoredPromotion(t *testing.T) {
	t.Parallel()
	ti := newStripeCheckoutTestInstance(t)
	attributePartnerSignup(t, ti.db, ti.orgID)
	oldIntent := seedExpiredCheckoutSession(t, ti, "cs_abandoned")
	ti.stripe.checkoutState = &stripeclient.CheckoutSessionState{ID: "cs_abandoned", Status: "expired", CustomerID: "cus_test"}
	checkoutURL, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	require.NotEmpty(t, checkoutURL)
	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 1)
	require.Equal(t, "promo_partner", checkouts[0].PromotionCodeID)
	require.NotEqual(t, oldIntent.idempotencyKey, checkouts[0].IdempotencyKey)
}

func TestPartnerCancellationCannotRedeemPromotionAgain(t *testing.T) {
	t.Parallel()
	ti := newStripeCheckoutTestInstance(t)
	attributePartnerSignup(t, ti.db, ti.orgID)
	require.NoError(t, authz.SeedSystemRoleGrants(t.Context(), ti.service.db, ti.orgID))
	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	event := &stripeclient.WebhookEvent{ID: "evt_partner_paid", Type: "checkout.session.completed", Created: time.Now().UTC(), ObjectID: "cs_1", CustomerID: "cus_1", SubscriptionID: "sub_partner"}
	ti.service.stripeClient = &fakeStripeWebhookClient{
		verify:   func([]byte, string) (*stripeclient.WebhookEvent, error) { return event, nil },
		checkout: &stripeclient.CheckoutSessionState{ID: "cs_1", Status: "complete", PaymentStatus: "paid", CustomerID: "cus_1", SubscriptionID: "sub_partner", SubscriptionCustomerID: "cus_1", SubscriptionStatus: "active", BillingCycleAnchor: time.Date(2026, time.August, 23, 9, 0, 0, 0, time.UTC)},
	}
	ti.service.stripeHandler = ti.service.serviceStripeWebhookHandler
	logger := ti.service.auditLogger
	ti.service.auditLogger = nil
	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(ti.service, "audit failure").Code)
	redemptions, err := repo.New(ti.db).ListOrganizationPromotionRedemptions(t.Context(), pgtype.Text{String: ti.orgID, Valid: true})
	require.NoError(t, err)
	require.Empty(t, redemptions, "audit failure rolls back redemption")
	pendingApplication, err := orgrepo.New(ti.db).GetOrganizationPromotionApplication(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "promo_partner", pendingApplication.StripePromotionCodeID)
	require.False(t, pendingApplication.RedeemedAt.Valid)
	require.False(t, pendingApplication.StripeSubscriptionID.Valid)
	metadata, err := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.False(t, metadata.StripeSubscriptionID.Valid)
	require.Equal(t, "cs_1", metadata.StripeCheckoutSessionID.String)
	ti.service.auditLogger = logger
	require.Equal(t, http.StatusOK, serveStripeWebhook(ti.service, "paid").Code)
	require.Equal(t, http.StatusOK, serveStripeWebhook(ti.service, "duplicate").Code)
	event.ID = "evt_partner_paid_duplicate"
	require.Equal(t, http.StatusOK, serveStripeWebhook(ti.service, "new event same subscription").Code)
	redemptions, err = repo.New(ti.db).ListOrganizationPromotionRedemptions(t.Context(), pgtype.Text{String: ti.orgID, Valid: true})
	require.NoError(t, err)
	require.Len(t, redemptions, 1)
	require.Equal(t, pendingApplication.ID, redemptions[0].ID, "activation transitions the original pending application")
	require.Equal(t, "promo_partner", redemptions[0].StripePromotionCodeID)
	require.Equal(t, "sub_partner", redemptions[0].StripeSubscriptionID.String)
	auditCount, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionOrganizationPromotionRedeemed)
	require.NoError(t, err)
	require.EqualValues(t, 1, auditCount)
	record, err := audittest.LatestAuditLogByAction(t.Context(), ti.db, audit.ActionOrganizationPromotionRedeemed)
	require.NoError(t, err)
	snapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "promo_partner", snapshot["stripe_promotion_code_id"])
	require.Equal(t, "sub_partner", snapshot["stripe_subscription_id"])

	event = &stripeclient.WebhookEvent{ID: "evt_partner_cancel", Type: "customer.subscription.deleted", Created: time.Now().UTC(), ObjectID: "sub_partner", CustomerID: "cus_1"}
	require.Equal(t, http.StatusOK, serveStripeWebhook(ti.service, "cancel").Code)
	ti.service.stripeClient = ti.stripe
	_, err = ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
	require.Contains(t, err.Error(), "already been redeemed")
	// Bypassing the preflight still cannot reserve an intent under the billing lock.
	_, err = ti.service.prepareStripeCheckoutIntent(t.Context(), ti.orgID, "cus_1", time.Now(), newStripeCheckoutIntent(ti.orgID, time.Now(), nil), pgtype.Text{}, pgtype.Text{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)

	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 1)
	// Even a concurrently prepared trusted session cannot redeem the same code
	// for a different subscription after the first subscription was canceled.
	_, err = repo.New(ti.db).PrepareStripeCheckoutIntent(t.Context(), repo.PrepareStripeCheckoutIntentParams{
		OrganizationID: ti.orgID, StripeCustomerID: "cus_1", StripeCheckoutIdempotencyKey: "second_intent",
		PreparedAt: finiteTimestamptz(time.Now()), StripeCheckoutBillingCycleAnchor: finiteTimestamptz(time.Now()), StripeCheckoutExpiresAt: finiteTimestamptz(time.Now().Add(time.Hour)),
	})
	require.NoError(t, err)
	require.NoError(t, repo.New(ti.db).SetStripeCheckoutSessionFixture(t.Context(), repo.SetStripeCheckoutSessionFixtureParams{OrganizationID: ti.orgID, StripeCheckoutSessionID: pgtype.Text{String: "cs_second", Valid: true}}))
	event = &stripeclient.WebhookEvent{ID: "evt_partner_second", Type: "checkout.session.completed", Created: time.Now().UTC(), ObjectID: "cs_second", CustomerID: "cus_1", SubscriptionID: "sub_second"}
	ti.service.stripeClient = &fakeStripeWebhookClient{
		verify:   func([]byte, string) (*stripeclient.WebhookEvent, error) { return event, nil },
		checkout: &stripeclient.CheckoutSessionState{ID: "cs_second", Status: "complete", PaymentStatus: "paid", CustomerID: "cus_1", SubscriptionID: "sub_second", SubscriptionCustomerID: "cus_1", SubscriptionStatus: "active", BillingCycleAnchor: time.Now().UTC()},
	}
	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(ti.service, "second subscription").Code)
	redemptions, err = repo.New(ti.db).ListOrganizationPromotionRedemptions(t.Context(), pgtype.Text{String: ti.orgID, Valid: true})
	require.NoError(t, err)
	require.Len(t, redemptions, 1)
	require.Equal(t, "sub_partner", redemptions[0].StripeSubscriptionID.String)
}

func TestPartnerRedemptionHistoryForAnotherPromotionDoesNotBlockCheckout(t *testing.T) {
	t.Parallel()
	testPartnerIndependentRedemptionHistory(t, false)
}

func TestPartnerRedemptionHistoryForAnotherOrganizationDoesNotBlockCheckout(t *testing.T) {
	t.Parallel()
	testPartnerIndependentRedemptionHistory(t, true)
}

func testPartnerIndependentRedemptionHistory(t *testing.T, differentOrg bool) {
	t.Helper()
	ti := newStripeCheckoutTestInstance(t)
	attributePartnerSignup(t, ti.db, ti.orgID)
	historyOrg, historyPromo := ti.orgID, "promo_other"
	if differentOrg {
		historyOrg, historyPromo = "org_other", "promo_partner"
		_, err := orgrepo.New(ti.db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{ID: historyOrg, Name: "Other billing organization", Slug: "other-billing"})
		require.NoError(t, err)
	}
	err := repo.New(ti.db).InsertOrganizationPromotionApplicationFixture(t.Context(), repo.InsertOrganizationPromotionApplicationFixtureParams{OrganizationID: pgtype.Text{String: historyOrg, Valid: true}, StripePromotionCodeID: historyPromo, StripeSubscriptionID: pgtype.Text{String: "sub_previous", Valid: true}, RedeemedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}})
	require.NoError(t, err)
	_, err = ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 1)
	require.Equal(t, "promo_partner", checkouts[0].PromotionCodeID)
	require.Nil(t, checkouts[0].TrialEnd)
}

func TestOrganizationPromotionApplicationLifecycleConstraints(t *testing.T) {
	t.Parallel()
	ti := newStripeCheckoutTestInstance(t)
	ctx := t.Context()
	q := repo.New(ti.db)
	orgID := pgtype.Text{String: ti.orgID, Valid: true}
	attributePartnerSignup(t, ti.db, ti.orgID)
	pending, err := orgrepo.New(ti.db).GetOrganizationPromotionApplication(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "promo_partner", pending.StripePromotionCodeID)
	require.False(t, pending.RedeemedAt.Valid)
	require.False(t, pending.StripeSubscriptionID.Valid)

	// A second pending offer cannot stack with the selected signup offer.
	require.Error(t, q.InsertOrganizationPromotionApplicationFixture(ctx, repo.InsertOrganizationPromotionApplicationFixtureParams{
		OrganizationID: orgID, StripePromotionCodeID: "promo_other",
	}))
	// Redemption fields transition together, never partially.
	for _, fields := range []repo.InsertOrganizationPromotionApplicationFixtureParams{
		{OrganizationID: orgID, StripePromotionCodeID: "promo_other", RedeemedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}},
		{OrganizationID: orgID, StripePromotionCodeID: "promo_other", StripeSubscriptionID: pgtype.Text{String: "sub_other", Valid: true}},
	} {
		require.Error(t, q.InsertOrganizationPromotionApplicationFixture(ctx, fields))
	}
	rows, err := q.RecordOrganizationPromotionRedemption(ctx, repo.RecordOrganizationPromotionRedemptionParams{
		OrganizationID: orgID, StripePromotionCodeID: "promo_partner", StripeSubscriptionID: pgtype.Text{String: "sub_first", Valid: true},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	rows, err = q.RecordOrganizationPromotionRedemption(ctx, repo.RecordOrganizationPromotionRedemptionParams{
		OrganizationID: orgID, StripePromotionCodeID: "promo_partner", StripeSubscriptionID: pgtype.Text{String: "sub_different", Valid: true},
	})
	require.NoError(t, err)
	require.Zero(t, rows, "redemption cannot overwrite a redeemed application")
	redeemed, err := orgrepo.New(ti.db).GetOrganizationPromotionApplication(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, pending.ID, redeemed.ID)
	require.True(t, redeemed.RedeemedAt.Valid)
	require.Equal(t, "sub_first", redeemed.StripeSubscriptionID.String)
	require.Error(t, q.InsertOrganizationPromotionApplicationFixture(ctx, repo.InsertOrganizationPromotionApplicationFixtureParams{
		OrganizationID: orgID, StripePromotionCodeID: "promo_partner",
	}))

	// History for one offer does not exhaust another offer. No UI exposes this yet.
	require.NoError(t, q.InsertOrganizationPromotionApplicationFixture(ctx, repo.InsertOrganizationPromotionApplicationFixtureParams{
		OrganizationID: orgID, StripePromotionCodeID: "promo_other",
	}))
	selected, err := ti.service.stripeCheckoutPromotionCodeID(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "promo_other", selected)
	blocked, err := q.HasRedeemedSelectedPromotion(ctx, ti.orgID)
	require.NoError(t, err)
	require.False(t, blocked)
}
