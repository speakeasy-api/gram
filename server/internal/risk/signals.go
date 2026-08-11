package risk

import (
	"math"
	"time"

	"github.com/speakeasy-api/gram/server/internal/risk/categories"
)

// Signal scoring: deterministic heuristics that place each rule-level signal
// on the same 0.1-10 severity scale the rest of the risk surfaces use (CVSS
//-style bands, see severityForScore). The base weight comes from the rule's
// category; confidence, volume, user spread, recency, and growth nudge it up
// or down. All weights live here so product tuning is a one-file change.

// signalCategoryBaseWeight is the category's starting severity on the 10
// scale before per-signal adjustments.
var signalCategoryBaseWeight = map[categories.Category]float64{
	categories.CategorySecrets:         8.5,
	categories.CategoryHealthcare:      7.5,
	categories.CategoryFinancial:       7.5,
	categories.CategoryGovernmentIDs:   7.0,
	categories.CategoryPromptInjection: 7.0,
	categories.CategoryDestructiveTool: 7.0,
	categories.CategoryCLIDestructive:  7.0,
	categories.CategoryAccountIdentity: 6.5,
	categories.CategoryShadowMCP:       6.0,
	categories.CategoryPII:             5.5,
	categories.CategoryPromptPolicy:    5.0,
	categories.CategoryOffPolicy:       4.5,
	categories.CategoryCustom:          5.0,
}

// signalDefaultBaseWeight applies to findings whose category is unknown
// (pre-classifier rows or future categories).
const signalDefaultBaseWeight = 5.0

// signalScoreInputs are the aggregate facts one score is computed from.
type signalScoreInputs struct {
	category      string
	avgConfidence float64
	findings      uint64
	users         uint64
	// lastSeen and windowEnd drive the recency bonus; zero lastSeen skips it.
	lastSeen  time.Time
	windowEnd time.Time
	// previousFindings drives the growth bonus; zero skips it.
	previousFindings uint64
}

// signalScore maps aggregate facts to a 0.1-10 severity score:
// base weight scaled by detector confidence (a 0-confidence signal keeps 70%
// of its base), plus bounded bonuses for volume (log-scaled), user spread
// (sqrt-scaled), last-24h activity, and >=50% window-over-window growth.
// Clamped to [0.5, 9.9] so a heuristic score never claims the extremes, and
// rounded to one decimal to match the CVSS-style display format.
func signalScore(in signalScoreInputs) float64 {
	base, ok := signalCategoryBaseWeight[categories.Category(in.category)]
	if !ok {
		base = signalDefaultBaseWeight
	}

	confidence := math.Max(0, math.Min(1, in.avgConfidence))
	score := base * (0.7 + 0.3*confidence)

	score += math.Min(1.2, 0.5*math.Log10(1+float64(in.findings)))
	score += math.Min(1.5, 0.3*math.Sqrt(float64(in.users)))

	if !in.lastSeen.IsZero() && !in.windowEnd.IsZero() && in.windowEnd.Sub(in.lastSeen) <= 24*time.Hour {
		score += 0.6
	}
	if in.previousFindings > 0 && float64(in.findings) >= 1.5*float64(in.previousFindings) {
		score += 0.5
	}

	return roundScore(math.Max(0.5, math.Min(9.9, score)))
}

// severityForScore mirrors the dashboard's CVSS-style bands
// (client/dashboard/src/pages/security/risk-utils.ts): the two must not
// drift or the badge and the score pill would disagree.
func severityForScore(score float64) string {
	switch {
	case score >= 9.0:
		return "critical"
	case score >= 7.0:
		return "high"
	case score >= 4.0:
		return "medium"
	default:
		return "low"
	}
}

// orgRiskScore blends per-signal scores into one 0-10 headline number: the
// worst signal dominates (50%), the mean of the top three keeps a lone
// outlier from saturating it alone (30%), and total finding volume
// contributes the rest (20%, log-scaled). No signals yields zero.
func orgRiskScore(signalScores []float64, totalFindings uint64) float64 {
	if len(signalScores) == 0 {
		return 0
	}

	top := topScoresDescending(signalScores, 3)
	maxScore := top[0]
	var topSum float64
	for _, s := range top {
		topSum += s
	}
	meanTop := topSum / float64(len(top))

	volume := math.Min(10, 1.2*math.Log10(1+float64(totalFindings)))

	score := 0.5*maxScore + 0.3*meanTop + 0.2*volume
	return roundScore(math.Max(0, math.Min(10, score)))
}

// topScoresDescending returns the n largest scores, largest first, without
// mutating the input.
func topScoresDescending(scores []float64, n int) []float64 {
	top := make([]float64, 0, n)
	for _, s := range scores {
		if len(top) < n {
			top = append(top, s)
			for i := len(top) - 1; i > 0 && top[i] > top[i-1]; i-- {
				top[i], top[i-1] = top[i-1], top[i]
			}
			continue
		}
		if s <= top[n-1] {
			continue
		}
		top[n-1] = s
		for i := n - 1; i > 0 && top[i] > top[i-1]; i-- {
			top[i], top[i-1] = top[i-1], top[i]
		}
	}
	return top
}

func roundScore(v float64) float64 {
	return math.Round(v*10) / 10
}
