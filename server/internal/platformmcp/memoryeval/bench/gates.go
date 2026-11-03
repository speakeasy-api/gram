package bench

import (
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
)

// Pass-bar thresholds an arm must meet against the search_chats baseline.
// The recall block size gate (under 8k tokens) is asserted by the recall
// probe, not here, because ArmScore does not carry it.
const (
	gateHardLiftPoints     = 15
	gateMinAccuracy        = 0.80
	gateMinValidCitations  = 0.95
	gateMinNotFound        = 0.80
	gateMaxToolTokenFactor = 2
)

// baselineArmPrefix names the arm every other arm is gated against.
const baselineArmPrefix = "B:"

// Gate is one pass-bar check and its outcome.
type Gate struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail"`
}

// Hard sums the paraphrase and synthesis questions, which share no keywords
// with their sources.
func (s ArmScore) Hard() KindScore {
	p, y := s.ByKind[memoryeval.QuestionParaphrase], s.ByKind[memoryeval.QuestionSynthesis]
	return KindScore{Correct: p.Correct + y.Correct, Total: p.Total + y.Total}
}

func (k KindScore) rate() float64 {
	return float64(k.Correct) / float64(max(k.Total, 1))
}

// Gates checks candidate against baseline on every pass-bar gate.
func Gates(candidate, baseline ArmScore) []Gate {
	hard, baseHard := candidate.Hard().rate(), baseline.Hard().rate()
	citations := float64(candidate.ValidCited) / float64(max(candidate.Answerable, 1))
	notFound := float64(candidate.NotFound) / float64(max(candidate.Negatives, 1))
	return []Gate{
		{
			Name:   "Hard set",
			Pass:   100*(hard-baseHard) >= gateHardLiftPoints,
			Detail: fmt.Sprintf("%.0f%% vs %.0f%%, needs +%d points", 100*hard, 100*baseHard, gateHardLiftPoints),
		},
		{
			Name:   "Overall accuracy",
			Pass:   candidate.Accuracy >= gateMinAccuracy && candidate.Accuracy >= baseline.Accuracy,
			Detail: fmt.Sprintf("%.0f%% vs %.0f%%, needs %.0f%% and no lower", 100*candidate.Accuracy, 100*baseline.Accuracy, 100*gateMinAccuracy),
		},
		{
			Name:   "Privacy",
			Pass:   len(candidate.Leaks) == 0 && len(candidate.IsolationViolations) == 0,
			Detail: fmt.Sprintf("%d leaks, %d isolation violations", len(candidate.Leaks), len(candidate.IsolationViolations)),
		},
		{
			Name:   "Citations",
			Pass:   citations >= gateMinValidCitations,
			Detail: fmt.Sprintf("%.0f%% valid, needs %.0f%%", 100*citations, 100*gateMinValidCitations),
		},
		{
			Name:   "Negatives",
			Pass:   notFound >= gateMinNotFound,
			Detail: fmt.Sprintf("%d/%d not found", candidate.NotFound, candidate.Negatives),
		},
		{
			Name:   "Tool tokens",
			Pass:   candidate.ToolTokensPerQuestion <= gateMaxToolTokenFactor*baseline.ToolTokensPerQuestion,
			Detail: fmt.Sprintf("%d vs %d per question, at most %dx", candidate.ToolTokensPerQuestion, baseline.ToolTokensPerQuestion, gateMaxToolTokenFactor),
		},
	}
}

// gatesMarkdown renders the pass bar for every tool arm against the same
// model's baseline arm, or nothing when the run has no baseline.
func (r *Report) gatesMarkdown() string {
	baselines := map[string]ArmScore{}
	for _, s := range r.Scores {
		if strings.HasPrefix(s.Arm, baselineArmPrefix) {
			baselines[s.Model] = s
		}
	}
	var b strings.Builder
	for _, s := range r.Scores {
		base, ok := baselines[s.Model]
		if !ok || !s.HasTools || strings.HasPrefix(s.Arm, baselineArmPrefix) {
			continue
		}
		if b.Len() == 0 {
			b.WriteString("\n## Pass bar against B\n\n| Model | Arm | Gate | Result | Detail |\n| --- | --- | --- | --- | --- |\n")
		}
		for _, g := range Gates(s, base) {
			result := "fail"
			if g.Pass {
				result = "pass"
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", s.Model, s.Arm, g.Name, result, g.Detail)
		}
	}
	return b.String()
}
