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

// A package that has not migrated is left alone.
func TestNoRawAuthzChecksSkipsUnmigratedPackages(t *testing.T) {
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
