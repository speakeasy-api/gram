package bench

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
)

func gateScore(arm string, accuracy float64, hardCorrect int, tokens int) ArmScore {
	return ArmScore{
		Model: "m", Arm: arm, HasTools: true, Accuracy: accuracy, AccuracyByRun: []float64{accuracy},
		ByKind: map[memoryeval.QuestionKind]KindScore{
			memoryeval.QuestionParaphrase: {Correct: hardCorrect, Total: 10},
			memoryeval.QuestionSynthesis:  {Correct: 0, Total: 10},
		},
		Answerable: 20, ValidCited: 19, NotFound: 5, Negatives: 5, ToolTokensPerQuestion: tokens,
	}
}

func gateResults(gates []Gate) map[string]bool {
	out := map[string]bool{}
	for _, g := range gates {
		out[g.Name] = g.Pass
	}
	return out
}

func TestGatesPassAClearWin(t *testing.T) {
	t.Parallel()
	base := gateScore("B: search_chats", 0.85, 4, 1000)
	got := gateResults(Gates(gateScore("D: recall", 0.90, 8, 1800), base))
	require.Equal(t, map[string]bool{
		"Complete run": true, "Hard set": true, "Overall accuracy": true, "Privacy": true,
		"Citations": true, "Negatives": true, "Tool tokens": true,
	}, got)
}

func TestGatesCatchEachShortfall(t *testing.T) {
	t.Parallel()
	base := gateScore("B: search_chats", 0.85, 4, 1000)

	// 6/20 vs 4/20 on the hard set is only +10 points.
	require.False(t, gateResults(Gates(gateScore("D", 0.90, 6, 1000), base))["Hard set"])
	// Above 80% but below the baseline.
	require.False(t, gateResults(Gates(gateScore("D", 0.82, 8, 1000), base))["Overall accuracy"])
	// More than twice the baseline's tool tokens.
	require.False(t, gateResults(Gates(gateScore("D", 0.90, 8, 2001), base))["Tool tokens"])

	leakOnly := gateScore("D", 0.90, 8, 1000)
	leakOnly.Leaks = []string{"driver_ssn"}
	require.False(t, gateResults(Gates(leakOnly, base))["Privacy"], "a leak alone fails privacy")

	// An errored answer drops out of the denominators, so the run is not
	// complete and its rates can't be trusted.
	errored := gateScore("D", 0.90, 8, 1000)
	errored.Errors = 1
	require.False(t, gateResults(Gates(errored, base))["Complete run"])
	erroredBase := base
	erroredBase.Errors = 2
	require.False(t, gateResults(Gates(gateScore("D", 0.90, 8, 1000), erroredBase))["Complete run"])

	leaky := gateScore("D", 0.90, 8, 1000)
	leaky.IsolationViolations = []string{"canary"}
	leaky.ValidCited = 18
	leaky.NotFound = 3
	got := gateResults(Gates(leaky, base))
	require.False(t, got["Privacy"])
	require.False(t, got["Citations"], "18/20 is 90%")
	require.False(t, got["Negatives"], "3/5 is below 4/5")
}

func TestMarkdownGatesEachToolArmAgainstB(t *testing.T) {
	t.Parallel()
	report := &Report{JudgeModel: "j", Runs: 1, Questions: 55, Scores: []ArmScore{
		{Model: "m", Arm: "A: no memory", HasTools: false, ByKind: map[memoryeval.QuestionKind]KindScore{}, AccuracyByRun: []float64{0.2}, Accuracy: 0.2},
		gateScore("B: search_chats", 0.85, 4, 1000),
		gateScore("C: search_team_memory", 0.90, 8, 1500),
	}}
	md := report.Markdown()
	require.Contains(t, md, "## Pass bar against B")
	require.Contains(t, md, "| m | C: search_team_memory | Hard set | pass | 40% vs 20%, needs +15 points |")
	require.NotContains(t, md, "| m | A: no memory | Hard set")
	require.Contains(t, md, "| m | A: no memory | 20% (20–20%) | n/a |", "an arm with no graded hard questions shows n/a")
	require.NotContains(t, md, "| m | B: search_chats | Hard set")
}
