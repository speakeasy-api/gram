package memorydigest_test

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/memorydigest"
)

func TestPromptEmptyPeriodSkipsLLM(t *testing.T) {
	t.Parallel()

	_, _, used := memorydigest.LevelDay.Prompt(date(2026, 10, 29), nil)
	require.Empty(t, used)
}

func TestDayPrompt(t *testing.T) {
	t.Parallel()

	system, user, used := memorydigest.LevelDay.Prompt(date(2026, 10, 29), []memorydigest.Input{
		{Kind: memorydigest.SourceChat, At: date(2026, 10, 29), Text: "We picked queue X for retries."},
		{Kind: memorydigest.SourceMemo, At: date(2026, 10, 29), Text: "Queue X because of ordering."},
	})
	require.Len(t, used, 2)
	require.Contains(t, system, "day digest of a software team's work for 2026-10-29")
	require.Contains(t, system, "chat summaries and team memos")
	require.Contains(t, system, "under 200 words")
	require.Contains(t, system, "Never guess or restore a masked value")
	require.Contains(t, user, "### chat · 2026-10-29\nWe picked queue X for retries.")
	require.Contains(t, user, "### memo · 2026-10-29\nQueue X because of ordering.")
	require.Less(t, strings.Index(user, "### chat"), strings.Index(user, "### memo"))
}

func TestRollupPromptNamesChildLevel(t *testing.T) {
	t.Parallel()

	system, user, used := memorydigest.LevelMonth.Prompt(date(2026, 10, 1), []memorydigest.Input{
		{Kind: memorydigest.SourceDigest, At: date(2026, 10, 5), Title: "week of 2026-10-05", Text: "w"},
	})
	require.Len(t, used, 1)
	require.Contains(t, user, "### digest: week of 2026-10-05 · 2026-10-05\nw")
	require.Contains(t, system, "month digest of a software team's work for October 2026")
	require.Contains(t, system, "week digests")
}

func TestPromptKeepsNewestWhenOverLong(t *testing.T) {
	t.Parallel()

	var inputs []memorydigest.Input
	for i := range 80 {
		inputs = append(inputs, memorydigest.Input{
			Kind: memorydigest.SourceChat,
			ID:   uuid.New(),
			At:   date(2026, 10, 29).Add(time.Duration(i) * time.Minute),
			Text: strings.Repeat(string(rune('a'+i%26)), 2_400),
		})
	}
	inputs[0].Title = "oldest"
	inputs[79].Title = "newest"
	ordered := slices.Clone(inputs)
	// Shuffle so capping before sorting would keep the wrong items.
	rand.New(rand.NewPCG(1, 2)).Shuffle(len(inputs), func(i, j int) { inputs[i], inputs[j] = inputs[j], inputs[i] })

	_, user, used := memorydigest.LevelDay.Prompt(date(2026, 10, 29), inputs)
	// Only the inputs the prompt holds count as covered: the newest ones.
	require.NotEmpty(t, used)
	require.Less(t, len(used), len(inputs))
	require.Equal(t, ordered[len(ordered)-len(used):], used)
	require.LessOrEqual(t, memorydigest.EstimateTokens(user), 30_000)
	require.Contains(t, user, "newest")
	require.NotContains(t, user, "oldest")
	require.True(t, strings.HasSuffix(user, strings.Repeat(string(rune('a'+79%26)), 2_400)))
}

func TestPromptOrdersByAtAndCapsTitle(t *testing.T) {
	t.Parallel()

	_, user, used := memorydigest.LevelWeek.Prompt(date(2026, 10, 26), []memorydigest.Input{
		{Kind: memorydigest.SourceDigest, At: date(2026, 10, 29), Title: strings.Repeat("a", 500), Text: "later"},
		{Kind: memorydigest.SourceDigest, At: date(2026, 10, 28), Text: "earlier"},
	})
	require.Len(t, used, 2)
	require.Equal(t, "earlier", used[0].Text)
	require.Contains(t, user, "earlier")
	require.Contains(t, user, "later")
	require.Less(t, strings.Index(user, "earlier"), strings.Index(user, "later"))
	require.NotContains(t, user, strings.Repeat("a", 201))
}

func TestPromptCapsNonASCIIByTokens(t *testing.T) {
	t.Parallel()

	var inputs []memorydigest.Input
	for i := range 40 {
		inputs = append(inputs, memorydigest.Input{
			Kind: memorydigest.SourceChat,
			ID:   uuid.New(),
			At:   date(2026, 10, 29).Add(time.Duration(i) * time.Minute),
			Text: strings.Repeat("記", 2_400),
		})
	}
	_, user, used := memorydigest.LevelDay.Prompt(date(2026, 10, 29), inputs)
	require.Less(t, len(used), len(inputs))
	require.LessOrEqual(t, memorydigest.EstimateTokens(user), 30_000)
}
