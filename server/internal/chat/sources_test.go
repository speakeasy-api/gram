package chat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalizeSources_CollapsesAliasesAndDropsEmpty(t *testing.T) {
	t.Parallel()

	got := canonicalizeSources([]string{
		"claude",
		"Claude Chat Desktop",
		"Claude Chat Web",
		"claude-code",
		"ClaudeCode",
		"Codex",
		"cursor",
		"Cursor",
		"Claude Cowork",
		"",
		"   ",
		"claude-code",
	})

	require.Equal(t, []string{"claude", "claude-chat-web", "claude-code", "codex", "cowork", "cursor"}, got)
}

func TestCanonicalizeSources_Empty(t *testing.T) {
	t.Parallel()

	require.Empty(t, canonicalizeSources(nil))
	require.Empty(t, canonicalizeSources([]string{"", "  "}))
}

func TestExpandSourceAliases_ExpandsCanonical(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"claude-code", "ClaudeCode"}, expandSourceAliases([]string{"claude-code"}))
	require.Equal(
		t,
		[]string{"claude", "claude-desktop", "claude-chat-desktop", "Claude Chat Desktop"},
		expandSourceAliases([]string{"claude"}),
	)
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

	require.Equal(
		t,
		[]string{"claude-code", "ClaudeCode", "codex", "Codex"},
		parseSourceFilter("claude-code, codex"),
	)
	require.Empty(t, parseSourceFilter(""))
}
