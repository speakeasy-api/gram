package usage

import "github.com/speakeasy-api/gram/server/internal/billing"

// TUMUnitPriceUSD is the immutable PAYG list price for a token under
// management: $0.35 per million tokens. Carry-forward allocations persist
// this value so their signed correction uses the original service contract.
const TUMUnitPriceUSD = billing.TUMUnitPriceUSD
