package billing

import gen "github.com/speakeasy-api/gram/server/gen/usage"

// TUMPricePerMillionUSD is the exact PAYG list price rendered by clients.
const TUMPricePerMillionUSD = "0.35"

// NewPaygTierLimits returns the usage-tier contract shared by every billing
// provider. Each call returns independently mutable slices.
func NewPaygTierLimits() *gen.TierLimits {
	price := TUMPricePerMillionUSD
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
			"Security inference funded by Speakeasy",
		},
		AddOnBullets:          []string{},
		TumPricePerMillionUsd: &price,
	}
}
