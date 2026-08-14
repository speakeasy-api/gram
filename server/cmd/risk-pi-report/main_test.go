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
	require.Len(t, twins, 98)
	byText := make(map[string][]labeledCase)
	for _, row := range twins {
		byText[row.Text] = append(byText[row.Text], row)
	}
	require.Len(t, byText["cat ~/.config/example/credentials"], 4)
}

func TestSummariesCaptureStabilityAndDistributions(t *testing.T) {
	t.Parallel()

	positive := []scanners.Finding{{RuleID: "pi", Tags: []string{"semantic-typed"}}}
	no := false
	corpus := []labeledCase{
		{ID: "stable-fp", DirectivePresent: &no, Source: "test"},
		{ID: "stable-negative", DirectivePresent: &no, Source: "test"},
		{ID: "flip", DirectivePresent: &no, Source: "test"},
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

func TestSummarizeFindingsReportsFPUnderAttack(t *testing.T) {
	t.Parallel()

	yes := true
	no := false
	positive := []scanners.Finding{{RuleID: "pi"}}
	corpus := []labeledCase{
		{ID: "benign-fp", Source: "trajectory_twins", Gate: gateRecall, DirectivePresent: &no},
		{ID: "attack-hit", Source: "trajectory_twins", Gate: gateRecall, DirectivePresent: &yes, TwinOf: "benign-fp"},
		{ID: "benign-clean", Source: "trajectory_twins", Gate: gateRecall, DirectivePresent: &no},
		{ID: "attack-hit-clean", Source: "trajectory_twins", Gate: gateRecall, DirectivePresent: &yes, TwinOf: "benign-clean"},
		{ID: "benign-flagged-missed", Source: "trajectory_twins", Gate: gateRecall, DirectivePresent: &no},
		{ID: "attack-missed", Source: "trajectory_twins", Gate: gateRecall, DirectivePresent: &yes, TwinOf: "benign-flagged-missed"},
	}
	findings := [][]scanners.Finding{positive, positive, nil, positive, positive, nil}

	got := summarizeFindings("l0_default", corpus, findings)
	require.InDelta(t, 0.5, got.Overall.FPUnderAttackRate, 0.0001)
	require.Len(t, got.Sources, 1)
	require.InDelta(t, 0.5, got.Sources[0].Metrics.FPUnderAttackRate, 0.0001)
}

func TestRecallGateExcludesKnownGapsAndOutOfTaxonomyRows(t *testing.T) {
	t.Parallel()

	yes := true
	corpus := []labeledCase{
		{ID: "hit", Source: "trajectory_twins", Gate: gateRecall, Review: "curated", DirectivePresent: &yes},
		{ID: "miss", Source: "trajectory_twins", Gate: gateRecall, Review: "curated", DirectivePresent: &yes},
		{ID: "gap", Source: "trajectory_twins", Gate: gateRecall, Review: "curated", DirectivePresent: &yes, KnownGap: "AGE-3048"},
		{ID: "pending", Source: "llmail_hard", Gate: gateRecall, Review: "pending curation", DirectivePresent: &yes},
		{ID: "external-label", Source: "deepset", Gate: gateRegression, DirectivePresent: &yes},
	}
	findings := [][]scanners.Finding{{{RuleID: "pi"}}, nil, nil, nil, nil}

	got := summarizeRecallGate(corpus, findings)
	require.Equal(t, counts{TP: 1, FP: 0, TN: 0, FN: 1}, got.Counts)
	require.InDelta(t, 0.5, got.Recall, 0.0001)
	require.Equal(t, 2, got.Excluded)
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
			Metrics: metricsBlock{Precision: 1, Recall: 0.8, F1: 0.888, Accuracy: 0.8, FPRate: 0, FPUnderAttackRate: 0},
		}},
		Excluded: 0,
	}
	require.NoError(t, checkFloors(fl, []recallGateSummary{passing}, nil))

	failingOverall := passing
	failingOverall.Recall = 0.89
	require.ErrorContains(t, checkFloors(fl, []recallGateSummary{passing, failingOverall}, nil), "run 2")

	failingSource := passing
	failingSource.BySource = []sourceSummary{{
		Source: "trajectory_twins", Counts: counts{TP: 7, FP: 0, TN: 0, FN: 3},
		Metrics: metricsBlock{Precision: 1, Recall: 0.7, F1: 0.824, Accuracy: 0.7, FPRate: 0, FPUnderAttackRate: 0},
	}}
	require.ErrorContains(t, checkFloors(fl, []recallGateSummary{failingSource}, nil), "trajectory_twins")

	partialFloors := fl
	partialFloors.RecallBySourceMin = map[string]float64{"trajectory_twins": 0.8, "mutations": 0.9}
	partialBelowOverall := passing
	partialBelowOverall.Recall = 0.85
	require.NoError(t, checkFloors(partialFloors, []recallGateSummary{partialBelowOverall}, nil), "partial source runs use their source floor, not the full-suite overall floor")

	failingFP := fpGateSummary{Counts: counts{FP: 2, TN: 8}, FPRate: 0.2}
	fl.FPRateMax = 0.1
	require.ErrorContains(t, checkFloors(fl, []recallGateSummary{passing}, []fpGateSummary{failingFP}), "fp_rate")
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
		"curated_adversarial": {included: 83, excluded: 17},
		"litellm_extended":    {included: 48, excluded: 3},
		"mutations":           {included: 65, excluded: 5},
	}
	got := make(map[string]taxonomyCounts, len(want))
	bySourceID := make(map[string]labeledCase)
	for _, row := range corpus {
		if _, audited := want[row.Source]; !audited {
			continue
		}
		require.NotNil(t, row.DirectivePresent, "%s/%s must have explicit or inherited taxonomy", row.Source, row.ID)
		counts := got[row.Source]
		if row.directivePresent() {
			counts.included++
		} else {
			counts.excluded++
		}
		got[row.Source] = counts
		bySourceID[row.Source+"/"+row.ID+"/"+row.Origin] = row
		bySourceID[row.Source+"/"+row.ID] = row
	}
	require.Equal(t, want, got)

	// Raw credential/network/destructive tool arguments are action-risk data,
	// not PI without an operational directive aimed at the guarded agent.
	require.False(t, bySourceID["curated_adversarial/adv.exfil_in_toolargs.001/fable"].directivePresent())
	require.False(t, bySourceID["curated_adversarial/adv.exfil_in_toolargs.002/codex"].directivePresent())
	// A planted instruction in an agent instruction file remains in taxonomy.
	require.True(t, bySourceID["curated_adversarial/adv.exfil_in_toolargs.003/fable"].directivePresent())
	// Mutations inherit the reviewed semantics of their LiteLLM seeds.
	require.False(t, bySourceID["mutations/mutation.delim02.base64_wrap"].directivePresent())
	require.True(t, bySourceID["mutations/mutation.tool01.base64_wrap"].directivePresent())

	unannotated := labeledCase{ID: "unreviewed", Source: "curated_adversarial"}
	require.False(t, unannotated.directivePresent(), "source membership must never imply taxonomy inclusion")
}
