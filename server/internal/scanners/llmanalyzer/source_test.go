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

func TestRuleIDForSource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		source string
		ruleID string
		ok     bool
	}{
		{source: "gitleaks", ruleID: llmanalyzer.RuleSecret, ok: true},
		{source: "presidio", ruleID: llmanalyzer.RulePII, ok: true},
		{source: "prompt_injection", ruleID: llmanalyzer.RulePromptInjection, ok: true},
		{source: "destructive_tool", ruleID: llmanalyzer.RuleDestructiveTool, ok: true},
		{source: "cli_destructive", ruleID: llmanalyzer.RuleCLIDestructive, ok: true},
		{source: "custom", ruleID: "", ok: false},
		{source: "shadow_mcp", ruleID: "", ok: false},
		{source: "account_identity", ruleID: "", ok: false},
		{source: "llm_judge", ruleID: "", ok: false},
		{source: llmanalyzer.Source, ruleID: "", ok: false},
		{source: "", ruleID: "", ok: false},
	}
	for _, tc := range cases {
		ruleID, ok := llmanalyzer.RuleIDForSource(tc.source)
		require.Equal(t, tc.ok, ok, "RuleIDForSource(%q) ok", tc.source)
		require.Equal(t, tc.ruleID, ruleID, "RuleIDForSource(%q) rule id", tc.source)
	}
}

// destructive_tool and cli_destructive share a model risk key but must not
// share a rule id, otherwise cli_destructive findings land in the
// destructive_tool category.
func TestSharedRiskKeyKeepsDistinctRuleIDs(t *testing.T) {
	t.Parallel()

	toolKey, ok := llmanalyzer.RiskKeyForSource("destructive_tool")
	require.True(t, ok)
	cliKey, ok := llmanalyzer.RiskKeyForSource("cli_destructive")
	require.True(t, ok)
	require.Equal(t, toolKey, cliKey, "both sources score the same risk key")

	toolRule, ok := llmanalyzer.RuleIDForSource("destructive_tool")
	require.True(t, ok)
	cliRule, ok := llmanalyzer.RuleIDForSource("cli_destructive")
	require.True(t, ok)
	require.NotEqual(t, toolRule, cliRule, "each source carries its own rule id")
	require.Equal(t, llmanalyzer.RuleCLIDestructive, cliRule)
	require.Equal(t, llmanalyzer.RuleDestructiveTool, llmanalyzer.RuleIDForKey(cliKey), "key-only lookup resolves to the destructive_tool rule")
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
		ruleID, ok := llmanalyzer.RuleIDForSource(source)
		require.True(t, ok, "covered source %q must map to a rule id", source)
		require.NotEmpty(t, ruleID, "covered source %q must map to a non-empty rule id", source)
	}
}

func TestAllRuleIDs(t *testing.T) {
	t.Parallel()

	got := llmanalyzer.AllRuleIDs()
	require.Equal(t, []string{
		llmanalyzer.RuleSecret,
		llmanalyzer.RulePII,
		llmanalyzer.RulePromptInjection,
		llmanalyzer.RuleDestructiveTool,
		llmanalyzer.RuleCLIDestructive,
		llmanalyzer.RuleDeadLetter,
	}, got)

	seen := make(map[string]bool, len(got))
	for _, ruleID := range got {
		require.False(t, seen[ruleID], "rule id %q listed twice", ruleID)
		seen[ruleID] = true
	}
	require.Len(t, got, len(llmanalyzer.CoveredSources)+1)
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
		{ruleID: llmanalyzer.RuleCLIDestructive, want: categories.CategoryCLIDestructive},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, categories.Classify(llmanalyzer.Source, tc.ruleID), "Classify(%q, %q)", llmanalyzer.Source, tc.ruleID)
	}

	// Every covered source's rule id resolves to the same category the legacy
	// source it replaces would have resolved to.
	for _, source := range llmanalyzer.CoveredSources {
		ruleID, ok := llmanalyzer.RuleIDForSource(source)
		require.True(t, ok)
		require.Equal(t, categories.Classify(source, ""), categories.Classify(llmanalyzer.Source, ruleID), "rule id %q for source %q", ruleID, source)
	}
}
