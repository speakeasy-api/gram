package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners"
)

func TestLoadCorpusPreservesTrajectoryTwinSemantics(t *testing.T) {
	t.Parallel()

	corpusDir := filepath.Join("..", "..", "internal", "scanners", "promptinjection", "testdata", "prompt_injection")
	corpus, err := loadCorpus(corpusDir, "")
	require.NoError(t, err)

	twins := filterSources(corpus, "trajectory_twins")
	require.Len(t, twins, 72)
	byText := make(map[string][]labeledCase)
	for _, row := range twins {
		byText[row.Text] = append(byText[row.Text], row)
	}
	require.Len(t, byText["cat ~/.config/example/credentials"], 2)
}

func TestSummariesCaptureStabilityAndDistributions(t *testing.T) {
	t.Parallel()

	positive := []scanners.Finding{{RuleID: "pi", Tags: []string{"semantic-typed"}}}
	corpus := []labeledCase{
		{ID: "stable-fp", Label: "benign", Source: "test"},
		{ID: "stable-negative", Label: "benign", Source: "test"},
		{ID: "flip", Label: "benign", Source: "test"},
	}
	runs := [][][]scanners.Finding{
		{positive, nil, positive},
		{positive, nil, nil},
		{positive, nil, positive},
	}

	require.Equal(t, stabilitySummary{
		Repeats: 3, StablePositive: 1, StableNegative: 1, Flipped: 1,
		FlipRate: 1.0 / 3.0, StableFalsePositives: 1, FlippedBenign: 1,
		FlipsAndStableFalseCore: []stabilityRow{
			{ID: "stable-fp", Source: "test", Label: "benign", PositiveRuns: 3, Outcome: "stable_false_positive"},
			{ID: "flip", Source: "test", Label: "benign", PositiveRuns: 2, Outcome: "flipped"},
		},
	}, summarizeStability(corpus, runs))

	dist := describeDistribution([]float64{3, 1, 2})
	require.InDelta(t, 1.0, dist.Min, 0.0001)
	require.InDelta(t, 2.0, dist.Median, 0.0001)
	require.InDelta(t, 3.0, dist.Max, 0.0001)
	require.InDelta(t, 2.0, dist.Mean, 0.0001)
	require.InDelta(t, 0.816496, dist.StdDev, 0.0001)
}

func TestRecallGateExcludesKnownGapsAndOutOfTaxonomyRows(t *testing.T) {
	t.Parallel()

	yes := true
	corpus := []labeledCase{
		{ID: "hit", Label: "malicious", Source: "trajectory_twins", DirectivePresent: &yes},
		{ID: "miss", Label: "malicious", Source: "trajectory_twins", DirectivePresent: &yes},
		{ID: "gap", Label: "malicious", Source: "trajectory_twins", DirectivePresent: &yes, KnownGap: "AGE-3048"},
		{ID: "external-label", Label: "malicious", Source: "deepset"},
	}
	findings := [][]scanners.Finding{{{RuleID: "pi"}}, nil, nil, nil}

	got := summarizeRecallGate(corpus, findings)
	require.Equal(t, counts{TP: 1, FP: 0, TN: 0, FN: 1}, got.Counts)
	require.InDelta(t, 0.5, got.Recall, 0.0001)
	require.Equal(t, 1, got.Excluded)
	require.Len(t, got.BySource, 1)
}

func TestRecallFloorsUseEveryRunAndSource(t *testing.T) {
	t.Parallel()

	overallFloor := 0.9
	fl := floors{
		FPRateMax:         0.006,
		RecallFloor:       &overallFloor,
		RecallBySourceMin: map[string]float64{"trajectory_twins": 0.8},
		LastUpdated:       "2026-07-22",
		LastUpdatedBy:     "test",
		Notes:             "test",
	}
	passing := recallGateSummary{
		Scope: "test", Counts: counts{TP: 9, FP: 0, TN: 0, FN: 1}, Recall: 0.9,
		BySource: []sourceSummary{{
			Source: "trajectory_twins", Counts: counts{TP: 8, FP: 0, TN: 0, FN: 2},
			Metrics: metricsBlock{Precision: 1, Recall: 0.8, F1: 0.888, Accuracy: 0.8, FPRate: 0},
		}},
		Excluded: 0,
	}
	require.NoError(t, checkRecallFloors(fl, []recallGateSummary{passing}))

	failingOverall := passing
	failingOverall.Recall = 0.89
	require.ErrorContains(t, checkRecallFloors(fl, []recallGateSummary{passing, failingOverall}), "run 2")

	failingSource := passing
	failingSource.BySource = []sourceSummary{{
		Source: "trajectory_twins", Counts: counts{TP: 7, FP: 0, TN: 0, FN: 3},
		Metrics: metricsBlock{Precision: 1, Recall: 0.7, F1: 0.824, Accuracy: 0.7, FPRate: 0},
	}}
	require.ErrorContains(t, checkRecallFloors(fl, []recallGateSummary{failingSource}), "trajectory_twins")

	partialFloors := fl
	partialFloors.RecallBySourceMin = map[string]float64{"trajectory_twins": 0.8, "mutations": 0.9}
	partialBelowOverall := passing
	partialBelowOverall.Recall = 0.85
	require.NoError(t, checkRecallFloors(partialFloors, []recallGateSummary{partialBelowOverall}), "partial source runs use their source floor, not the full-suite overall floor")
}

func TestResolveDirectivePresenceFromMutationSeed(t *testing.T) {
	t.Parallel()

	yes := true
	corpus := []labeledCase{
		{ID: "seed", DirectivePresent: &yes},
		{ID: "mutation", SeedID: "seed"},
	}
	require.NoError(t, resolveDirectivePresence(corpus))
	require.NotNil(t, corpus[1].DirectivePresent)
	require.True(t, *corpus[1].DirectivePresent)

	unknown := []labeledCase{{ID: "mutation", SeedID: "missing"}}
	require.ErrorContains(t, resolveDirectivePresence(unknown), "unknown seed_id")
	ambiguous := []labeledCase{{ID: "seed"}, {ID: "seed"}, {ID: "mutation", SeedID: "seed"}}
	require.ErrorContains(t, resolveDirectivePresence(ambiguous), "ambiguous seed_id")

	cycle := []labeledCase{{ID: "a", SeedID: "b"}, {ID: "b", SeedID: "a"}}
	require.ErrorContains(t, resolveDirectivePresence(cycle), "seed cycle")
}

func TestSummarizeEvaluation(t *testing.T) {
	t.Parallel()

	stats := summarizeEvaluation([]decisionObservation{{
		Calls: []callObservation{
			{Latency: 11 * time.Second, PromptTokens: 10, CompletionTokens: 2, CostUSD: 0.01},
			{Latency: time.Second, Err: context.DeadlineExceeded},
		},
		Latency: 11 * time.Second,
	}})
	require.Equal(t, 2, stats.PhysicalCalls)
	require.Equal(t, 1, stats.CallsOver10Seconds)
	require.Equal(t, 1, stats.Timeouts)
	require.Equal(t, 10, stats.PromptTokens)
}

func TestCommittedRecallFixturesUseReviewedDirectiveTaxonomy(t *testing.T) {
	t.Parallel()

	corpusDir := filepath.Join("..", "..", "internal", "scanners", "promptinjection", "testdata", "prompt_injection")
	corpus, err := loadCorpus(corpusDir, "")
	require.NoError(t, err)

	type taxonomyCounts struct {
		included int
		excluded int
	}
	want := map[string]taxonomyCounts{
		"adversarial_fable": {included: 41, excluded: 9},
		"adversarial_codex": {included: 42, excluded: 8},
		"litellm_extended":  {included: 50, excluded: 1},
		"mutations":         {included: 65, excluded: 5},
	}
	got := make(map[string]taxonomyCounts, len(want))
	bySourceID := make(map[string]labeledCase)
	for _, row := range corpus {
		source := recallGateSource(row.Source)
		if _, audited := want[source]; !audited {
			continue
		}
		require.NotNil(t, row.DirectivePresent, "%s/%s must have explicit or inherited taxonomy", source, row.ID)
		counts := got[source]
		if directivePresentForGate(row) {
			counts.included++
		} else {
			counts.excluded++
		}
		got[source] = counts
		bySourceID[source+"/"+row.ID] = row
	}
	require.Equal(t, want, got)

	// Raw credential/network/destructive tool arguments are action-risk data,
	// not PI without an operational directive aimed at the guarded agent.
	require.False(t, directivePresentForGate(bySourceID["adversarial_fable/adv.exfil_in_toolargs.001"]))
	require.False(t, directivePresentForGate(bySourceID["adversarial_codex/adv.exfil_in_toolargs.002"]))
	// A planted instruction in an agent instruction file remains in taxonomy.
	require.True(t, directivePresentForGate(bySourceID["adversarial_fable/adv.exfil_in_toolargs.003"]))
	// Mutations inherit the reviewed semantics of their LiteLLM seeds.
	require.False(t, directivePresentForGate(bySourceID["mutations/mutation.delim02.base64_wrap"]))
	require.True(t, directivePresentForGate(bySourceID["mutations/mutation.tool01.base64_wrap"]))

	unannotated := labeledCase{ID: "unreviewed", Label: "malicious", Source: "adversarial_fable"}
	require.False(t, directivePresentForGate(unannotated), "source membership must never imply taxonomy inclusion")
}
