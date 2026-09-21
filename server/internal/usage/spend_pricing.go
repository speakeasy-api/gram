package usage

import (
	"fmt"
	"math/big"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/metering"
)

const (
	millionRateQuantity = "1000000"
	gibRateQuantity     = "1073741824"
)

// SpendProductSpec defines the exact PAYG list price for one metered product.
type SpendProductSpec struct {
	// ID is the stable product identifier returned by metering queries.
	ID string

	// Label is the dashboard display name.
	Label string

	// Unit is the unit carried by the underlying meter readings.
	Unit string

	// RateQuantity is the quantity covered by one RateUSD charge.
	RateQuantity string

	// RateUSD is the exact decimal USD price per RateQuantity.
	RateUSD string
}

var spendProductSpecs = [...]SpendProductSpec{
	{
		ID:           "agent_session_storage",
		Label:        "Agent session storage",
		Unit:         string(metering.UnitSTokens),
		RateQuantity: millionRateQuantity,
		RateUSD:      billing.TUMPricePerMillionUSD,
	},
	{
		ID:           "risk_content_scans",
		Label:        "Risk scanning",
		Unit:         string(metering.UnitSTokens),
		RateQuantity: millionRateQuantity,
		RateUSD:      billing.RiskScanPricePerMillionUSD,
	},
	{
		ID:           "mcp_egress",
		Label:        "MCP gateway",
		Unit:         string(metering.UnitBytes),
		RateQuantity: gibRateQuantity,
		RateUSD:      billing.MCPEgressPricePerGiBUSD,
	},
}

// SpendProductSpecs returns the priced products in dashboard display order.
func SpendProductSpecs() [3]SpendProductSpec {
	return spendProductSpecs
}

// PriceSpendQuantity applies a product's exact PAYG list price to quantity.
func PriceSpendQuantity(quantity *big.Int, spec SpendProductSpec) (*big.Rat, error) {
	rate, ok := new(big.Rat).SetString(spec.RateUSD)
	if !ok {
		return nil, fmt.Errorf("invalid spend rate %q", spec.RateUSD)
	}
	rateQuantity, ok := new(big.Int).SetString(spec.RateQuantity, 10)
	if !ok || rateQuantity.Sign() <= 0 {
		return nil, fmt.Errorf("invalid spend rate quantity %q", spec.RateQuantity)
	}
	return new(big.Rat).Quo(new(big.Rat).Mul(new(big.Rat).SetInt(quantity), rate), new(big.Rat).SetInt(rateQuantity)), nil
}
