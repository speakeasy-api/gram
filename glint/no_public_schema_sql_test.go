package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestNoPublicSchemaSql(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newNoPublicSchemaSqlAnalyzer(noPublicSchemaSqlSettings{}), "nopublicschemasql")
}

func TestBuildAnalyzersSkipsDisabledNoPublicSchemaSql(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()
	p.settings.Rules.NoPublicSchemaSql.Disabled = false

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Len(t, analyzers, 1)
	require.Equal(t, noPublicSchemaSqlAnalyzer, analyzers[0].Name)
}
