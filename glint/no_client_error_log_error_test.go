package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestNoClientErrorLogError(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analyzer := newNoClientErrorLogErrorAnalyzer(noClientErrorLogErrorSettings{
		Codes: []string{"CodeUnauthorized", "CodeNotFound"},
	})
	analysistest.Run(t, testdata, analyzer, "github.com/speakeasy-api/gram/server/internal/noclienterrorlogerror")
}

func TestNoClientErrorLogErrorAllowsKnownCodes(t *testing.T) {
	t.Parallel()

	enabled, err := clientErrorCodeSet([]string{"CodeUnauthorized", "CodeNotFound", "CodeBadRequest"})
	require.NoError(t, err)
	require.Len(t, enabled, 3)
}

func TestNoClientErrorLogErrorRejectsUnknownCode(t *testing.T) {
	t.Parallel()

	_, err := clientErrorCodeSet([]string{"CodeUnauthorized", "CodeNonsense"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "CodeNonsense")
}

func TestNoClientErrorLogErrorRejectsNonClientFaultCode(t *testing.T) {
	t.Parallel()

	// A 5xx / server-fault code must not be accepted: it is not eligible for
	// demotion off error level.
	_, err := clientErrorCodeSet([]string{"CodeUnexpected"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "CodeUnexpected")
}

func TestBuildAnalyzersSkipsDisabledNoClientErrorLogError(t *testing.T) {
	t.Parallel()

	p := disabledAllRulesPlugin()
	p.settings.Rules.NoClientErrorLogError.Disabled = false

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Len(t, analyzers, 1)
	require.Equal(t, noClientErrorLogErrorAnalyzer, analyzers[0].Name)
}
