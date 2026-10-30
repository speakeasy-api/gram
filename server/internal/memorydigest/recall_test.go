package memorydigest_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/memorydigest"
)

func TestUncoveredShowsEachPeriodAtItsMostCompressedLevel(t *testing.T) {
	t.Parallel()

	chatInDay := uuid.New()
	chatToday := uuid.New()
	dayInWeek := memorydigest.Digest{ID: uuid.New(), Level: memorydigest.LevelDay, PeriodStart: date(2026, 10, 5), Content: "day in covered week", ChatIDs: []uuid.UUID{chatInDay}}
	weekInMonth := memorydigest.Digest{ID: uuid.New(), Level: memorydigest.LevelWeek, PeriodStart: date(2026, 10, 5), Content: "week in month", ChildDigestIDs: []uuid.UUID{dayInWeek.ID}}
	month := memorydigest.Digest{ID: uuid.New(), Level: memorydigest.LevelMonth, PeriodStart: date(2026, 10, 1), Content: "October", ChildDigestIDs: []uuid.UUID{weekInMonth.ID}}
	openWeek := memorydigest.Digest{ID: uuid.New(), Level: memorydigest.LevelWeek, PeriodStart: date(2026, 11, 2), Content: "first week of November"}
	openDay := memorydigest.Digest{ID: uuid.New(), Level: memorydigest.LevelDay, PeriodStart: date(2026, 11, 9), Content: "Monday"}

	entries := memorydigest.Uncovered(
		[]memorydigest.Digest{openDay, dayInWeek, month, openWeek, weekInMonth},
		[]memorydigest.ChatNote{
			{ID: chatInDay, SummarizedAt: date(2026, 10, 5), Summary: "covered"},
			{ID: chatToday, Title: "Retry\n## queue", SummarizedAt: date(2026, 11, 10), Summary: "Picked queue X.\n\nMore detail."},
		},
	)

	var ids []uuid.UUID
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	require.Equal(t, []uuid.UUID{month.ID, openWeek.ID, openDay.ID, chatToday}, ids)
	require.Equal(t, "October 2026 (month digest)", entries[0].Heading)
	require.Equal(t, memorydigest.SourceChat, entries[3].Kind)
	require.Equal(t, "Chat 2026-11-10: Retry ## queue", entries[3].Heading)
	require.Equal(t, "Picked queue X.", entries[3].Text)
}

func TestBudgetDropsOldestFirst(t *testing.T) {
	t.Parallel()

	var entries []memorydigest.Entry
	for i := range 10 {
		entries = append(entries, memorydigest.Entry{
			Kind:    memorydigest.SourceDigest,
			ID:      uuid.New(),
			Heading: "h",
			At:      date(2026, 10, 1).AddDate(0, 0, i),
			Text:    strings.Repeat("x", 396), // 99 tokens, plus 1 for the heading
		})
	}

	kept, dropped := memorydigest.Budget(entries, 350)
	require.Equal(t, 7, dropped)
	require.Equal(t, entries[7:], kept)

	kept, dropped = memorydigest.Budget(entries, 10_000)
	require.Zero(t, dropped)
	require.Len(t, kept, 10)

	// Order comes from At, not from the slice.
	shuffled := []memorydigest.Entry{entries[9], entries[0], entries[8]}
	kept, dropped = memorydigest.Budget(shuffled, 250)
	require.Equal(t, 1, dropped)
	require.Equal(t, []memorydigest.Entry{entries[8], entries[9]}, kept)

	kept, dropped = memorydigest.Budget(nil, 100)
	require.Zero(t, dropped)
	require.Empty(t, kept)
}

func TestChatSnippetIsCut(t *testing.T) {
	t.Parallel()

	entries := memorydigest.Uncovered(nil, []memorydigest.ChatNote{
		{ID: uuid.New(), SummarizedAt: time.Now(), Summary: strings.Repeat("y", 2_000)},
	})
	require.Len(t, entries, 1)
	require.Len(t, []rune(entries[0].Text), 600)
	require.True(t, strings.HasSuffix(entries[0].Text, "…"))
}

func TestRender(t *testing.T) {
	t.Parallel()

	digestID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	chatID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	out := memorydigest.Render([]memorydigest.Entry{
		{Kind: memorydigest.SourceDigest, ID: digestID, Heading: "October 2026 (month digest)", Text: "Picked queue X."},
		{Kind: memorydigest.SourceChat, ID: chatID, Heading: "Chat 2026-11-10", Text: "Shipped retries."},
	}, 3)

	require.Equal(t, "(3 older entries omitted to fit the budget; use search_team_memory for older history.)\n\n"+
		"## October 2026 (month digest) [digest:00000000-0000-0000-0000-000000000001]\nPicked queue X.\n\n"+
		"## Chat 2026-11-10 [chat:00000000-0000-0000-0000-000000000002]\nShipped retries.", out)
	require.Empty(t, memorydigest.Render(nil, 0))
}
