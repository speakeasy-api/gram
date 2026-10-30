package memorydigest_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/memorydigest"
)

func TestPromptEmptyPeriodSkipsLLM(t *testing.T) {
	t.Parallel()

	_, _, ok := memorydigest.LevelDay.Prompt(date(2026, 10, 29), nil)
	require.False(t, ok)
}

func TestDayPrompt(t *testing.T) {
	t.Parallel()

	system, user, ok := memorydigest.LevelDay.Prompt(date(2026, 10, 29), []memorydigest.Input{
		{Kind: memorydigest.SourceChat, At: date(2026, 10, 29), Title: "Retry queue", Text: "We picked queue X for retries."},
		{Kind: memorydigest.SourceMemo, At: date(2026, 10, 29), Author: "Ada", Text: "Queue X because of ordering."},
	})
	require.True(t, ok)
	require.Contains(t, system, "day digest of a software team's work for 2026-10-29")
	require.Contains(t, system, "chat summaries and team memos")
	require.Contains(t, system, "under 200 words")
	require.Contains(t, system, "Never guess or restore a masked value")
	require.Contains(t, user, "### chat: Retry queue · 2026-10-29\nWe picked queue X for retries.")
	require.Contains(t, user, "### memo (saved by Ada) · 2026-10-29\nQueue X because of ordering.")
	require.Less(t, strings.Index(user, "### chat"), strings.Index(user, "### memo"))
}

func TestRollupPromptNamesChildLevel(t *testing.T) {
	t.Parallel()

	system, _, ok := memorydigest.LevelMonth.Prompt(date(2026, 10, 1), []memorydigest.Input{
		{Kind: memorydigest.SourceDigest, At: date(2026, 10, 5), Title: "week of 2026-10-05", Text: "w"},
	})
	require.True(t, ok)
	require.Contains(t, system, "month digest of a software team's work for October 2026")
	require.Contains(t, system, "week digests")
}

func TestPromptKeepsNewestWhenOverLong(t *testing.T) {
	t.Parallel()

	var inputs []memorydigest.Input
	for i := range 80 {
		inputs = append(inputs, memorydigest.Input{
			Kind: memorydigest.SourceChat,
			At:   date(2026, 10, 29),
			Text: strings.Repeat(string(rune('a'+i%26)), 2_400),
		})
	}
	inputs[0].Title = "oldest"
	inputs[79].Title = "newest"

	_, user, ok := memorydigest.LevelDay.Prompt(date(2026, 10, 29), inputs)
	require.True(t, ok)
	require.LessOrEqual(t, len([]rune(user)), 120_000)
	require.Contains(t, user, "newest")
	require.NotContains(t, user, "oldest")
	require.True(t, strings.HasSuffix(user, strings.Repeat(string(rune('a'+79%26)), 2_400)))
}

func TestPromptOrdersByAtAndCapsAuthor(t *testing.T) {
	t.Parallel()

	_, user, ok := memorydigest.LevelDay.Prompt(date(2026, 10, 29), []memorydigest.Input{
		{Kind: memorydigest.SourceMemo, At: date(2026, 10, 29).Add(2 * time.Hour), Author: strings.Repeat("a", 500), Text: "later"},
		{Kind: memorydigest.SourceChat, At: date(2026, 10, 29).Add(time.Hour), Text: "earlier"},
	})
	require.True(t, ok)
	require.Less(t, strings.Index(user, "earlier"), strings.Index(user, "later"))
	require.NotContains(t, user, strings.Repeat("a", 201))
}
