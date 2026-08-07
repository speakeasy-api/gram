package email

// WeeklyUsageSummary is the weekly digest sent to an organization's billing
// alert contact summarizing tokens-under-management usage so far in the
// active billing cycle, compared against the same elapsed point of the
// previous cycle. The total is computed by the registry-driven TUM measure,
// so it tracks the TUM definition without a Loops template change.
type WeeklyUsageSummary struct {
	// OrganizationName is the display name of the organization.
	OrganizationName string
	// CycleEndDate is the active billing cycle's last covered day, formatted
	// for display, e.g. "August 6, 2026".
	CycleEndDate string
	// DaysRemaining is the time left in the cycle as a human phrase with its
	// unit included, e.g. "1 day" or "8 days"; the Loops template inserts it
	// without adding a unit of its own.
	DaysRemaining string
	// CycleElapsedPercent is how far through the billing cycle the summary
	// was taken, e.g. "73".
	CycleElapsedPercent string
	// TotalTokens is the cycle-to-date tokens-under-management total,
	// formatted for display.
	TotalTokens string
	// PreviousTotalTokens is the previous cycle's total at the same elapsed
	// point, formatted for display.
	PreviousTotalTokens string
	// TotalChangePercent is the signed percent change of TotalTokens against
	// PreviousTotalTokens, e.g. "+19%", "-3%", or "New" when the previous
	// cycle had no usage at this point.
	TotalChangePercent string
	// UsageTableHTML is the pre-rendered cycle-total summary table. The Loops
	// template inserts it unescaped via a standalone data-variable block
	// (verified to render raw HTML).
	UsageTableHTML string
	// ViewUsageURL links to the organization's billing page.
	ViewUsageURL string
}

func (t WeeklyUsageSummary) TransactionalID() TransactionalID {
	return transactionalIDWeeklyUsageSummary
}

func (t WeeklyUsageSummary) AddToAudience() bool { return false }

func (t WeeklyUsageSummary) Variables() map[string]string {
	return map[string]string{
		"organization_name":     t.OrganizationName,
		"cycle_end_date":        t.CycleEndDate,
		"days_remaining":        t.DaysRemaining,
		"cycle_elapsed_percent": t.CycleElapsedPercent,
		"total_tokens":          t.TotalTokens,
		"previous_total_tokens": t.PreviousTotalTokens,
		"total_change_percent":  t.TotalChangePercent,
		"usage_table_html":      t.UsageTableHTML,
		"view_usage_url":        t.ViewUsageURL,
	}
}
