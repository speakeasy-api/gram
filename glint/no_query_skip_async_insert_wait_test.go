package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestNoQuerySkipAsyncInsertWait(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newNoQuerySkipAsyncInsertWaitAnalyzer(noQuerySkipAsyncInsertWaitSettings{}), "noqueryskipasyncinsertwait")
}

func TestBuildAnalyzersSkipsDisabledNoQuerySkipAsyncInsertWait(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()
	p.settings.Rules.NoQuerySkipAsyncInsertWait.Disabled = false

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Len(t, analyzers, 1)
	require.Equal(t, noQuerySkipAsyncInsertWaitAnalyzer, analyzers[0].Name)
}
