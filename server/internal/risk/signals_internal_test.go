package risk

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSignalScore_GoldenValues(t *testing.T) {
	t.Parallel()

	windowEnd := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	// Full-confidence secrets signal, active in the last 24h, tripled
	// window-over-window: base 8.5 + volume 0.30 + spread 0.42 + recency 0.6 +
	// growth 0.5 clamps at the 9.9 ceiling.
	require.InDelta(t, 9.9, signalScore(signalScoreInputs{
		category:         "secrets",
		avgConfidence:    1,
		findings:         3,
		users:            2,
		lastSeen:         windowEnd.Add(-2 * time.Hour),
		windowEnd:        windowEnd,
		previousFindings: 1,
	}), 0.001)

	// Quiet single-user PII signal with no recency or growth bonus:
	// 5.5 + 0.5*log10(4) + 0.3*sqrt(1) = 6.101 -> 6.1.
	require.InDelta(t, 6.1, signalScore(signalScoreInputs{
		category:         "pii",
		avgConfidence:    1,
		findings:         3,
		users:            1,
		lastSeen:         windowEnd.Add(-30 * time.Hour),
		windowEnd:        windowEnd,
		previousFindings: 0,
	}), 0.001)

	// Zero confidence keeps 70% of the base weight: 4.5*0.7 = 3.15, plus
	// volume 0.5*log10(2) = 0.151 -> 3.3.
	require.InDelta(t, 3.3, signalScore(signalScoreInputs{
		category:         "off_policy",
		avgConfidence:    0,
		findings:         1,
		users:            0,
		lastSeen:         time.Time{},
		windowEnd:        windowEnd,
		previousFindings: 0,
	}), 0.001)

	// Unknown categories fall back to the default base weight (5.0).
	require.InDelta(t, 5.0*(0.7+0.3*0.5)+0.5*0.30103+0.3, signalScore(signalScoreInputs{
		category:         "not_a_category",
		avgConfidence:    0.5,
		findings:         1,
		users:            1,
		lastSeen:         time.Time{},
		windowEnd:        windowEnd,
		previousFindings: 0,
	}), 0.05)
}

func TestSignalScore_FloorAndCeiling(t *testing.T) {
	t.Parallel()

	// The floor holds even for an implausibly weak signal.
	require.GreaterOrEqual(t, signalScore(signalScoreInputs{
		category:         "off_policy",
		avgConfidence:    0,
		findings:         0,
		users:            0,
		lastSeen:         time.Time{},
		windowEnd:        time.Time{},
		previousFindings: 0,
	}), 0.5)

	// The ceiling holds for a maxed-out signal.
	require.LessOrEqual(t, signalScore(signalScoreInputs{
		category:         "secrets",
		avgConfidence:    1,
		findings:         100000,
		users:            10000,
		lastSeen:         time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		windowEnd:        time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		previousFindings: 1,
	}), 9.9)
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
