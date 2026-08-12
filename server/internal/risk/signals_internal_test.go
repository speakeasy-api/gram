package risk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSignalScore_GoldenValues(t *testing.T) {
	t.Parallel()

	// A matched policy's configured score is the signal score, verbatim.
	require.InDelta(t, 2.0, signalScore(2.0, "secrets"), 0.001)
	require.InDelta(t, 9.6, signalScore(9.6, "pii"), 0.001)

	// No policy attribution falls back to the category weight, verbatim.
	require.InDelta(t, 8.5, signalScore(0, "secrets"), 0.001)
	require.InDelta(t, 5.5, signalScore(0, "pii"), 0.001)
	require.InDelta(t, 4.5, signalScore(0, "off_policy"), 0.001)

	// Unknown categories fall back to the default weight.
	require.InDelta(t, 5.0, signalScore(0, "not_a_category"), 0.001)

	// Scores are normalized to one decimal.
	require.InDelta(t, 7.3, signalScore(7.25001, "secrets"), 0.001)
}

func TestSeverityForScore_BandsMatchDashboard(t *testing.T) {
	t.Parallel()

	// Mirrors SEVERITY_BANDS in client/dashboard/src/pages/security/risk-utils.ts.
	require.Equal(t, "critical", severityForScore(9.0))
	require.Equal(t, "critical", severityForScore(9.9))
	require.Equal(t, "high", severityForScore(7.0))
	require.Equal(t, "high", severityForScore(8.9))
	require.Equal(t, "medium", severityForScore(4.0))
	require.Equal(t, "medium", severityForScore(6.9))
	require.Equal(t, "low", severityForScore(3.9))
	require.Equal(t, "low", severityForScore(0.5))
}

func TestOrgRiskScore(t *testing.T) {
	t.Parallel()

	require.InDelta(t, 0, orgRiskScore(nil, 0), 0.001)
	require.InDelta(t, 0, orgRiskScore([]float64{}, 100), 0.001)

	// Single signal: 0.5*s + 0.3*s + 0.2*volume.
	single := orgRiskScore([]float64{8.0}, 9)
	require.InDelta(t, 0.5*8+0.3*8+0.2*1.2, single, 0.05)

	// The worst signal dominates: adding low signals to one critical one moves
	// the blend down only through the top-3 mean.
	high := orgRiskScore([]float64{9.9}, 10)
	blended := orgRiskScore([]float64{9.9, 1.0, 1.0, 1.0}, 10)
	require.Greater(t, high, blended)
	require.Greater(t, blended, 5.0)

	// Bounded to 10 regardless of volume.
	require.LessOrEqual(t, orgRiskScore([]float64{9.9, 9.9, 9.9}, 1_000_000_000), 10.0)
}

func TestTopScoresDescending(t *testing.T) {
	t.Parallel()

	require.Equal(t, []float64{9, 7, 5}, topScoresDescending([]float64{5, 9, 1, 7, 3}, 3))
	require.Equal(t, []float64{4, 2}, topScoresDescending([]float64{2, 4}, 3))
	require.Equal(t, []float64{8}, topScoresDescending([]float64{8, 8, 8}, 1))
}
