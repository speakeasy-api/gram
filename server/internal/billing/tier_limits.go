package billing

import gen "github.com/speakeasy-api/gram/server/gen/usage"

const (
	// TUMUnitPriceUSD is the exact Stripe meter price for one managed token.
	TUMUnitPriceUSD = "0.00000035"
	// TUMPricePerMillionUSD is the current PAYG list price per million managed tokens.
	TUMPricePerMillionUSD = "0.35"
	// RiskScanPricePerMillionUSD is the current PAYG list price per million tokens processed by each scanner.
	RiskScanPricePerMillionUSD = "0.99"
	// MCPEgressPricePerGiBUSD is the current PAYG list price per GiB of MCP gateway egress.
	MCPEgressPricePerGiBUSD = "20"
)

// NewPaygTierLimits returns the usage-tier contract shared by every billing
// provider. Each call returns independently mutable slices.
func NewPaygTierLimits() *gen.TierLimits {
	price := TUMPricePerMillionUSD
	riskScanPrice := RiskScanPricePerMillionUSD
	mcpEgressPrice := MCPEgressPricePerGiBUSD
	return &gen.TierLimits{
		BasePrice:                  0,
		IncludedToolCalls:          0,
		IncludedServers:            0,
		IncludedCredits:            0,
		PricePerAdditionalToolCall: 0,
		PricePerAdditionalServer:   0,
		FeatureBullets: []string{
			"Oauth 2.1 proxy support",
			"Register your own OAuth server",
			"Custom domain",
			"30 day log retention",
			"SSO",
			"Audit logs",
			"Self-hosting Gram dataplane",
		},
		IncludedBullets: []string{
			"Other inference billed at provider cost",
			"Platform-initiated inference billed at provider cost",
		},
		AddOnBullets:               []string{},
		TumPricePerMillionUsd:      &price,
		RiskScanPricePerMillionUsd: &riskScanPrice,
		McpEgressPricePerGibUsd:    &mcpEgressPrice,
	}
}
