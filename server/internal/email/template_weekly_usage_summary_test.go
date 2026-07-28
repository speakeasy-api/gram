package email

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWeeklyUsageSummary_TransactionalID(t *testing.T) {
	t.Parallel()

	require.Equal(t, transactionalIDWeeklyUsageSummary, WeeklyUsageSummary{}.TransactionalID())
}

func TestWeeklyUsageSummary_Variables_RendersExpectedKeys(t *testing.T) {
	t.Parallel()

	tmpl := WeeklyUsageSummary{
		OrganizationName:    "Acme Inc",
		CycleEndDate:        "August 6, 2026",
		DaysRemaining:       "8",
		CycleElapsedPercent: "73",
		TotalTokens:         "45,000,000",
		PreviousTotalTokens: "38,000,000",
		TotalChangePercent:  "+18%",
		UsageRowsHTML:       "<table></table>",
		UsageRowsText:       "Input tokens: 1 (previous cycle at this point: 2, -50%)",
		ViewUsageURL:        "https://app.getgram.ai/acme/billing",
	}

	require.Equal(t, map[string]string{
		"organization_name":     "Acme Inc",
		"cycle_end_date":        "August 6, 2026",
		"days_remaining":        "8",
		"cycle_elapsed_percent": "73",
		"total_tokens":          "45,000,000",
		"previous_total_tokens": "38,000,000",
		"total_change_percent":  "+18%",
		"usage_rows_html":       "<table></table>",
		"usage_rows_text":       "Input tokens: 1 (previous cycle at this point: 2, -50%)",
		"view_usage_url":        "https://app.getgram.ai/acme/billing",
	}, tmpl.Variables())
}

func TestWeeklyUsageSummary_Variables_PassesEmptyFieldsThrough(t *testing.T) {
	t.Parallel()

	vars := WeeklyUsageSummary{}.Variables()
	require.Len(t, vars, 10, "all merge keys must be present even when empty")
}

func TestWeeklyUsageSummary_AddToAudience(t *testing.T) {
	t.Parallel()

	require.False(t, WeeklyUsageSummary{}.AddToAudience(),
		"usage digests should not add recipients to the Loops audience")
}
