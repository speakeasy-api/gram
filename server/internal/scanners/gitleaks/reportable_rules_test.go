package gitleaks

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReportableRuleIDsReflectEffectiveScannerConfig(t *testing.T) {
	t.Parallel()

	first, err := ReportableRuleIDs()
	require.NoError(t, err)
	require.NotEmpty(t, first)
	require.True(t, slices.IsSorted(first))
	require.Equal(t, first, slices.Compact(slices.Clone(first)))
	require.Contains(t, first, SecretAccessKeyRuleID)
	require.Contains(t, first, SessionTokenRuleID)
	require.NotContains(t, first, AccessKeyIDRuleID)

	second, err := ReportableRuleIDs()
	require.NoError(t, err)
	require.Equal(t, first, second)
}
