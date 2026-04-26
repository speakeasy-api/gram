package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestNoAnonymousDefer(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newNoAnonymousDeferAnalyzer(noAnonymousDeferSettings{}), "noanonymousdefer")
}

func TestNoAnonymousDeferCustomMessage(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(
		t,
		testdata,
		newNoAnonymousDeferAnalyzer(noAnonymousDeferSettings{Message: "use a named deferred helper instead"}),
		"noanonymousdefercustommessage",
	)
}

func TestBuildAnalyzersSkipsDisabledNoAnonymousDefer(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Empty(t, analyzers)
}
