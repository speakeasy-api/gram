package transcriptstore

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
)

func TestLikePatternMatchesPostgresILIKE(t *testing.T) {
	t.Parallel()
	f := setup(t, false)
	texts := []string{"", "abc", "ABC", "a_c", "a%c", `a\c`, "xabcx", "Straße", "ÉCOLE école", `{"name": "Lookup"}`, "a\nb"}
	patterns := []string{"%abc%", "%a_c%", `%a\_c%`, `%a\%c%`, `%a\\c%`, "%A%C%", "abc", "a%", "%", "%%", "_bc", "%école%", "%STRASSE%", "%straße%", `%"name": "lookup"%`, "%a_b%", `%\a%`}
	for _, text := range texts {
		for _, pattern := range patterns {
			var want bool
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT $1::text ILIKE $2::text`, text, pattern).Scan(&want)) //nolint:glint // notestingrawsql: compares the Go matcher with Postgres ILIKE on literal values.
			require.Equal(t, want, compileLike(pattern).match(text), "text %q pattern %q", text, pattern)
		}
	}
}

func TestJSONBTextMatchesPostgresJSONBText(t *testing.T) {
	t.Parallel()
	f := setup(t, false)
	values := []string{
		`[]`, `{}`, `null`, `[{"id":"call_a","function":{"name":"lookup","arguments":"{\"q\":1}"}}]`,
		`{"a": [1, 2.50, true, null], "b": {"c": "d"}}`, `"<b>&amp;</b>"`, `{"html":"<a href=\"x\">&</a>"}`,
		`["tab\tnew\nline", "quote \" back \\ slash", "\u0001 \u001f", "  é 😀", "😀", "a\/b"]`,
		`{"key with spaces": " , : ", "k:,": ",:"}`,
	}
	for _, value := range values {
		var want string
		require.NoError(t, f.db.QueryRow(t.Context(), `SELECT $1::jsonb::text`, value).Scan(&want)) //nolint:glint // notestingrawsql: compares the Go layout with PostgreSQL's jsonb text output.
		require.Equal(t, want, jsonbText([]byte(want)), "jsonb text is kept: %s", value)
		// A shadow holds the JSON as encoding/json writes it.
		compact, err := json.Marshal(json.RawMessage(want))
		require.NoError(t, err)
		require.Equal(t, want, jsonbText(compact), "shadow layout: %s", compact)
	}
	require.True(t, compileLike(`%"name": "lookup"%`).matchBody("", []byte(`[{"function":{"name":"lookup"}}]`), nil))
	require.True(t, compileLike(`%<b>%`).matchBody("", nil, []byte(`"<b>"`)))
}

// searchFixture is a chat of n messages, oldest first, with deterministic
// creation times so the chunk boundaries are known.
func (f *fixture) addText(t *testing.T, n int, body func(i int) (content string, toolCalls []byte, contentRaw []byte)) []uuid.UUID {
	t.Helper()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ids := make([]uuid.UUID, n)
	for i := range n {
		ids[i] = uuid.New()
		content, toolCalls, contentRaw := body(i)
		_, err := fixturerepo.New(f.db).CreateTextMessage(t.Context(), ids[i], f.chat, project(f.project), content, contentRaw, toolCalls, pgtype.Timestamptz{Time: base.Add(time.Duration(i) * time.Second), InfinityModifier: pgtype.Finite, Valid: true})
		require.NoError(t, err)
	}
	return ids
}

// searchAll follows a chat search to the end and returns every page.
func searchAll(t *testing.T, transcripts *Transcripts, f *fixture, query string) []ChatSearchPage {
	t.Helper()
	var pages []ChatSearchPage
	cursor := ""
	for {
		page, err := transcripts.SearchChat(t.Context(), f.db, ChatSearch{ProjectID: f.project, ChatID: f.chat, Generation: 0, Query: query, Cursor: cursor, ContextSize: 5, MatchLimit: 200})
		require.NoError(t, err)
		pages = append(pages, page)
		if !page.More {
			return pages
		}
		require.NotEmpty(t, page.NextCursor)
		require.Less(t, len(pages), 10, "the search must end")
		cursor = page.NextCursor
	}
}

func matchedIDs(pages ...ChatSearchPage) []uuid.UUID {
	var ids []uuid.UUID
	for _, page := range pages {
		for _, row := range page.Rows {
			if row.IsMatch && !slices.Contains(ids, row.ID) {
				ids = append(ids, row.ID)
			}
		}
	}
	return ids
}

func TestSearchChatScansShadowsInChunks(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	// Ordinals (1-based) 10, 190, 201, 300 and 420 match. 300 has no shadow,
	// 420 matches only in its tool calls and 190 only in its structured content.
	ids := f.addText(t, 450, func(i int) (string, []byte, []byte) {
		switch i + 1 {
		case 10, 201:
			return fmt.Sprintf("message %d mentions the NeedleWord", i+1), nil, nil
		case 190:
			return "nothing here", nil, []byte(`{"text": "a needleword in the payload"}`)
		case 300:
			return "an unshadowed needleword", nil, nil
		case 420:
			return "a tool call", []byte(`[{"function": {"name": "find_needleword"}}]`), nil
		}
		return fmt.Sprintf("message %d", i+1), nil, nil
	})
	captured := slices.Clone(ids)
	captured = slices.Delete(captured, 299, 300)
	f.capture(t, captured...)

	transcripts := NewTranscripts(f.store, f.project)
	pages := searchAll(t, transcripts, f, "needleword")
	require.Len(t, pages, 3, "450 messages scan in chunks of 200")
	require.Equal(t, []int64{200, 400, 450}, []int64{pages[0].Scanned, pages[1].Scanned, pages[2].Scanned})
	require.ElementsMatch(t, []uuid.UUID{ids[9], ids[189]}, matchedIDs(pages[0]))
	require.ElementsMatch(t, []uuid.UUID{ids[200], ids[299]}, matchedIDs(pages[1]))
	require.ElementsMatch(t, []uuid.UUID{ids[419]}, matchedIDs(pages[2]))

	// The window around ordinal 190 ends at 195 and the one around 201 starts
	// at 196, so the second page repeats 195 to join them into one segment.
	second := pages[1].Rows
	require.Equal(t, int64(195), second[0].Rn)
	require.Equal(t, int64(206), second[11].Rn)
	require.Equal(t, "message 195", second[0].Content)

	// The SQL search over plaintext finds the same matches in one call.
	plain := searchAll(t, NewTranscripts(f.store, uuid.New()), f, "needleword")
	require.Len(t, plain, 1)
	require.ElementsMatch(t, matchedIDs(plain...), matchedIDs(pages...))
	require.ElementsMatch(t, matchedIDs(plain...), matchedIDs(searchAll(t, transcripts, f, "NEEDLE_ORD")...), "'_' is a wildcard as in the SQL search")
	require.Empty(t, matchedIDs(searchAll(t, transcripts, f, `needle\_ord`)...), `'\_' is a literal underscore`)
	require.Empty(t, matchedIDs(searchAll(t, NewTranscripts(f.store, uuid.New()), f, `needle\_ord`)...))

	// A query with jsonb's separator spacing matches the shadow's compact JSON
	// as it matches the plaintext column.
	spaced := `"name": "find_needleword"`
	require.Equal(t, []uuid.UUID{ids[419]}, matchedIDs(searchAll(t, transcripts, f, spaced)...))
	require.Equal(t, []uuid.UUID{ids[419]}, matchedIDs(searchAll(t, NewTranscripts(f.store, uuid.New()), f, spaced)...))
}

func TestSearchChatCountsScannedLikeEntryTotals(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	ids := f.addText(t, 6, func(i int) (string, []byte, []byte) {
		if i == 4 {
			return "message with tool calls", []byte(`[{"id":"call"}]`), nil
		}
		return fmt.Sprintf("message %d", i+1), nil, nil
	})
	// Two plain system messages are left out of the totals; a system message
	// with tool calls and the unshadowed last message are counted.
	fixtures := fixturerepo.New(f.db)
	for _, id := range []uuid.UUID{ids[1], ids[3], ids[4]} {
		_, err := fixtures.SetRole(t.Context(), "system", id, project(f.project))
		require.NoError(t, err)
	}
	f.capture(t, ids[:5]...)

	transcripts := NewTranscripts(f.store, f.project)
	totals, err := transcripts.GetChatEntryTotals(t.Context(), f.db, GetChatEntryTotalsParams{ChatID: f.chat, ProjectID: f.project, Generation: 0, RiskMessageIds: []uuid.UUID{}, RiskContentPartIds: []uuid.UUID{}})
	require.NoError(t, err)
	require.Equal(t, int64(4), totals.Total)
	pages := searchAll(t, transcripts, f, "message")
	require.Len(t, pages, 1)
	require.Equal(t, totals.Total, pages[0].Scanned)
	require.Len(t, matchedIDs(pages...), 6, "system messages are still searched")
}

func TestSearchChatCursorResumesAndStopsAtMatchLimit(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	ids := f.addText(t, 250, func(i int) (string, []byte, []byte) {
		if i%50 == 0 {
			return "needle", nil, nil
		}
		return "hay", nil, nil
	})
	f.capture(t, ids...)
	transcripts := NewTranscripts(f.store, f.project)

	first, err := transcripts.SearchChat(t.Context(), f.db, ChatSearch{ProjectID: f.project, ChatID: f.chat, Generation: 0, Query: "needle", Cursor: "", ContextSize: 0, MatchLimit: 5})
	require.NoError(t, err)
	require.True(t, first.More)
	require.Len(t, matchedIDs(first), 4)

	// Resuming from the cursor twice returns the same page: the cursor is the
	// whole state.
	for range 2 {
		second, err := transcripts.SearchChat(t.Context(), f.db, ChatSearch{ProjectID: f.project, ChatID: f.chat, Generation: 0, Query: "needle", Cursor: first.NextCursor, ContextSize: 0, MatchLimit: 5})
		require.NoError(t, err)
		require.False(t, second.More)
		require.Equal(t, []uuid.UUID{ids[200]}, matchedIDs(second))
	}

	// The fourth match reaches the limit of 4, so the search ends on the first
	// page even though a fifth match lies in the next chunk.
	limited, err := transcripts.SearchChat(t.Context(), f.db, ChatSearch{ProjectID: f.project, ChatID: f.chat, Generation: 0, Query: "needle", Cursor: "", ContextSize: 0, MatchLimit: 4})
	require.NoError(t, err)
	require.False(t, limited.More)
	require.Empty(t, limited.NextCursor)

	for _, bad := range []string{"garbage", chatSearchCursorPrefix + "e30", first.NextCursor + "x"} {
		_, err = transcripts.SearchChat(t.Context(), f.db, ChatSearch{ProjectID: f.project, ChatID: f.chat, Generation: 0, Query: "needle", Cursor: bad, ContextSize: 0, MatchLimit: 5})
		require.ErrorIs(t, err, ErrInvalidCursor, bad)
	}
	_, err = transcripts.SearchChat(t.Context(), f.db, ChatSearch{ProjectID: f.project, ChatID: f.chat, Generation: 1, Query: "needle", Cursor: first.NextCursor, ContextSize: 0, MatchLimit: 5})
	require.ErrorIs(t, err, ErrInvalidCursor, "a cursor belongs to one generation")
}

func TestSearchChatMatchesShadowNotTamperedPlaintext(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	ids := f.addText(t, 3, func(i int) (string, []byte, []byte) {
		return fmt.Sprintf("shadow text %d", i), nil, nil
	})
	f.capture(t, ids...)
	_, err := fixturerepo.New(f.db).SetContent(t.Context(), "plaintext decoy", ids[1], project(f.project))
	require.NoError(t, err)
	transcripts := NewTranscripts(f.store, f.project)
	search := ChatSearch{ProjectID: f.project, ChatID: f.chat, Generation: 0, Query: "", Cursor: "", ContextSize: 0, MatchLimit: 200}
	cursor := chatSearchCursor{Generation: 0, CreatedAtMicros: 0, Seq: 0, WindowEnd: 0, Matches: 0, Scanned: 0}

	// The scan matches the shadow's text and never the edited plaintext.
	search.Query = "shadow text 1"
	chunk, err := transcripts.scanChatChunk(t.Context(), f.db, search, cursor)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{ids[1]}, chunk.matches)
	search.Query = "decoy"
	chunk, err = transcripts.scanChatChunk(t.Context(), f.db, search, cursor)
	require.NoError(t, err)
	require.Empty(t, chunk.matches)
	page, err := transcripts.SearchChat(t.Context(), f.db, search)
	require.NoError(t, err)
	require.Empty(t, page.Rows)

	// Showing the match reads the message for display, which refuses the
	// stale shadow as every read does.
	search.Query = "shadow text 1"
	_, err = transcripts.SearchChat(t.Context(), f.db, search)
	require.ErrorIs(t, err, ErrStale)

	// A project off the list matches the plaintext in SQL.
	search.Query = "decoy"
	page, err = NewTranscripts(f.store, uuid.New()).SearchChat(t.Context(), f.db, search)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{ids[1]}, matchedIDs(page))
}
