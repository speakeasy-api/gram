package risk

import (
	"math"

	"github.com/speakeasy-api/gram/server/internal/risk/categories"
)

// Signal scoring: a signal's score IS the matched policy's configured 0.1-10
// score — the operator's own severity definition, on the same CVSS-style
// bands the rest of the risk surfaces use (see severityForScore). Category
// weights only cover findings with no policy attribution. A richer
// evidence-weighted formula was prototyped and parked for review; see
// docs/risk_scores/formula.md in the internal docs repo.

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

// signalScore is the matched policy's configured 0.1-10 score, verbatim.
// policyScore <= 0 means no policy attribution (pre-policy rows), which
// falls back to the rule category's fixed weight. Rounded to one decimal to
// match the CVSS-style display format.
func signalScore(policyScore float64, category string) float64 {
	if policyScore > 0 {
		return roundScore(policyScore)
	}
	base, ok := signalCategoryBaseWeight[categories.Category(category)]
	if !ok {
		base = signalDefaultBaseWeight
	}
	return roundScore(base)
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
