package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func TestCommittedBenchSetMatchesProductionAndCoversEveryAnchor(t *testing.T) {
	t.Parallel()

	set, err := loadBenchSet("cases.json")
	require.NoError(t, err)
	require.Equal(t, efficacy.JudgePromptVersion, set.JudgePromptVersion)
	require.Equal(t, efficacy.JudgeModel, set.JudgeModel)
	require.Equal(t, 0.8, set.MinimumAgreement)
	require.Len(t, set.Cases, 10)

	for _, anchor := range []float64{0, 0.25, 0.5, 0.75, 1} {
		covered := false
		for _, tc := range set.Cases {
			covered = covered || tc.ScoreMin <= anchor && anchor <= tc.ScoreMax
		}
		require.True(t, covered, "score anchor %.2f has no labeled case", anchor)
	}
}

func TestLoadBenchSetRejectsStalePromptVersion(t *testing.T) {
	t.Parallel()

	set := validTestBenchSet()
	set.JudgePromptVersion = "stale"
	b, err := json.Marshal(set)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "cases.json")
	require.NoError(t, os.WriteFile(path, b, 0o600))

	_, err = loadBenchSet(path)
	require.EqualError(t, err, `corpus prompt version "stale" does not match production "v2"`)
}

func TestBuildRequestMatchesProductionJudgeSettings(t *testing.T) {
	t.Parallel()

	request, err := buildRequest(efficacy.JudgeModel, validTestBenchSet().Cases[0])

	require.NoError(t, err)
	require.Equal(t, efficacy.JudgeModel, request.Model)
	require.Equal(t, efficacy.SystemPrompt, request.SystemPrompt)
	require.Equal(t, billing.ModelUsageSourceSkillEfficacy, request.UsageSource)
	require.Equal(t, openrouter.KeyTypeInternal, request.KeyType)
	require.Equal(t, billing.ModelUsageSourceSkillEfficacy, request.KeySlot)
	require.NotNil(t, request.Temperature)
	require.Zero(t, *request.Temperature)
	require.NotNil(t, request.JSONSchema)
	require.Equal(t, efficacy.VerdictSchema(), request.JSONSchema.Schema)
}

func TestSummarizeUsesSuccessfulRunMedianAndReportsErrors(t *testing.T) {
	t.Parallel()

	set := validTestBenchSet()
	set.Cases = append(set.Cases, testCase{ID: "second", ScoreMin: 0.7, ScoreMax: 0.8})
	firstLowScore := 0.4
	firstHighScore := 0.6
	secondScore := 0.75
	results := []result{
		{RequestedModel: efficacy.JudgeModel, CaseID: "case", ScoreMin: 0.4, ScoreMax: 0.6, Score: &firstLowScore, Latency: time.Second, Tokens: 100},
		{RequestedModel: efficacy.JudgeModel, CaseID: "case", ScoreMin: 0.4, ScoreMax: 0.6, Score: &firstHighScore, Latency: 2 * time.Second, Tokens: 100},
		{RequestedModel: efficacy.JudgeModel, CaseID: "second", ScoreMin: 0.7, ScoreMax: 0.8, Score: &secondScore, Latency: 3 * time.Second, Tokens: 100},
		{RequestedModel: efficacy.JudgeModel, CaseID: "second", ScoreMin: 0.7, ScoreMax: 0.8, Score: nil, Latency: 4 * time.Second, Error: "completion failed"},
	}

	summary := summarize(set, efficacy.JudgeModel, results)

	require.Equal(t, 2, summary.AgreedCases)
	require.Equal(t, 1.0, summary.Agreement)
	require.Equal(t, 0.75, summary.RunAgreement)
	require.Equal(t, 1, summary.Errors)
	require.Equal(t, 2*time.Second, summary.P50)
	require.Equal(t, 3*time.Second, summary.P95)
}

func TestLoadBaselineRejectsMixedModels(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "baseline.json")
	b, err := json.Marshal([]result{{RequestedModel: "model-a"}, {RequestedModel: "model-b"}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, b, 0o600))

	_, err = loadBaseline(path)
	require.EqualError(t, err, "baseline must contain exactly one requested model, found 2")
}

func validTestBenchSet() benchSet {
	return benchSet{
		JudgePromptVersion: efficacy.JudgePromptVersion,
		JudgeModel:         efficacy.JudgeModel,
		MinimumAgreement:   0.8,
		Cases: []testCase{{
			ID:           "case",
			SkillName:    "verification",
			SkillContent: "Run the relevant check.",
			Surface:      "dev",
			ActivatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Transcript: efficacy.Transcript{Messages: []efficacy.TranscriptMessage{{
				Index:   1,
				Role:    "assistant",
				Content: "The focused check passes.",
			}}},
			ScoreMin: 0.4,
			ScoreMax: 0.6,
			Note:     "synthetic",
		}},
	}
}
