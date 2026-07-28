package email

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTumUsageThreshold_TransactionalID(t *testing.T) {
	t.Parallel()

	require.Equal(t, transactionalIDTumUsageThreshold, TumUsageThreshold{}.TransactionalID())
}

func TestTumUsageThreshold_Variables_RendersExpectedKeys(t *testing.T) {
	t.Parallel()

	tmpl := TumUsageThreshold{
		OrganizationName: "Acme Inc",
		ThresholdPercent: "75",
		UsageTokens:      "45,000,000",
		TokenLimit:       "60,000,000",
		CycleStart:       "June 1, 2026",
		CycleEnd:         "July 1, 2026",
	}

	require.Equal(t, map[string]string{
		"organization_name": "Acme Inc",
		"threshold_percent": "75",
		"usage_tokens":      "45,000,000",
		"token_limit":       "60,000,000",
		"cycle_start":       "June 1, 2026",
		"cycle_end":         "July 1, 2026",
	}, tmpl.Variables())
}

func TestTumUsageThreshold_Variables_PassesEmptyFieldsThrough(t *testing.T) {
	t.Parallel()

	vars := TumUsageThreshold{}.Variables()
	require.Len(t, vars, 6, "all merge keys must be present even when empty")
}

func TestTumUsageThreshold_AddToAudience(t *testing.T) {
	t.Parallel()

	require.False(t, TumUsageThreshold{}.AddToAudience(),
		"billing alerts should not add recipients to the Loops audience")
}
