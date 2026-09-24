package gram

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

// TestNewLocalFeatureFlagsAcceptsRowsWithAndWithoutVariant pins the contract
// flags.csv relies on: the header names four columns while rows carry three
// or four, a three-column row still sets its boolean, and only a non-empty
// fourth column sets a variant.
func TestNewLocalFeatureFlagsAcceptsRowsWithAndWithoutVariant(t *testing.T) { //nolint:paralleltest // changes the working directory
	t.Chdir(t.TempDir())
	wd, err := os.Getwd()
	require.NoError(t, err)
	path := filepath.Join(wd, "flags.csv")
	require.NoError(t, os.WriteFile(path, []byte(
		"distinct_id,flag,enabled,variant\n"+
			"org_three,gram-risk-llm-analyzer,true\n"+
			"org_four,gram-risk-llm-analyzer,true,shadow\n"+
			"# a comment line\n"+
			"org_empty,gram-risk-llm-analyzer,true,\n"+
			"org_off,gram-risk-watchdog,false\n",
	), 0o600))

	flags := newLocalFeatureFlags(t.Context(), slog.New(slog.DiscardHandler), path)

	for _, tc := range []struct {
		distinctID string
		flag       feature.Flag
		enabled    bool
		variant    feature.Variant
	}{
		{distinctID: "org_three", flag: feature.FlagRiskLLMAnalyzer, enabled: true, variant: ""},
		{distinctID: "org_four", flag: feature.FlagRiskLLMAnalyzer, enabled: true, variant: feature.VariantRiskLLMShadow},
		{distinctID: "org_empty", flag: feature.FlagRiskLLMAnalyzer, enabled: true, variant: ""},
		{distinctID: "org_off", flag: feature.FlagRiskWatchdog, enabled: false, variant: ""},
	} {
		enabled, err := flags.IsFlagEnabled(t.Context(), tc.flag, tc.distinctID, nil)
		require.NoError(t, err, tc.distinctID)
		require.Equal(t, tc.enabled, enabled, tc.distinctID)
		variant, err := flags.FlagVariant(t.Context(), tc.flag, tc.distinctID, nil)
		require.NoError(t, err, tc.distinctID)
		require.Equal(t, tc.variant, variant, tc.distinctID)
	}
}
