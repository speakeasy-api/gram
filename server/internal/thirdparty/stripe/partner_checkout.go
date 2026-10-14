package stripe

import (
	"context"
	"errors"
	"fmt"
	stripesdk "github.com/stripe/stripe-go/v85"
	"time"
)

// CheckoutPartnerState is server-retrieved evidence, never a client claim.
type CheckoutPartnerState struct {
	PromotionCodeID string
	GatewayEligible bool
	PaymentMethodID string
	Monthly         bool
	TrialEnd        time.Time
}

func (c *client) checkoutPartnerState(ctx context.Context, sub *stripesdk.Subscription) (*CheckoutPartnerState, error) {
	state := &CheckoutPartnerState{PromotionCodeID: "", GatewayEligible: false, PaymentMethodID: "", Monthly: false, TrialEnd: unixTime(sub.TrialEnd)}
	if sub.DefaultPaymentMethod != nil {
		state.PaymentMethodID = sub.DefaultPaymentMethod.ID
	}
	if sub.Items == nil || len(sub.Items.Data) == 0 {
		return state, nil
	}
	state.Monthly = true
	gatewayProduct := ""
	for _, item := range sub.Items.Data {
		if item == nil || item.Price == nil || item.Price.Recurring == nil || item.Price.Recurring.Interval != stripesdk.PriceRecurringIntervalMonth || item.Price.Recurring.IntervalCount != 1 {
			state.Monthly = false
			continue
		}
		if item.Price.ID == c.catalog.PriceIDMCPEgress && item.Price.Product != nil {
			gatewayProduct = item.Price.Product.ID
		}
	}
	if len(sub.Discounts) != 1 {
		return state, nil
	}
	d := sub.Discounts[0]
	if d == nil || d.Deleted || d.PromotionCode == nil || d.PromotionCode.ID == "" || d.Source == nil || d.Source.Coupon == nil || d.Source.Coupon.ID == "" || d.SubscriptionItem != "" || (d.Subscription != "" && d.Subscription != sub.ID) {
		return state, nil
	}
	state.PromotionCodeID = d.PromotionCode.ID
	params := new(stripesdk.CouponRetrieveParams)
	params.AddExpand("applies_to")
	coupon, err := c.api.retrieveCoupon(ctx, d.Source.Coupon.ID, params)
	if err != nil {
		return nil, fmt.Errorf("retrieve applied Checkout coupon scope: %w", err)
	}
	// Applied discounts remain valid evidence after redemption limits are reached.
	// Eligibility here concerns scope, not permission to redeem the coupon again.
	state.GatewayEligible = coupon != nil && coupon.ID == d.Source.Coupon.ID && coupon.AppliesTo != nil && len(coupon.AppliesTo.Products) == 1 && gatewayProduct != "" && coupon.AppliesTo.Products[0] == gatewayProduct
	return state, nil
}

// EndPartnerCheckoutTrial ends only an already accepted partner Checkout trial.
// The caller durably binds acceptance to Checkout before calling and must retrieve
// fresh subscription state afterwards; an update response is not activation proof.
func (c *client) EndPartnerCheckoutTrial(ctx context.Context, subscriptionID, idempotencyKey string) error {
	if subscriptionID == "" || idempotencyKey == "" {
		return errors.New("partner trial conversion requires subscription and idempotency identities")
	}
	params := new(stripesdk.SubscriptionUpdateParams)
	params.TrialEndNow = new(true)
	params.ProrationBehavior = stripesdk.String("none")
	params.SetIdempotencyKey(idempotencyKey)
	subscription, err := c.api.updateSubscription(ctx, subscriptionID, params)
	if err != nil {
		return fmt.Errorf("end accepted partner subscription trial: %w", err)
	}
	if subscription == nil || subscription.ID != subscriptionID {
		return errors.New("partner trial update returned an unexpected subscription")
	}
	return nil
}
