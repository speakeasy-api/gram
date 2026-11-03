package memoryeval_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	digesttestrepo "github.com/speakeasy-api/gram/server/internal/memorydigest/memorydigesttest/repo"
	digestrepo "github.com/speakeasy-api/gram/server/internal/memorydigest/repo"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/bench"
)

// TestMemorySourcesResolvesMemosAndDigestTrees checks that a memo citation
// resolves to the chat it was saved from, and a month digest to every chat
// below it: its day digest's chats and the chats of that day's memos.
func TestMemorySourcesResolvesMemosAndDigestTrees(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	corpus := memoryeval.Build(corpusNow)
	loaded := loadCorpus(t, "memoryeval_sources", corpus)
	target := loaded.target()
	project := target.Projects[memoryeval.ProjectMain].ID
	chatID := func(key string) uuid.UUID {
		chat, ok := corpus.Chat(key)
		require.True(t, ok, key)
		return chat.ID
	}

	memo, err := digesttestrepo.New(loaded.db).InsertMemoFixture(ctx, digesttestrepo.InsertMemoFixtureParams{
		OrganizationID: loaded.organizationID,
		ProjectID:      project,
		ChatID:         uuid.NullUUID{UUID: chatID("reversal-pooler"), Valid: true},
		Content:        "We switched to PgCat.",
		CreatedAt:      pgtype.Timestamptz{Time: corpusNow, Valid: true, InfinityModifier: pgtype.Finite},
	})
	require.NoError(t, err)

	digests := digestrepo.New(loaded.db)
	insert := func(level string, start time.Time, end time.Time, chats, memos, children []uuid.UUID) uuid.UUID {
		// The array columns are NOT NULL, so an absent list is empty.
		for _, list := range []*[]uuid.UUID{&chats, &memos, &children} {
			if *list == nil {
				*list = []uuid.UUID{}
			}
		}
		id, err := digests.InsertDigest(ctx, digestrepo.InsertDigestParams{
			OrganizationID: loaded.organizationID, ProjectID: project, Level: level,
			PeriodStart: pgtype.Date{Time: start, Valid: true, InfinityModifier: pgtype.Finite},
			PeriodEnd:   pgtype.Date{Time: end, Valid: true, InfinityModifier: pgtype.Finite},
			Content:     level + " digest", ChatIds: chats, MemoIds: memos, ChildDigestIds: children,
			Model: pgtype.Text{}, InputTokens: pgtype.Int4{}, OutputTokens: pgtype.Int4{},
		})
		require.NoError(t, err)
		return id
	}
	day := time.Date(2026, time.October, 5, 0, 0, 0, 0, time.UTC) // a Monday
	dayID := insert("day", day, day.AddDate(0, 0, 1), []uuid.UUID{chatID("decision-pooler")}, []uuid.UUID{memo}, nil)
	weekID := insert("week", day, day.AddDate(0, 0, 7), nil, nil, []uuid.UUID{dayID})
	monthStart := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	monthID := insert("month", monthStart, monthStart.AddDate(0, 1, 0), nil, nil, []uuid.UUID{weekID})

	sources := bench.MemorySources(target)

	chats, err := sources(ctx, "memo", memo)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{chatID("reversal-pooler")}, chats)

	chats, err = sources(ctx, "digest", monthID)
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{chatID("decision-pooler"), chatID("reversal-pooler")}, chats)

	chats, err = sources(ctx, "digest", uuid.New())
	require.NoError(t, err)
	require.Empty(t, chats, "an unknown digest has no sources")

	_, err = sources(ctx, "chat", chatID("decision-pooler"))
	require.Error(t, err, "chat citations need no lookup")

	_, err = loaded.db.Exec(ctx, "UPDATE memory_memos SET deleted_at = clock_timestamp() WHERE id = $1", memo)
	require.NoError(t, err)

	chats, err = sources(ctx, "memo", memo)
	require.NoError(t, err)
	require.Empty(t, chats, "a deleted memo no longer backs a citation")

	chats, err = sources(ctx, "digest", monthID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{chatID("decision-pooler")}, chats, "a digest drops the chats of its deleted memos")
}
