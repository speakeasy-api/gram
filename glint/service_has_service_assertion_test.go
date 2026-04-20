package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestServiceHasServiceAssertionGood(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newServiceHasServiceAssertionAnalyzer(serviceHasServiceAssertionSettings{}), "serviceannotation")
}

func TestServiceHasServiceAssertionMissing(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newServiceHasServiceAssertionAnalyzer(serviceHasServiceAssertionSettings{}), "serviceannotationmissingimpl")
}

func TestBuildAnalyzersSkipsDisabledServiceHasServiceAssertion(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()
	p.settings.Rules.ServiceHasServiceAssertion.Disabled = false

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Len(t, analyzers, 1)
	require.Equal(t, serviceHasServiceAssertionAnalyzer, analyzers[0].Name)
}
