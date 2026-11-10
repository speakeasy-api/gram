package chat_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat"
)

func TestIsPlaceholderTitle(t *testing.T) {
	t.Parallel()

	require.True(t, chat.IsPlaceholderTitle(""))
	require.True(t, chat.IsPlaceholderTitle("   "))
	require.True(t, chat.IsPlaceholderTitle(" "+chat.DefaultInferenceChatTitle+" "))
	require.False(t, chat.IsPlaceholderTitle("Claude Tag in #dev-demo"))
	require.False(t, chat.IsPlaceholderTitle("Reduce token usage in prompts"))
}

func TestDerivedTitleTrimsAndBoundsByRunes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "why does the build fail?", chat.DerivedTitle("\n  why does the build fail?  "))

	// Bounded by runes so a multi-byte prompt is never cut mid-character.
	title := chat.DerivedTitle(strings.Repeat("é", 200))
	require.Equal(t, strings.Repeat("é", chat.MaxDerivedTitleRunes), title)
}

func TestIsDerivedTitleMatchesTheMessageItCameFrom(t *testing.T) {
	t.Parallel()

	short := "combine the domain verification task into the main setup task"
	long := "ok i would like to simplify this pr. the ideal state should be that we will have exactly one code path for this"

	require.True(t, chat.IsDerivedTitle(chat.DerivedTitle(short), "unrelated", short))
	require.True(t, chat.IsDerivedTitle(chat.DerivedTitle(long), long))
	require.False(t, chat.IsDerivedTitle("Simplifying The Pull Request", short, long))
	// Only a title cut at the rune cap may match a longer message by prefix.
	require.False(t, chat.IsDerivedTitle(short, short+" and more"))
	require.False(t, chat.IsDerivedTitle(strings.Repeat("x", chat.MaxDerivedTitleRunes+1), strings.Repeat("x", 200)))
}

// An empty title is a placeholder, not something derived from a blank message.
func TestIsDerivedTitleIgnoresEmptyTitle(t *testing.T) {
	t.Parallel()

	require.False(t, chat.IsDerivedTitle("", ""))
	require.False(t, chat.IsDerivedTitle("  ", "  "))
}
