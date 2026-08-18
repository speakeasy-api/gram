package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestNoRawUserEmailFilter(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newNoRawUserEmailFilterAnalyzer(noRawUserEmailFilterSettings{}), "norawuseremailfilter")
}

func TestBuildAnalyzersSkipsDisabledNoRawUserEmailFilter(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()
	p.settings.Rules.NoRawUserEmailFilter.Disabled = false

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Len(t, analyzers, 1)
	require.Equal(t, noRawUserEmailFilterAnalyzer, analyzers[0].Name)
}
