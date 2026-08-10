// Package money defines Cents, the native Go type the spend-rules tables map
// their *_usd_cents BIGINT columns to (via sqlc go_type overrides). Keeping the
// unit in the type — rather than a bare int64 — makes the cents/USD boundary
// explicit at every call site and centralises the conversion.
package money

import (
	"fmt"
	"math"
)

const centsPerUSD = 100

// Cents is a signed integer count of US cents. It is the sqlc-generated type for
// the spend_rules / spend_rule_events *_usd_cents columns; its int64 kind lets
// pgx scan and encode it directly against BIGINT.
type Cents int64

// FromUSD converts a US dollar amount to Cents, rounding to the nearest cent. It
// rejects NaN, infinities, and amounts whose cent value overflows int64.
func FromUSD(amount float64) (Cents, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, fmt.Errorf("invalid USD amount %v", amount)
	}
	// Asymmetric bounds: float64(math.MaxInt64) rounds up to 2^63, so an amount
	// equal to maxUSD converts to 2^63 and overflows int64 (reject with >=). On
	// the low end, -maxUSD converts to exactly math.MinInt64 (-2^63), which is
	// representable, so only amounts strictly below it overflow (reject with <).
	maxUSD := float64(math.MaxInt64) / centsPerUSD
	if amount >= maxUSD || amount < -maxUSD {
		return 0, fmt.Errorf("USD amount %v is outside cents range", amount)
	}
	return Cents(math.Round(amount * centsPerUSD)), nil
}

// USD returns the value as US dollars.
func (c Cents) USD() float64 {
	return float64(c) / centsPerUSD
}
