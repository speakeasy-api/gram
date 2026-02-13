package polar

import (
	polarComponents "github.com/polarsource/polar-go/models/components"
)

type TierLimits struct {
	ToolCalls int
	Servers   int
	Credits   int
}

func extractTierLimits(catalog *Catalog, product *polarComponents.Product) TierLimits {
	toolCallLimit := 0
	serversLimit := 0
	creditsLimit := 0

	for _, benefit := range product.Benefits {
		if benefit.BenefitMeterCredit == nil {
			continue
		}
		benefitProperties := benefit.BenefitMeterCredit.Properties
		if benefitProperties.MeterID == catalog.MeterIDToolCalls {
			toolCallLimit = int(benefitProperties.Units)
		}
		if benefitProperties.MeterID == catalog.MeterIDServers {
			serversLimit = int(benefitProperties.Units)
		}
		if benefitProperties.MeterID == catalog.MeterIDCredits {
			creditsLimit = int(benefitProperties.Units)
		}
	}

	return TierLimits{
		ToolCalls: toolCallLimit,
		Servers:   serversLimit,
		Credits:   creditsLimit,
	}
}
