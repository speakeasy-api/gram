package polar

import (
	polarComponents "github.com/polarsource/polar-go/models/components"
)

type TierLimits struct {
	ToolCalls int
	Servers int
}

func ExtractTierLimits(product *polarComponents.Product) TierLimits {
	freeTierToolCalls := 0
	freeTierServers := 0

	for _, benefit := range product.Benefits {
		if (benefit.BenefitMeterCredit == nil) {
			continue
		}
		benefitProperties := benefit.BenefitMeterCredit.Properties
		if (benefitProperties.MeterID == ToolCallsMeterID) {
			freeTierToolCalls = int(benefitProperties.Units)
		}
		if (benefitProperties.MeterID == ServersMeterID) {
			freeTierServers = int(benefitProperties.Units)
		}
	}

	return TierLimits{
		ToolCalls: freeTierToolCalls,
		Servers: freeTierServers,
	}
}