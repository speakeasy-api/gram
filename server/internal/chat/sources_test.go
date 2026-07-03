package chat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalizeSources_CollapsesAliasesAndDropsEmpty(t *testing.T) {
	t.Parallel()

	got := canonicalizeSources([]string{
		"claude-code",
		"ClaudeCode",
		"Codex",
		"",
		"   ",
		"claude-code",
	})

	require.Equal(t, []string{"Codex", "claude-code"}, got)
}

func TestCanonicalizeSources_Empty(t *testing.T) {
	t.Parallel()

	require.Empty(t, canonicalizeSources(nil))
	require.Empty(t, canonicalizeSources([]string{"", "  "}))
}

func TestExpandSourceAliases_ExpandsCanonical(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"claude-code", "ClaudeCode"}, expandSourceAliases([]string{"claude-code"}))
}

func TestExpandSourceAliases_PassesThroughUnknown(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"Codex", "playground"}, expandSourceAliases([]string{"Codex", "playground"}))
}

func TestExpandSourceAliases_Dedupes(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"claude-code", "ClaudeCode"}, expandSourceAliases([]string{"claude-code", "ClaudeCode"}))
}

func TestParseSourceFilter_ExpandsAliases(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"claude-code", "ClaudeCode", "Codex"}, parseSourceFilter("claude-code, Codex"))
	require.Empty(t, parseSourceFilter(""))
}
