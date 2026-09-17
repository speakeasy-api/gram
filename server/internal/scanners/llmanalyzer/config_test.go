package llmanalyzer_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
)

func TestConfig_Validate(t *testing.T) {
	t.Parallel()

	valid := llmanalyzer.Config{
		BaseURL:   "https://model.example.com/v1",
		APIKey:    "secret",
		Model:     "risk-judge-4b",
		Timeout:   0,
		MaxTokens: 0,
	}

	cases := []struct {
		name    string
		mutate  func(c *llmanalyzer.Config)
		wantErr string
	}{
		{name: "valid with zero timeout and max tokens", mutate: func(*llmanalyzer.Config) {}, wantErr: ""},
		{name: "valid with explicit timeout and max tokens", mutate: func(c *llmanalyzer.Config) { c.Timeout = time.Second; c.MaxTokens = 10 }, wantErr: ""},
		{name: "empty base url", mutate: func(c *llmanalyzer.Config) { c.BaseURL = "" }, wantErr: "base url is required"},
		{name: "http base url", mutate: func(c *llmanalyzer.Config) { c.BaseURL = "http://model.example.com/v1" }, wantErr: "scheme must be https"},
		{name: "relative base url", mutate: func(c *llmanalyzer.Config) { c.BaseURL = "model.example.com/v1" }, wantErr: "scheme must be https"},
		{name: "base url without host", mutate: func(c *llmanalyzer.Config) { c.BaseURL = "https:///v1" }, wantErr: "must include a host"},
		{name: "unparsable base url", mutate: func(c *llmanalyzer.Config) { c.BaseURL = "https://model.example.com/%zz" }, wantErr: "parse base url"},
		{name: "base url with query", mutate: func(c *llmanalyzer.Config) { c.BaseURL = "https://model.example.com/v1?api-version=1" }, wantErr: "must not include a query or fragment"},
		{name: "base url with fragment", mutate: func(c *llmanalyzer.Config) { c.BaseURL = "https://model.example.com/v1#v1" }, wantErr: "must not include a query or fragment"},
		{name: "empty model", mutate: func(c *llmanalyzer.Config) { c.Model = "" }, wantErr: "model is required"},
		{name: "empty api key", mutate: func(c *llmanalyzer.Config) { c.APIKey = "" }, wantErr: "api key is required"},
		{name: "api key with newline", mutate: func(c *llmanalyzer.Config) { c.APIKey = "secret\n" }, wantErr: "api key must not contain control characters"},
		{name: "negative timeout", mutate: func(c *llmanalyzer.Config) { c.Timeout = -time.Second }, wantErr: "timeout must not be negative"},
		{name: "negative max tokens", mutate: func(c *llmanalyzer.Config) { c.MaxTokens = -1 }, wantErr: "max tokens must not be negative"},
	}
	for _, tc := range cases {
		cfg := valid
		tc.mutate(&cfg)
		err := cfg.Validate()
		if tc.wantErr == "" {
			require.NoError(t, err, tc.name)
			continue
		}
		require.Error(t, err, tc.name)
		require.ErrorContains(t, err, tc.wantErr, tc.name)
	}
}

func TestConfig_ValidateJoinsEveryProblem(t *testing.T) {
	t.Parallel()

	err := llmanalyzer.Config{BaseURL: "", APIKey: "", Model: "", Timeout: 0, MaxTokens: 0}.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "base url is required")
	require.ErrorContains(t, err, "model is required")
	require.ErrorContains(t, err, "api key is required")
}

func TestConfig_Enabled(t *testing.T) {
	t.Parallel()

	require.False(t, llmanalyzer.Config{BaseURL: "", APIKey: "k", Model: "m", Timeout: 0, MaxTokens: 0}.Enabled())
	require.True(t, llmanalyzer.Config{BaseURL: "https://model.example.com/v1", APIKey: "", Model: "", Timeout: 0, MaxTokens: 0}.Enabled())
}

func TestConfig_Defaults(t *testing.T) {
	t.Parallel()

	require.Equal(t, 15*time.Second, llmanalyzer.DefaultTimeout)
	require.Equal(t, 1024, llmanalyzer.DefaultMaxTokens)
	require.Equal(t, "risk-judge-4b", llmanalyzer.DefaultModel)
}
