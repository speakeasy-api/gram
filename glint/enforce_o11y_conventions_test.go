package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestEnforceO11yConventions(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newEnforceO11yConventionsAnalyzer(enforceO11yConventionsSettings{}), "enforceo11yconventions")
}

func TestEnforceO11yConventionsCustomMessage(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(
		t,
		testdata,
		newEnforceO11yConventionsAnalyzer(enforceO11yConventionsSettings{Message: "use attr.Slog* helpers instead"}),
		"enforceo11yconventionscustommessage",
	)
}

func TestBuildAnalyzersSkipsDisabledEnforceO11yConventions(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Empty(t, analyzers)
}
