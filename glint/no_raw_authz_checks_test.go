package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestNoRawAuthzChecks(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newNoRawAuthzChecksAnalyzer(noRawAuthzChecksSettings{Disabled: false, Packages: []string{"github.com/speakeasy-api/gram/server/internal/norawauthzchecks"}}), "github.com/speakeasy-api/gram/server/internal/norawauthzchecks")
}

// By default the rule applies to every package outside internal/authz.
func TestNoRawAuthzChecksAppliesEverywhereByDefault(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newNoRawAuthzChecksAnalyzer(noRawAuthzChecksSettings{Disabled: false, Packages: nil}), "github.com/speakeasy-api/gram/server/internal/environments")
}

// internal/authz builds the typed refs from these literals and is left alone.
func TestNoRawAuthzChecksSkipsAuthz(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	results := analysistest.Run(t, testdata, newNoRawAuthzChecksAnalyzer(noRawAuthzChecksSettings{Disabled: false, Packages: nil}), "github.com/speakeasy-api/gram/server/internal/authz")
	for _, result := range results {
		require.Empty(t, result.Diagnostics)
	}
}

// A package outside the configured prefixes is left alone.
func TestNoRawAuthzChecksSkipsPackagesOutsideTheList(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	results := analysistest.Run(t, testdata, newNoRawAuthzChecksAnalyzer(noRawAuthzChecksSettings{Disabled: false, Packages: []string{"some/other/package"}}), "github.com/speakeasy-api/gram/server/internal/norawauthzchecksunmigrated")
	for _, result := range results {
		require.Empty(t, result.Diagnostics)
	}
}

func TestBuildAnalyzersSkipsDisabledNoRawAuthzChecks(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()
	p.settings.Rules.NoRawAuthzChecks.Disabled = false

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Len(t, analyzers, 1)
	require.Equal(t, noRawAuthzChecksAnalyzer, analyzers[0].Name)
}
