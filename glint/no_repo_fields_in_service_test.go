package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestNoRepoFieldsInServiceGood(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newNoRepoFieldsInServiceAnalyzer(noRepoFieldsInServiceSettings{}), "serviceannotation")
}

func TestNoRepoFieldsInServiceViolation(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newNoRepoFieldsInServiceAnalyzer(noRepoFieldsInServiceSettings{}), "serviceannotationrepofield")
}

func TestBuildAnalyzersSkipsDisabledNoRepoFieldsInService(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()
	p.settings.Rules.NoRepoFieldsInService.Disabled = false

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Len(t, analyzers, 1)
	require.Equal(t, noRepoFieldsInServiceAnalyzer, analyzers[0].Name)
}
