//go:build memoryeval

package memoryeval_test

import (
	"log/slog"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	// defaultAnswerModel answers every question in every arm, so the tools are
	// the only variable between arms.
	defaultAnswerModel = "anthropic/claude-sonnet-5.5"

	// defaultJudgeModel grades answers. It differs from the answering model so
	// the judge does not favor its own phrasing.
	defaultJudgeModel = "openai/gpt-5.5"

	// defaultRuns repeats each arm so results carry a range, not one sample.
	defaultRuns = 3

	// defaultConcurrency bounds in-flight questions across arms and runs.
	defaultConcurrency = 8
)

// envOr returns the variable's value, or fallback when it is empty or holds
// the "unset" placeholder mise.toml uses for missing keys.
func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" && value != "unset" {
		return value
	}
	return fallback
}

func envInt(t *testing.T, name string, fallback int) int {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	require.NoError(t, err, name)
	require.Positive(t, n, name)
	return n
}

// TestMemoryEvalJudged runs the LLM half of the eval: an answering agent per
// arm, graded by a judge model. It costs LLM calls, so it runs only under the
// memoryeval build tag through mise run eval:memory.
func TestMemoryEvalJudged(t *testing.T) { //nolint:paralleltest // one long run that fans out internally
	apiKey := envOr("OPENROUTER_DEV_KEY", os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" || apiKey == "unset" {
		t.Skip("set OPENROUTER_DEV_KEY to run the judged memory eval")
	}
	answerModel := envOr("MEMORYEVAL_ANSWER_MODEL", defaultAnswerModel)
	judgeModel := envOr("MEMORYEVAL_JUDGE_MODEL", defaultJudgeModel)
	require.NotEqual(t, answerModel, judgeModel, "the judge must differ from the answering model")
	require.True(t, openrouter.IsModelAllowed(answerModel), answerModel)
	require.True(t, openrouter.IsModelAllowed(judgeModel), judgeModel)
	runs := envInt(t, "MEMORYEVAL_RUNS", defaultRuns)
	concurrency := envInt(t, "MEMORYEVAL_CONCURRENCY", defaultConcurrency)

	provisioner := openrouter.NewDevelopment(apiKey)
	client := openrouter.NewUnifiedClient(
		slog.New(slog.DiscardHandler),
		guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)),
		provisioner,
		&openrouter.PlatformKeyResolver{Provisioner: provisioner},
		nil,
		nil,
		nil,
		nil,
	)

	corpus := memoryeval.Build(corpusNow)
	loaded := loadCorpus(t, "memoryeval_judged", corpus)
	ctx, searchArm := searchChatsArm(t, loaded)
	arms := []arm{
		{name: "A: no memory", search: nil, sweep: nil, tools: nil, call: nil, instructions: ""},
		searchArm,
	}

	answers, verdicts := runEval(ctx, t, client, answerModel, judgeModel, corpus, arms, runs, concurrency)
	report := buildReport(corpus, answerModel, judgeModel, runs, arms, answers, verdicts)
	t.Log("\n" + report)
	if out := os.Getenv("MEMORYEVAL_OUT"); out != "" {
		require.NoError(t, os.WriteFile(out, []byte(report), 0o600))
	}
}
