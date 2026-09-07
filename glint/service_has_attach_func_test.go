package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestServiceHasAttachFuncGood(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newServiceHasAttachFuncAnalyzer(serviceHasAttachFuncSettings{}), "github.com/speakeasy-api/gram/server/internal/serviceannotation")
}

func TestServiceHasAttachFuncMissing(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newServiceHasAttachFuncAnalyzer(serviceHasAttachFuncSettings{}), "github.com/speakeasy-api/gram/server/internal/serviceannotationmissingattach")
}

func TestBuildAnalyzersSkipsDisabledServiceHasAttachFunc(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()
	p.settings.Rules.ServiceHasAttachFunc.Disabled = false

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Len(t, analyzers, 1)
	require.Equal(t, serviceHasAttachFuncAnalyzer, analyzers[0].Name)
}
