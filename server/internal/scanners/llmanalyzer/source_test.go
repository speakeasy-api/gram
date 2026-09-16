package llmanalyzer_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
)

func TestRiskKeyForSource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		source string
		key    string
		ok     bool
	}{
		{source: "gitleaks", key: llmanalyzer.KeySecretsLeak, ok: true},
		{source: "presidio", key: llmanalyzer.KeyPersonalDataLeak, ok: true},
		{source: "prompt_injection", key: llmanalyzer.KeyPromptInjection, ok: true},
		{source: "destructive_tool", key: llmanalyzer.KeyDestructiveToolCall, ok: true},
		{source: "cli_destructive", key: llmanalyzer.KeyDestructiveToolCall, ok: true},
		{source: "custom", key: "", ok: false},
		{source: "shadow_mcp", key: "", ok: false},
		{source: "account_identity", key: "", ok: false},
		{source: "llm_judge", key: "", ok: false},
		{source: llmanalyzer.Source, key: "", ok: false},
		{source: "", key: "", ok: false},
	}
	for _, tc := range cases {
		key, ok := llmanalyzer.RiskKeyForSource(tc.source)
		require.Equal(t, tc.ok, ok, "RiskKeyForSource(%q) ok", tc.source)
		require.Equal(t, tc.key, key, "RiskKeyForSource(%q) key", tc.source)
	}
}

func TestRuleIDForKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		key    string
		ruleID string
	}{
		{key: llmanalyzer.KeySecretsLeak, ruleID: llmanalyzer.RuleSecret},
		{key: llmanalyzer.KeyPersonalDataLeak, ruleID: llmanalyzer.RulePII},
		{key: llmanalyzer.KeyPromptInjection, ruleID: llmanalyzer.RulePromptInjection},
		{key: llmanalyzer.KeyDestructiveToolCall, ruleID: llmanalyzer.RuleDestructiveTool},
		{key: "unknown", ruleID: ""},
		{key: "", ruleID: ""},
	}
	for _, tc := range cases {
		require.Equal(t, tc.ruleID, llmanalyzer.RuleIDForKey(tc.key), "RuleIDForKey(%q)", tc.key)
	}
}

func TestCoveredSourcesRoundTrip(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"gitleaks", "presidio", "prompt_injection", "destructive_tool", "cli_destructive"}, llmanalyzer.CoveredSources)
	for _, source := range llmanalyzer.CoveredSources {
		key, ok := llmanalyzer.RiskKeyForSource(source)
		require.True(t, ok, "covered source %q must map to a risk key", source)
		require.NotEmpty(t, llmanalyzer.RuleIDForKey(key), "risk key %q must map to a rule id", key)
	}
}

func TestCoversAnySource(t *testing.T) {
	t.Parallel()

	require.True(t, llmanalyzer.CoversAnySource([]string{"gitleaks"}))
	require.True(t, llmanalyzer.CoversAnySource([]string{"custom", "presidio"}))
	require.True(t, llmanalyzer.CoversAnySource([]string{"cli_destructive"}))
	require.False(t, llmanalyzer.CoversAnySource([]string{"custom", "shadow_mcp", "account_identity"}))
	require.False(t, llmanalyzer.CoversAnySource(nil))
	require.False(t, llmanalyzer.CoversAnySource([]string{}))
	require.False(t, llmanalyzer.CoversAnySource([]string{""}))
}

// Every rule id the analyzer emits must resolve to a user-facing category
// without depending on the analyzer's own source name.
func TestRuleIDsClassifyByRuleID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		ruleID string
		want   categories.Category
	}{
		{ruleID: llmanalyzer.RuleSecret, want: categories.CategorySecrets},
		{ruleID: llmanalyzer.RulePII, want: categories.CategoryPII},
		{ruleID: llmanalyzer.RulePromptInjection, want: categories.CategoryPromptInjection},
		{ruleID: llmanalyzer.RuleDestructiveTool, want: categories.CategoryDestructiveTool},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, categories.Classify(llmanalyzer.Source, tc.ruleID), "Classify(%q, %q)", llmanalyzer.Source, tc.ruleID)
	}
}
