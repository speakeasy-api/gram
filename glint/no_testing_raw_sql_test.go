package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestNoTestingRawSql(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newNoTestingRawSqlAnalyzer(noTestingRawSqlSettings{}), "notestingrawsql")
}

func TestBuildAnalyzersSkipsDisabledNoTestingRawSql(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()
	p.settings.Rules.NoTestingRawSql.Disabled = false

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Len(t, analyzers, 1)
	require.Equal(t, noTestingRawSqlAnalyzer, analyzers[0].Name)
}
