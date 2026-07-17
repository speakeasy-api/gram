package email

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTumUsageOverage_TransactionalID(t *testing.T) {
	t.Parallel()

	require.Equal(t, transactionalIDTumUsageOverage, TumUsageOverage{}.TransactionalID())
}

func TestTumUsageOverage_Variables_RendersExpectedKeys(t *testing.T) {
	t.Parallel()

	tmpl := TumUsageOverage{
		OrganizationName: "Acme Inc",
		ThresholdPercent: "150",
		UsageTokens:      "90,000,000",
		TokenLimit:       "60,000,000",
		OverageTokens:    "30,000,000",
		CycleStart:       "June 1, 2026",
		CycleEnd:         "July 1, 2026",
	}

	require.Equal(t, map[string]string{
		"organization_name": "Acme Inc",
		"threshold_percent": "150",
		"usage_tokens":      "90,000,000",
		"token_limit":       "60,000,000",
		"overage_tokens":    "30,000,000",
		"cycle_start":       "June 1, 2026",
		"cycle_end":         "July 1, 2026",
	}, tmpl.Variables())
}

func TestTumUsageOverage_Variables_PassesEmptyFieldsThrough(t *testing.T) {
	t.Parallel()

	vars := TumUsageOverage{}.Variables()
	require.Len(t, vars, 7, "all merge keys must be present even when empty")
}

func TestTumUsageOverage_AddToAudience(t *testing.T) {
	t.Parallel()

	require.False(t, TumUsageOverage{}.AddToAudience(),
		"billing alerts should not add recipients to the Loops audience")
}
