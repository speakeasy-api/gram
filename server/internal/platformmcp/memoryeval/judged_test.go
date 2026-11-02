//go:build memoryeval

package memoryeval_test

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/bench"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	// defaultAnswerModels answer every question in every arm, so the tools are
	// the only variable between arms. MEMORYEVAL_ANSWER_MODELS takes a comma
	// separated list to compare models in one run.
	defaultAnswerModels = "anthropic/claude-sonnet-5.5"

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
	cfg := bench.Config{
		AnswerModels: strings.Split(envOr("MEMORYEVAL_ANSWER_MODELS", envOr("MEMORYEVAL_ANSWER_MODEL", defaultAnswerModels)), ","),
		JudgeModel:   envOr("MEMORYEVAL_JUDGE_MODEL", defaultJudgeModel),
		Runs:         envInt(t, "MEMORYEVAL_RUNS", defaultRuns),
		Concurrency:  envInt(t, "MEMORYEVAL_CONCURRENCY", defaultConcurrency),
	}
	for i := range cfg.AnswerModels {
		cfg.AnswerModels[i] = strings.TrimSpace(cfg.AnswerModels[i])
	}
	require.NoError(t, cfg.Validate())

	provisioner := openrouter.NewDevelopment(apiKey)
	client := openrouter.NewUnifiedClient(
		testenv.NewLogger(t),
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
	searchArm, err := bench.SearchChatsArm(testenv.NewLogger(t), loaded.target())
	require.NoError(t, err)

	// Team memory is written by the summary and digest models under test
	// before the arms that read it run.
	models := bench.MemoryModels{
		Summary: envOr("MEMORYEVAL_SUMMARY_MODEL", bench.DefaultMemoryModels.Summary),
		Digest:  envOr("MEMORYEVAL_DIGEST_MODEL", bench.DefaultMemoryModels.Digest),
	}
	prep, err := bench.Prepare(t.Context(), testenv.NewLogger(t), client, loaded.target(), models, cfg.Concurrency)
	require.NoError(t, err)
	memoryArm, err := bench.SearchTeamMemoryArm(testenv.NewLogger(t), loaded.target())
	require.NoError(t, err)

	report, err := bench.Run(t.Context(), client, corpus, []bench.Arm{bench.NoMemoryArm(), searchArm, memoryArm}, cfg)
	require.NoError(t, err)
	report.Preparation = &prep
	markdown := report.Markdown()
	t.Log("\n" + markdown)
	if out := os.Getenv("MEMORYEVAL_OUT"); out != "" {
		require.NoError(t, os.WriteFile(out, []byte(markdown), 0o600))
	}
}
