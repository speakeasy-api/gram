package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestRpcEndpointFormat(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newRpcEndpointFormatAnalyzer(rpcEndpointFormatSettings{}), "rpcendpointformat")
}

func TestBuildAnalyzersSkipsDisabledRpcEndpointFormat(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()
	p.settings.Rules.RpcEndpointFormat.Disabled = false

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Len(t, analyzers, 1)
	require.Equal(t, rpcEndpointFormatAnalyzer, analyzers[0].Name)
}
