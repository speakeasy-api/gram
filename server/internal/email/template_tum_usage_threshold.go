package email

// TumUsageThreshold is sent to an organization's billing alert contact when
// tokens-under-management usage for the active billing cycle crosses an
// approach threshold (50%, 75%, 90%) of the contracted monthly allowance.
// Usage at or beyond 100% sends TumUsageOverage instead.
type TumUsageThreshold struct {
	// OrganizationName is the display name of the organization.
	OrganizationName string
	// ThresholdPercent is the crossed threshold, e.g. "75".
	ThresholdPercent string
	// UsageTokens is the cycle's tokens-under-management total so far,
	// formatted for display.
	UsageTokens string
	// TokenLimit is the contracted monthly token allowance, formatted for
	// display.
	TokenLimit string
	// CycleStart is the active billing cycle's start date, formatted for
	// display.
	CycleStart string
	// CycleEnd is the active billing cycle's end date, formatted for display.
	CycleEnd string
}

func (t TumUsageThreshold) TransactionalID() TransactionalID {
	return transactionalIDTumUsageThreshold
}

func (t TumUsageThreshold) AddToAudience() bool { return false }

func (t TumUsageThreshold) Variables() map[string]string {
	return map[string]string{
		"organization_name": t.OrganizationName,
		"threshold_percent": t.ThresholdPercent,
		"usage_tokens":      t.UsageTokens,
		"token_limit":       t.TokenLimit,
		"cycle_start":       t.CycleStart,
		"cycle_end":         t.CycleEnd,
	}
}
