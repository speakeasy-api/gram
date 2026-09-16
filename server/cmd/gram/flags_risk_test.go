package gram

import (
	"flag"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
)

func TestRiskLLMFlagsAreRegisteredOnStreams(t *testing.T) {
	t.Parallel()

	flags := newStreamsCommand().Flags
	for name, envVar := range map[string]string{
		"risk-llm-url":     "GRAM_RISK_LLM_URL",
		"risk-llm-api-key": "GRAM_RISK_LLM_API_KEY",
		"risk-llm-model":   "GRAM_RISK_LLM_MODEL",
	} {
		idx := slices.IndexFunc(flags, func(f cli.Flag) bool { return slices.Contains(f.Names(), name) })
		require.NotEqual(t, -1, idx, "flag %s must be registered on the streams command", name)
		stringFlag, ok := flags[idx].(*cli.StringFlag)
		require.True(t, ok, "flag %s must be a string flag", name)
		require.Equal(t, []string{envVar}, stringFlag.EnvVars)
	}
}

func TestLLMAnalyzerConfigFromCLI(t *testing.T) {
	t.Parallel()

	set := flag.NewFlagSet("streams", flag.ContinueOnError)
	for _, f := range riskLLMFlags() {
		require.NoError(t, f.Apply(set))
	}
	require.NoError(t, set.Parse([]string{
		"--risk-llm-url", "https://model.example.com/v1",
		"--risk-llm-api-key", "test-key",
	}))

	cfg := llmAnalyzerConfigFromCLI(cli.NewContext(cli.NewApp(), set, nil))
	require.Equal(t, llmanalyzer.Config{
		BaseURL:   "https://model.example.com/v1",
		APIKey:    "test-key",
		Model:     llmanalyzer.DefaultModel,
		Timeout:   llmanalyzer.DefaultTimeout,
		MaxTokens: llmanalyzer.DefaultMaxTokens,
	}, cfg)
	require.NoError(t, cfg.Validate())
}

func TestLLMAnalyzerConfigFromCLI_DisabledWithoutURL(t *testing.T) {
	t.Parallel()

	set := flag.NewFlagSet("streams", flag.ContinueOnError)
	for _, f := range riskLLMFlags() {
		require.NoError(t, f.Apply(set))
	}
	require.NoError(t, set.Parse(nil))

	cfg := llmAnalyzerConfigFromCLI(cli.NewContext(cli.NewApp(), set, nil))
	require.False(t, cfg.Enabled())
	require.Equal(t, llmanalyzer.DefaultModel, cfg.Model)
}
