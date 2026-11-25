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
	PromotionCodeID  string
	CouponID         string
	GatewayProductID string
	PaymentMethodID  string
	Monthly          bool
	TrialEnd         time.Time
}

func (c *client) checkoutPartnerState(sub *stripesdk.Subscription) *CheckoutPartnerState {
	state := &CheckoutPartnerState{PromotionCodeID: "", CouponID: "", GatewayProductID: "", PaymentMethodID: "", Monthly: false, TrialEnd: unixTime(sub.TrialEnd)}
	if sub.DefaultPaymentMethod != nil {
		state.PaymentMethodID = sub.DefaultPaymentMethod.ID
	}
	if sub.Items == nil || len(sub.Items.Data) == 0 {
		return state
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
		return state
	}
	d := sub.Discounts[0]
	if d == nil || d.Deleted || d.PromotionCode == nil || d.PromotionCode.ID == "" || d.Source == nil || d.Source.Coupon == nil || d.Source.Coupon.ID == "" || d.SubscriptionItem != "" || (d.Subscription != "" && d.Subscription != sub.ID) {
		return state
	}
	state.PromotionCodeID = d.PromotionCode.ID
	state.CouponID = d.Source.Coupon.ID
	state.GatewayProductID = gatewayProduct
	return state
}

// VerifyCheckoutGatewayScope is called only after an enabled offer matches the
// server-retrieved applied ID. Unrelated discounts never depend on this lookup.
func (c *client) VerifyCheckoutGatewayScope(ctx context.Context, state *CheckoutPartnerState) (bool, error) {
	if state == nil || state.CouponID == "" || state.GatewayProductID == "" {
		return false, nil
	}
	params := new(stripesdk.CouponRetrieveParams)
	params.AddExpand("applies_to")
	coupon, err := c.api.retrieveCoupon(ctx, state.CouponID, params)
	if err != nil {
		return false, fmt.Errorf("retrieve applied Checkout coupon scope: %w", err)
	}
	// Applied discounts remain valid evidence after redemption limits are reached.
	// Eligibility here concerns scope, not permission to redeem the coupon again.
	return coupon != nil && coupon.ID == state.CouponID && coupon.AppliesTo != nil && len(coupon.AppliesTo.Products) == 1 && coupon.AppliesTo.Products[0] == state.GatewayProductID, nil
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
