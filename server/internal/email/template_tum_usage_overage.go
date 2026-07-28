package email

// TumUsageOverage is sent to an organization's billing alert contact when
// tokens-under-management usage for the active billing cycle reaches or
// exceeds the contracted monthly allowance (100%, then every additional 50%).
type TumUsageOverage struct {
	// OrganizationName is the display name of the organization.
	OrganizationName string
	// ThresholdPercent is the crossed threshold, e.g. "150".
	ThresholdPercent string
	// UsageTokens is the cycle's tokens-under-management total so far,
	// formatted for display.
	UsageTokens string
	// TokenLimit is the contracted monthly token allowance, formatted for
	// display.
	TokenLimit string
	// OverageTokens is how far usage sits beyond the allowance, formatted for
	// display.
	OverageTokens string
	// CycleStart is the active billing cycle's start date, formatted for
	// display.
	CycleStart string
	// CycleEnd is the active billing cycle's end date, formatted for display.
	CycleEnd string
}

func (t TumUsageOverage) TransactionalID() TransactionalID {
	return transactionalIDTumUsageOverage
}

func (t TumUsageOverage) AddToAudience() bool { return false }

func (t TumUsageOverage) Variables() map[string]string {
	return map[string]string{
		"organization_name": t.OrganizationName,
		"threshold_percent": t.ThresholdPercent,
		"usage_tokens":      t.UsageTokens,
		"token_limit":       t.TokenLimit,
		"overage_tokens":    t.OverageTokens,
		"cycle_start":       t.CycleStart,
		"cycle_end":         t.CycleEnd,
	}
}
