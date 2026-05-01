package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestNoSqlErrNoRows(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newNoSqlErrNoRowsAnalyzer(noSqlErrNoRowsSettings{}), "nosqlerrnorows")
}

func TestNoSqlErrNoRowsSuggestedFix(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.RunWithSuggestedFixes(t, testdata, newNoSqlErrNoRowsAnalyzer(noSqlErrNoRowsSettings{}), "nosqlerrnorows")
}

func TestBuildAnalyzersSkipsDisabledNoSqlErrNoRows(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Empty(t, analyzers)
}
