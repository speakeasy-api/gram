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
		"claude-code-desktop",
		"Codex",
		"cursor",
		"Cursor",
		"Claude Cowork",
		"chatgpt",
		"ChatGPT",
		"",
		"   ",
		"claude-code",
	})

	require.Equal(t, []string{"chatgpt", "claude", "claude-chat-web", "claude-code", "claude-code-desktop", "codex", "cowork", "cursor"}, got)
}

func TestCanonicalizeSources_Empty(t *testing.T) {
	t.Parallel()

	require.Empty(t, canonicalizeSources(nil))
	require.Empty(t, canonicalizeSources([]string{"", "  "}))
}

func TestExpandSourceAliases_ExpandsCanonical(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"claude-code", "ClaudeCode"}, expandSourceAliases([]string{"claude-code"}))
	// Claude Code Desktop is its own surface — it must not expand into (or be
	// swallowed by) the claude-code CLI aliases.
	require.Equal(t, []string{"claude-code-desktop"}, expandSourceAliases([]string{"claude-code-desktop"}))
	require.Equal(
		t,
		[]string{"claude", "claude-desktop", "claude-chat-desktop", "Claude Chat Desktop"},
		expandSourceAliases([]string{"claude"}),
	)
	// ChatGPT rows arrive from the compliance import under the chatgpt hook
	// source; the display alias must round-trip through the filter.
	require.Equal(t, []string{"chatgpt", "ChatGPT"}, expandSourceAliases([]string{"chatgpt"}))
	// Codex cloud web-task transcripts import under codex-web — a distinct
	// surface from live device codex sessions, which must not absorb it.
	require.Equal(t, []string{"codex-web", "CodexWeb", "Codex Web"}, expandSourceAliases([]string{"codex-web"}))
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
