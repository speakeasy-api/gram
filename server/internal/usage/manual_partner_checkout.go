package usage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

type partnerScopeVerifier interface {
	VerifyCheckoutGatewayScope(context.Context, *stripeclient.CheckoutPartnerState) (bool, error)
}

type partnerTrialEnder interface {
	EndPartnerCheckoutTrial(context.Context, string, string) error
}

// acceptManualPartnerCheckout persists acceptance before a remote billing write.
// The completed session remains bound in billing_metadata until redemption and
// activation commit. No webhook receipt is committed by this preparation step.
func (s *Service) acceptManualPartnerCheckout(ctx context.Context, event *stripeclient.WebhookEvent, checkout *stripeclient.CheckoutSessionState) error {
	if checkout == nil || checkout.Partner == nil || checkout.Partner.PromotionCodeID == "" {
		return nil
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("prepare accepted partner Checkout: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := repo.New(tx)
	if err := q.AcquireStripeSubscriptionActivationLock(ctx, event.SubscriptionID); err != nil {
		return fmt.Errorf("prepare accepted partner Checkout: %w", err)
	}
	org, err := q.GetBillingMetadataOrganizationByStripeCustomerID(ctx, pgtype.Text{String: event.CustomerID, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("prepare accepted partner Checkout: %w", err)
	}
	if _, err := trialsrepo.New(tx).LockTrialLifecycle(ctx, org); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("prepare accepted partner Checkout: %w", err)
	}
	if err := acquireOpenRouterBillingLocks(ctx, q, org); err != nil {
		return fmt.Errorf("prepare accepted partner Checkout: %w", err)
	}
	if err := q.LockBillingMetadataOrganization(ctx, org); err != nil {
		return fmt.Errorf("prepare accepted partner Checkout: %w", err)
	}
	state, err := q.GetPaygActivationState(ctx, org)
	if err != nil {
		return fmt.Errorf("prepare accepted partner Checkout: %w", err)
	}
	organizations := orgrepo.New(tx)
	_, err = organizations.GetOrganizationPromotionApplication(ctx, org)
	if err == nil {
		return nil
	} // Accepted URL/manual applications are immutable.
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("prepare accepted partner Checkout: %w", err)
	}
	_, err = organizations.GetEnabledSignupOffer(ctx, checkout.Partner.PromotionCodeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("prepare accepted partner Checkout: %w", err)
	}
	if state.StripeSubscriptionID.Valid {
		return errors.New("manual partner offer requires a first Checkout subscription")
	}
	prior, err := q.HasActivatedPaygSubscription(ctx, org)
	if err != nil {
		return fmt.Errorf("prepare accepted partner Checkout: %w", err)
	}
	if prior {
		return errors.New("manual partner offer is not available to an existing subscriber")
	}
	if !state.StripeCheckoutSessionID.Valid || state.StripeCheckoutSessionID.String != checkout.ID || !state.StripeCheckoutIdempotencyKey.Valid || state.StripeCheckoutIdempotencyKey.String == "" {
		return errors.New("manual partner Checkout does not match the persisted intent")
	}
	live, eligible, err := s.currentCheckoutSession(ctx, event)
	if err != nil {
		return fmt.Errorf("prepare accepted partner Checkout: %w", err)
	}
	if !eligible || live.Partner == nil || live.Partner.PromotionCodeID != checkout.Partner.PromotionCodeID {
		return errors.New("manual partner Checkout identity changed")
	}
	verifier, ok := s.stripeClient.(partnerScopeVerifier)
	if !ok {
		return errors.New("partner coupon scope verification is unavailable")
	}
	scopeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	gatewayEligible, err := verifier.VerifyCheckoutGatewayScope(scopeCtx, live.Partner)
	cancel()
	if err != nil {
		return fmt.Errorf("verify first partner coupon scope: %w", err)
	}
	if !gatewayEligible {
		return nil
	} // Unsupported scopes remain ordinary discounts.
	if !live.Partner.Monthly || live.Partner.PaymentMethodID == "" {
		return errors.New("manual partner Checkout is not eligible for paid conversion")
	}
	rows, err := organizations.AttributePartnerSignup(ctx, orgrepo.AttributePartnerSignupParams{OrganizationID: pgtype.Text{String: org, Valid: true}, StripePromotionCodeID: live.Partner.PromotionCodeID})
	if err != nil {
		return fmt.Errorf("prepare accepted partner Checkout: %w", err)
	}
	if rows != 1 {
		return errors.New("manual partner acceptance changed concurrently")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit partner acceptance: %w", err)
	}
	return nil
}

// verifiedPartnerCheckoutTx runs with the activation locks held. Never trust the
// pre-lock Stripe read: a deletion may have committed while this handler waited.
func (s *Service) verifiedPartnerCheckoutTx(ctx context.Context, tx pgx.Tx, org string, event *stripeclient.WebhookEvent, state repo.GetPaygActivationStateRow, checkout *stripeclient.CheckoutSessionState) (*stripeclient.CheckoutSessionState, error) {
	app, err := orgrepo.New(tx).GetOrganizationPromotionApplication(ctx, org)
	if errors.Is(err, pgx.ErrNoRows) {
		return checkout, nil
	}
	if err != nil {
		return nil, fmt.Errorf("verify accepted partner Checkout: %w", err)
	}
	if !app.RedeemedAt.Valid && (!state.StripeCheckoutSessionID.Valid || state.StripeCheckoutSessionID.String != checkout.ID || !state.StripeCheckoutIdempotencyKey.Valid || state.StripeCheckoutIdempotencyKey.String == "") {
		return nil, errors.New("partner Checkout is not bound to the persisted intent")
	}
	live, eligible, err := s.currentCheckoutSession(ctx, event)
	if err != nil {
		return nil, fmt.Errorf("verify accepted partner Checkout: %w", err)
	}
	if !eligible {
		return nil, errors.New("partner subscription cannot be activated")
	}
	if live.Partner != nil {
		if live.Partner.PromotionCodeID != app.StripePromotionCodeID || live.Partner.PaymentMethodID == "" {
			return nil, errors.New("partner Checkout promotion or payment method does not match acceptance")
		}
		if live.SubscriptionStatus == "trialing" && !app.RedeemedAt.Valid {
			if !live.Partner.Monthly {
				return nil, errors.New("partner conversion requires monthly subscription items")
			}
			ender, ok := s.stripeClient.(partnerTrialEnder)
			if !ok {
				return nil, errors.New("partner trial conversion is unavailable")
			}
			operationCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err = ender.EndPartnerCheckoutTrial(operationCtx, event.SubscriptionID, "partner-trial-end:"+checkout.ID)
			cancel()
			if err != nil {
				return nil, fmt.Errorf("convert accepted partner trial: %w", err)
			}
			live, eligible, err = s.currentCheckoutSession(ctx, event)
			if err != nil {
				return nil, fmt.Errorf("verify accepted partner Checkout: %w", err)
			}
			if !eligible || live.Partner == nil || live.Partner.PromotionCodeID != app.StripePromotionCodeID || live.Partner.PaymentMethodID == "" {
				return nil, errors.New("converted partner Checkout identity changed")
			}
		}
	}
	if live.Partner != nil && !live.Partner.Monthly {
		return nil, errors.New("partner subscription no longer has supported monthly periods")
	}
	if live.SubscriptionStatus != "active" {
		return nil, errors.New("partner subscription is not active; retry after payment confirmation")
	}
	if app.RedeemedAt.Valid && (!state.StripeBillingCycleAnchor.Valid || !state.StripeBillingCycleAnchor.Time.Equal(live.BillingCycleAnchor)) {
		return nil, errors.New("redeemed partner subscription billing anchor changed")
	}
	return live, nil
}
