package email

// WeeklyUsageSummary is the weekly digest sent to an organization's billing
// alert contact summarizing tokens-under-management usage so far in the
// active billing cycle, compared against the same elapsed point of the
// previous cycle.
//
// The per-line-item table is pre-rendered by the sender (one row per
// billing.TumComponents entry plus a total row) so the line items always
// track the TUM definition without a Loops template change. The Loops
// template must therefore insert usage_rows_html unescaped (an HTML block);
// usage_rows_text carries the same rows as plain text for templates or
// clients that cannot render injected HTML.
type WeeklyUsageSummary struct {
	// OrganizationName is the display name of the organization.
	OrganizationName string
	// CycleEndDate is the active billing cycle's last covered day, formatted
	// for display, e.g. "August 6, 2026".
	CycleEndDate string
	// DaysRemaining is the number of days until the cycle ends, e.g. "8".
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
	// UsageRowsHTML is the pre-rendered per-component usage table (HTML).
	UsageRowsHTML string
	// UsageRowsText is the same usage table as plain text lines.
	UsageRowsText string
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
		"usage_rows_html":       t.UsageRowsHTML,
		"usage_rows_text":       t.UsageRowsText,
		"view_usage_url":        t.ViewUsageURL,
	}
}
