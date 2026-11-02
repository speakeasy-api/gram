package bench

import (
	"context"
	"errors"
	"testing"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

var testNow = time.Date(2026, time.October, 30, 12, 0, 0, 0, time.UTC)

// TestSummarizeLeavesErrorsOutOfAccuracy checks that an answer that failed
// for API reasons counts as an error, not as a wrong answer.
func TestSummarizeLeavesErrorsOutOfAccuracy(t *testing.T) {
	t.Parallel()
	corpus := memoryeval.Build(testNow)
	a := NoMemoryArm()
	cfg := Config{AnswerModels: []string{"anthropic/claude-sonnet-5.5", "google/gemini-3.1-flash-lite"}, JudgeModel: "openai/gpt-5.5", Runs: 2, Concurrency: 1}
	answers := []answer{
		{model: cfg.AnswerModels[0], arm: a.Name, run: 0, question: corpus.Questions[0], text: "right", usage: usage{promptTokens: 10, completionTokens: 5, costUSD: 0.25, latency: time.Second}},
		{model: cfg.AnswerModels[0], arm: a.Name, run: 0, question: corpus.Questions[1], err: errors.New("rate limited"), usage: usage{latency: 3 * time.Second}},
		// Run 1 of the first model graded nothing, so it stays out of accuracy.
		{model: cfg.AnswerModels[0], arm: a.Name, run: 1, question: corpus.Questions[0], err: errors.New("rate limited"), usage: usage{latency: 3 * time.Second}},
		{model: cfg.AnswerModels[1], arm: a.Name, run: 0, question: corpus.Questions[0], text: "wrong", usage: usage{promptTokens: 7, completionTokens: 3, costUSD: 0.01, latency: time.Second}},
		{model: cfg.AnswerModels[1], arm: a.Name, run: 1, question: corpus.Questions[0], text: "right", usage: usage{promptTokens: 7, completionTokens: 3, costUSD: 0.01, latency: time.Second}},
	}
	verdicts := []verdict{
		{Correct: true, Miss: "", Reason: "ok", usage: usage{costUSD: 0.5}},
		// An errored answer is never sent to the judge, so no client is needed.
		judge(t.Context(), nil, cfg.JudgeModel, answers[1]),
		judge(t.Context(), nil, cfg.JudgeModel, answers[2]),
		{Correct: false, Miss: "reasoning", Reason: "wrong", usage: usage{costUSD: 0.5}},
		{Correct: true, Miss: "", Reason: "ok", usage: usage{costUSD: 0.5}},
	}

	report := summarize(corpus, cfg, []Arm{a}, answers, verdicts)
	require.Len(t, report.Scores, 2)
	other := report.Scores[1]
	require.Equal(t, cfg.AnswerModels[1], other.Model)
	require.Equal(t, []float64{0, 1}, other.AccuracyByRun)
	require.InDelta(t, 0.5, other.Accuracy, 1e-9)
	require.Equal(t, 14, other.PromptTokens)
	require.InDelta(t, 0.02, other.CostUSD, 1e-9)
	require.InDelta(t, 1.5, report.JudgeCostUSD, 1e-9)
	s := report.Scores[0]
	require.Equal(t, cfg.AnswerModels[0], s.Model)
	require.Len(t, s.AccuracyByRun, 1)
	require.InDelta(t, 1.0, s.AccuracyByRun[0], 1e-9)
	require.InDelta(t, 1.0, s.Accuracy, 1e-9)
	require.Equal(t, 2, s.Errors)
	require.Equal(t, 10, s.PromptTokens)
	require.Equal(t, 5, s.CompletionTokens)
	require.InDelta(t, 0.25, s.CostUSD, 1e-9)
	require.Equal(t, int64(2333), s.LatencyMeanMS)
	require.Equal(t, int64(3000), s.LatencyP95MS)
}

func TestLatencyStats(t *testing.T) {
	t.Parallel()
	mean, p95 := latencyStats(nil)
	require.Zero(t, mean)
	require.Zero(t, p95)

	latencies := make([]time.Duration, 0, 100)
	for i := 100; i >= 1; i-- {
		latencies = append(latencies, time.Duration(i)*time.Millisecond)
	}
	mean, p95 = latencyStats(latencies)
	require.Equal(t, int64(50), mean)
	require.Equal(t, int64(95), p95)
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()
	ok := Config{AnswerModels: []string{"anthropic/claude-sonnet-5.5", "google/gemini-3.1-flash-lite"}, JudgeModel: "openai/gpt-5.5", Runs: 1, Concurrency: 1}
	require.NoError(t, ok.Validate())

	for name, mutate := range map[string]func(*Config){
		"no answer models":  func(c *Config) { c.AnswerModels = nil },
		"judge answers too": func(c *Config) { c.AnswerModels = []string{c.JudgeModel} },
		"duplicate model":   func(c *Config) { c.AnswerModels = []string{c.AnswerModels[0], c.AnswerModels[0]} },
		"unknown model":     func(c *Config) { c.AnswerModels = []string{"example/not-a-model"} },
		"unknown judge":     func(c *Config) { c.JudgeModel = "example/not-a-model" },
		"zero runs":         func(c *Config) { c.Runs = 0 },
		"zero concurrency":  func(c *Config) { c.Concurrency = 0 },
	} {
		cfg := ok
		cfg.AnswerModels = append([]string(nil), ok.AnswerModels...)
		mutate(&cfg)
		require.Error(t, cfg.Validate(), name)
	}
}

func TestParseVerdictRejectsIncompleteOrContradictoryReplies(t *testing.T) {
	t.Parallel()
	ok, err := parseVerdict(`{"correct": false, "miss": "reasoning", "reason": "wrong library"}`)
	require.NoError(t, err)
	require.Equal(t, verdict{Correct: false, Miss: "reasoning", Reason: "wrong library", usage: usage{}}, ok)

	for _, reply := range []string{
		`{"miss": "", "reason": "no verdict"}`,
		`{"correct": true, "miss": "retrieval", "reason": "both"}`,
		`{"correct": false, "miss": "", "reason": "no miss"}`,
		`not json`,
	} {
		_, err := parseVerdict(reply)
		require.Error(t, err, reply)
	}
}

// keyResolverSentinel stops a completion right after request validation.
type keyResolverSentinel struct{}

var errReachedKeyResolver = errors.New("reached key resolution")

func (keyResolverSentinel) ResolveKey(context.Context, string, string, billing.ModelUsageSource, openrouter.KeyType) (openrouter.ResolvedKey, error) {
	return openrouter.ResolvedKey{}, errReachedKeyResolver
}

// TestCompletionRequestPassesClientValidation runs the eval's request through
// the real completion client. Key resolution happens after validation, so
// reaching it proves the client would send the request to the model.
func TestCompletionRequestPassesClientValidation(t *testing.T) {
	t.Parallel()
	client := openrouter.NewUnifiedClient(
		testenv.NewLogger(t),
		guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)),
		openrouter.NewDevelopment("unused"),
		keyResolverSentinel{},
		nil, nil, nil, nil,
	)
	_, err := client.GetCompletion(t.Context(), completionRequest("anthropic/claude-sonnet-5.5", []or.ChatMessages{openrouter.CreateMessageUser("hi")}, nil, nil))
	require.ErrorIs(t, err, errReachedKeyResolver)
}

func TestSomeSucceeded(t *testing.T) {
	t.Parallel()
	errs := []error{errors.New("rate limited"), nil}
	require.NoError(t, someSucceeded("answer", len(errs), func(i int) error { return errs[i] }))
	errs = []error{errors.New("rate limited"), errors.New("bad key")}
	require.ErrorContains(t, someSucceeded("answer", len(errs), func(i int) error { return errs[i] }), "every answer call failed")
}
