package glint

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestGlintNolint(t *testing.T) {
	t.Parallel()

	analyzer, err := newGlintNolintAnalyzer()
	require.NoError(t, err)

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, analyzer, "glintnolint")
}

// An empty reason cannot be exercised by a fixture because analysistest's
// trailing "// want" comment would itself become the reason.
func TestGlintNolintExplanationMissingReason(t *testing.T) {
	t.Parallel()

	validNames := map[string]struct{}{noAnonymousDeferAnalyzer: {}}

	require.Equal(t, `add a reason after "noanonymousdefer:" in the //nolint:glint explanation`, glintNolintExplanationProblem("noanonymousdefer:", validNames, noAnonymousDeferAnalyzer))
	require.Equal(t, `add a reason after "noanonymousdefer:" in the //nolint:glint explanation`, glintNolintExplanationProblem("noanonymousdefer:   ", validNames, noAnonymousDeferAnalyzer))
	require.Empty(t, glintNolintExplanationProblem("noanonymousdefer: why", validNames, noAnonymousDeferAnalyzer))
}

func TestGlintNolintExplanationMissing(t *testing.T) {
	t.Parallel()

	validNames := map[string]struct{}{noAnonymousDeferAnalyzer: {}}

	require.Equal(t, `start the //nolint:glint explanation with the suppressed glint analyzer name(s) and a colon, e.g. "//nolint:glint // notestingrawsql: <reason>"; got ""`, glintNolintExplanationProblem("", validNames, noAnonymousDeferAnalyzer))
}

func TestNewNolintBuildsAnalyzer(t *testing.T) {
	t.Parallel()

	p, err := NewNolint(nil)
	require.NoError(t, err)

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Len(t, analyzers, 1)
	require.Equal(t, glintNolintAnalyzer, analyzers[0].Name)
}

func TestNewNolintDisabled(t *testing.T) {
	t.Parallel()

	p, err := NewNolint(map[string]any{"disabled": true})
	require.NoError(t, err)

	analyzers, err := p.BuildAnalyzers()
	require.NoError(t, err)
	require.Empty(t, analyzers)
}
