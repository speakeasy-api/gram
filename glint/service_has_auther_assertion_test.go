package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestServiceHasAutherAssertionGood(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newServiceHasAutherAssertionAnalyzer(serviceHasAutherAssertionSettings{}), "serviceannotation")
}

func TestServiceHasAutherAssertionMissing(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newServiceHasAutherAssertionAnalyzer(serviceHasAutherAssertionSettings{}), "serviceannotationmissingauth")
}

func TestBuildAnalyzersSkipsDisabledServiceHasAutherAssertion(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()
	p.settings.Rules.ServiceHasAutherAssertion.Disabled = false

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Len(t, analyzers, 1)
	require.Equal(t, serviceHasAutherAssertionAnalyzer, analyzers[0].Name)
}
