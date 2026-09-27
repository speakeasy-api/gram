package email

// WeeklyUsageSummary is the weekly digest sent to an organization's billing
// contacts or administrators. It summarizes storage, scanning, and egress
// usage so far in the active billing cycle, compared with the same elapsed
// point of the previous cycle.
type WeeklyUsageSummary struct {
	// OrganizationName is the display name of the organization.
	OrganizationName string

	// CycleEndDate is the active billing cycle's last covered day, formatted
	// for display, e.g. "August 6, 2026".
	CycleEndDate string

	// DaysRemaining is the time left in the cycle as a human phrase with its
	// unit included, e.g. "1 day" or "8 days".
	DaysRemaining string

	// UsageThroughDate is the last completed UTC day included in the summary,
	// formatted for display.
	UsageThroughDate string

	// StorageTokens is cycle-to-date storage usage, formatted for display.
	StorageTokens string

	// StorageChangePercent is the signed change in storage usage from the same
	// elapsed point of the previous cycle.
	StorageChangePercent string

	// ScanningTokens is cycle-to-date scanning usage, formatted for display.
	ScanningTokens string

	// ScanningChangePercent is the signed change in scanning usage from the
	// same elapsed point of the previous cycle.
	ScanningChangePercent string

	// EgressGib is cycle-to-date egress usage in GiB, formatted for display.
	EgressGib string

	// EgressChangePercent is the signed change in egress usage from the same
	// elapsed point of the previous cycle.
	EgressChangePercent string

	// ShowEstimatedSpend is "true" when the PAYG spend section should render.
	ShowEstimatedSpend string

	// StorageCostUSD is the estimated PAYG storage cost, formatted with a
	// leading dollar sign.
	StorageCostUSD string

	// ScanningCostUSD is the estimated PAYG scanning cost, formatted with a
	// leading dollar sign.
	ScanningCostUSD string

	// EgressCostUSD is the estimated PAYG egress cost, formatted with a leading
	// dollar sign.
	EgressCostUSD string

	// TotalCostUSD is the total estimated PAYG cost, formatted with a leading
	// dollar sign.
	TotalCostUSD string

	// ViewUsageURL links to the organization's billing page.
	ViewUsageURL string
}

func (t WeeklyUsageSummary) Key() TemplateKey {
	return TemplateKeyWeeklyUsageSummary
}

func (t WeeklyUsageSummary) AddToAudience() bool { return false }

func (t WeeklyUsageSummary) Variables() map[string]string {
	return map[string]string{
		"organization_name":       t.OrganizationName,
		"cycle_end_date":          t.CycleEndDate,
		"days_remaining":          t.DaysRemaining,
		"usage_through_date":      t.UsageThroughDate,
		"storage_tokens":          t.StorageTokens,
		"storage_change_percent":  t.StorageChangePercent,
		"scanning_tokens":         t.ScanningTokens,
		"scanning_change_percent": t.ScanningChangePercent,
		"egress_gib":              t.EgressGib,
		"egress_change_percent":   t.EgressChangePercent,
		"show_estimated_spend":    t.ShowEstimatedSpend,
		"storage_cost_usd":        t.StorageCostUSD,
		"scanning_cost_usd":       t.ScanningCostUSD,
		"egress_cost_usd":         t.EgressCostUSD,
		"total_cost_usd":          t.TotalCostUSD,
		"view_usage_url":          t.ViewUsageURL,
	}
}
