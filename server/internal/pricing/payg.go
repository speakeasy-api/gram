// Package pricing holds the server-side port of Gram's pay-as-you-go (PAYG)
// rate card. The canonical model lives in the dashboard's contract-pricing.ts
// and is used there for the platform-admin contract estimator; this package
// mirrors the PAYG bands so backend internal tooling (the admin pricing
// tracker) can quote the same monthly PAYG figure off observed token volume
// without a round-trip to the browser.
//
// Nothing here is billed from — these are sales-side approximations off
// observed tokens under management, kept in a pure, unit-testable module.
package pricing

const tokensPerMillion = 1_000_000

// paygTier is one graduated pay-as-you-go band, keyed on an absolute monthly
// token ceiling. Rates are expressed in US dollars per million tokens.
type paygTier struct {
	upToTokens     float64
	ratePerMillion float64
}

// paygTiers are the pay-as-you-go bands, keyed on absolute monthly volume. The
// floor rate ($0.24) deliberately sits above both the committed baseline rate
// and the lowest contract overage rate, so PAYG is never the cheaper option at
// equivalent volume — it prices flexibility, not discount. Keep these in sync
// with PAYG_TIERS in client/dashboard/src/components/billing/contract-pricing.ts.
var paygTiers = []paygTier{
	{upToTokens: 10e9, ratePerMillion: 0.35},
	{upToTokens: 30e9, ratePerMillion: 0.30},
	{upToTokens: 75e9, ratePerMillion: 0.27},
	{upToTokens: 1e18, ratePerMillion: 0.24}, // effectively unbounded top band
}

// PaygMonthlyPrice returns the full pay-as-you-go bill, in US dollars, for one
// month at monthlyTokens of observed volume. Every token is charged from the
// first one against the graduated bands (no baseline to subtract): the bill is
// the sum of each band's slice, matching graduatedLines in contract-pricing.ts.
// Zero or negative volume prices at $0.
func PaygMonthlyPrice(monthlyTokens int64) float64 {
	if monthlyTokens <= 0 {
		return 0
	}
	tokens := float64(monthlyTokens)
	var cost float64
	var lower float64
	for _, band := range paygTiers {
		upper := band.upToTokens
		inBand := min(tokens, upper) - lower
		if inBand > 0 {
			cost += (inBand / tokensPerMillion) * band.ratePerMillion
		}
		if tokens <= upper {
			break
		}
		lower = upper
	}
	return cost
}

// PaygEffectiveRatePerMillion returns the blended dollars-per-million-tokens
// that the monthly PAYG bill works out to at the given volume — the single
// number that makes rate cards comparable across volumes. Returns 0 when there
// is no volume to rate.
func PaygEffectiveRatePerMillion(monthlyTokens int64) float64 {
	if monthlyTokens <= 0 {
		return 0
	}
	return PaygMonthlyPrice(monthlyTokens) / (float64(monthlyTokens) / tokensPerMillion)
}
