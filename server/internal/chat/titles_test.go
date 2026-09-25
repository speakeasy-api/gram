package chat_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat"
)

func TestIsPlaceholderTitleCoversEverySeededTitle(t *testing.T) {
	t.Parallel()

	// Every stand-in a capture path seeds a chat with has to be recognized
	// here, or that path's chats are treated as deliberately titled and never
	// get a generated name.
	for _, title := range []string{
		"",
		"   ",
		chat.DefaultChatTitle,
		chat.DefaultClaudeChatTitle,
		chat.DefaultCoworkChatTitle,
		chat.DefaultClaudeAmbiguous,
		chat.DefaultCursorChatTitle,
		chat.DefaultCodexChatTitle,
		chat.DefaultInferenceChatTitle,
	} {
		require.True(t, chat.IsPlaceholderTitle(title), "expected %q to be a placeholder", title)
	}
}

func TestIsPlaceholderTitleLeavesRealTitles(t *testing.T) {
	t.Parallel()

	require.False(t, chat.IsPlaceholderTitle("Claude Tag in #dev-demo"))
	require.False(t, chat.IsPlaceholderTitle("Reduce token usage in prompts"))
}

func TestDerivedTitleTrimsAndBoundsByRunes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "why does the build fail?", chat.DerivedTitle("\n  why does the build fail?  "))

	// Bounded by runes, not bytes, so a multi-byte prompt is never cut
	// mid-character.
	title := chat.DerivedTitle(strings.Repeat("é", 200))
	require.Len(t, []rune(title), 80)
	require.Equal(t, strings.Repeat("é", 80), title)
}

// A session's stand-in title is re-derived from the chat's own messages to
// decide whether title generation may replace it, so the two must agree for
// both short and truncated prompts.
func TestIsDerivedTitleMatchesTheMessageItCameFrom(t *testing.T) {
	t.Parallel()

	short := "combine the domain verification task into the main setup task"
	long := "ok i would like to simplify this pr. the ideal state should be that we will have exactly one code path for this"

	require.True(t, chat.IsDerivedTitle(chat.DerivedTitle(short), "unrelated", short))
	require.True(t, chat.IsDerivedTitle(chat.DerivedTitle(long), long))
	require.False(t, chat.IsDerivedTitle("Simplifying The Pull Request", short, long))
}

// An empty title is not "derived from" an empty message: emptiness is handled
// as a placeholder, and treating it as derived would let any chat with a blank
// tool result match.
func TestIsDerivedTitleIgnoresEmptyTitle(t *testing.T) {
	t.Parallel()

	require.False(t, chat.IsDerivedTitle("", ""))
	require.False(t, chat.IsDerivedTitle("  ", "  "))
}
