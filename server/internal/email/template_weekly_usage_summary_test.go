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
		DaysRemaining:       "8 days",
		CycleElapsedPercent: "73",
		TotalTokens:         "45,000,000",
		PreviousTotalTokens: "38,000,000",
		TotalChangePercent:  "+18%",
		UsageTableHTML:      "<table></table>",
		ViewUsageURL:        "https://app.getgram.ai/acme/billing",
	}

	require.Equal(t, map[string]string{
		"organization_name":     "Acme Inc",
		"cycle_end_date":        "August 6, 2026",
		"days_remaining":        "8 days",
		"cycle_elapsed_percent": "73",
		"total_tokens":          "45,000,000",
		"previous_total_tokens": "38,000,000",
		"total_change_percent":  "+18%",
		"usage_table_html":      "<table></table>",
		"view_usage_url":        "https://app.getgram.ai/acme/billing",
	}, tmpl.Variables())
}

func TestWeeklyUsageSummary_Variables_PassesEmptyFieldsThrough(t *testing.T) {
	t.Parallel()

	vars := WeeklyUsageSummary{}.Variables()
	require.Len(t, vars, 9, "all merge keys must be present even when empty")
}

func TestWeeklyUsageSummary_AddToAudience(t *testing.T) {
	t.Parallel()

	require.False(t, WeeklyUsageSummary{}.AddToAudience(),
		"usage digests should not add recipients to the Loops audience")
}
