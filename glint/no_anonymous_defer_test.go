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

	p := &plugin{
		settings: settings{
			Rules: ruleSettings{
				NoAnonymousDefer:           noAnonymousDeferSettings{Disabled: true},
				ServiceHasServiceAssertion: serviceHasServiceAssertionSettings{Disabled: true},
				ServiceHasAutherAssertion:  serviceHasAutherAssertionSettings{Disabled: true},
				ServiceHasAttachFunc:       serviceHasAttachFuncSettings{Disabled: true},
				NoRepoFieldsInService:      noRepoFieldsInServiceSettings{Disabled: true},
			},
		},
	}

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Empty(t, analyzers)
}
