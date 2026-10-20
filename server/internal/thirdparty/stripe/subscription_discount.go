package stripe

import (
	"fmt"
	"math"
	"time"

	stripesdk "github.com/stripe/stripe-go/v85"
)

// Only subscription-attached discounts are evidence of an applied promotion.
// Redemption validity is deliberately not checked: a coupon can stop accepting
// new redemptions while its existing subscription discount remains in force.
func (c *client) subscriptionDiscount(sub *stripesdk.Subscription, now time.Time) (*string, time.Time, bool) {
	if sub.Status != stripesdk.SubscriptionStatusActive && sub.Status != stripesdk.SubscriptionStatusPastDue {
		return nil, time.Time{}, false
	}
	// Stacking changes the effective benefit; don't describe one coupon as the total.
	if len(sub.Discounts) != 1 {
		return nil, time.Time{}, false
	}
	d := sub.Discounts[0]
	if d == nil || d.ID == "" || d.Deleted || d.Start <= 0 || d.Start > now.Unix() || (d.End != 0 && d.End <= now.Unix()) || d.Source == nil || d.Source.Coupon == nil {
		return nil, time.Time{}, false
	}
	coupon := d.Source.Coupon
	// A once-only coupon may remain attached after its invoice was discounted;
	// without invoice history it does not prove a current benefit.
	if coupon.Duration != stripesdk.CouponDurationRepeating && coupon.Duration != stripesdk.CouponDurationForever {
		return nil, time.Time{}, false
	}
	if coupon.Duration == stripesdk.CouponDurationRepeating && d.End == 0 {
		return nil, time.Time{}, false
	}
	if coupon.ID == "" || (d.Subscription != "" && d.Subscription != sub.ID) || d.SubscriptionItem != "" {
		return nil, time.Time{}, false
	}
	gatewayOnly := false
	monthlyGatewayPrice := false
	if coupon.AppliesTo != nil && len(coupon.AppliesTo.Products) == 1 && sub.Items != nil {
		for _, item := range sub.Items.Data {
			if item == nil || item.Price == nil {
				continue
			}
			p := item.Price
			if !IsConfigured(c.catalog.PriceIDMCPEgress) || p.ID != c.catalog.PriceIDMCPEgress || p.Product == nil || p.Product.ID != coupon.AppliesTo.Products[0] {
				continue
			}
			gatewayOnly = true
			// The metered gateway reports bytes. Allow only Stripe's half-unit rounding
			// at its 12-decimal cents/byte precision, not a materially different rate.
			monthlyGatewayPrice = p.Currency == stripesdk.CurrencyUSD &&
				p.BillingScheme == stripesdk.PriceBillingSchemePerUnit &&
				p.TransformQuantity == nil && p.Recurring != nil &&
				p.Recurring.Interval == stripesdk.PriceRecurringIntervalMonth &&
				p.Recurring.IntervalCount == 1 &&
				p.Recurring.UsageType == stripesdk.PriceRecurringUsageTypeMetered &&
				math.Abs(p.UnitAmountDecimal*1073741824-2000) <= 0.000537
		}
	}
	known := gatewayOnly && monthlyGatewayPrice && coupon.Currency == stripesdk.CurrencyUSD && coupon.AmountOff == 10000 && coupon.PercentOff == 0
	description := ""
	if known {
		description = "5 GiB of gateway usage free each month ($100 discount)."
	} else {
		scope := "eligible charges"
		if gatewayOnly {
			scope = "gateway usage"
		}
		if coupon.AmountOff > 0 && coupon.Currency == stripesdk.CurrencyUSD && coupon.PercentOff == 0 {
			description = fmt.Sprintf("$%g discount on %s.", float64(coupon.AmountOff)/100, scope)
		} else if coupon.PercentOff > 0 && coupon.PercentOff <= 100 && coupon.AmountOff == 0 {
			description = fmt.Sprintf("%g%% discount on %s.", coupon.PercentOff, scope)
		}
	}
	if description == "" {
		return nil, time.Time{}, false
	}
	return &description, unixTime(d.End), known
}
