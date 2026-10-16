package channels

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/channels/repo"
)

// TestConversationDiscoveryIsPaged is the fix for a snapshot that silently
// capped the conversation list. An organization with more conversations than
// fit in one page must be able to reach all of them, and must be told that
// the first page is not everything.
func TestConversationDiscoveryIsPaged(t *testing.T) {
	t.Parallel()
	db := newDatabase(t, "channels_discovery")
	f := newFixtureOn(t, db)
	f.handler.limits.ConversationPage = 5
	f.enable(t, "alice")
	alice := f.connect(t, "alice", "Author")

	const total = 17
	titles := map[string]bool{}
	for index := range total {
		title := "Conversation " + strconv.Itoa(index)
		titles[title] = true
		thread := f.post(t, "alice", "channels_create_thread", map[string]any{
			"instanceId": alice["instanceId"], "title": title,
		})
		// Give roughly half of them activity, so the page order exercises both
		// the activity sequence and the identifier tiebreak.
		if index%2 == 0 {
			f.post(t, "alice", "channels_post", map[string]any{
				"instanceId": alice["instanceId"], "threadId": thread["id"],
				"text": "activity", "idempotencyKey": "activity-" + strconv.Itoa(index),
			})
		}
	}

	snapshot := f.snapshot(t, "alice")
	firstPage := mapOf(t, snapshot["threads"])
	require.Len(t, firstPage, 5, "the snapshot carries one bounded page")
	require.True(t, boolOf(t, snapshot["threadsHasMore"]), "the snapshot did not admit there are more conversations")
	after := stringOf(t, snapshot["threadsNextAfter"])
	require.NotEmpty(t, after)

	seen := map[string]bool{}
	for id, value := range firstPage {
		seen[id] = true
		require.True(t, titles[stringOf(t, mapOf(t, value)["title"])])
	}
	for range 10 {
		page := f.post(t, "alice", "channels_conversations", map[string]any{"after": after, "limit": 5})
		threads := mapOf(t, page["threads"])
		require.LessOrEqual(t, len(threads), 5)
		for id := range threads {
			require.False(t, seen[id], "conversation %s appeared on two pages", id)
			seen[id] = true
		}
		if !boolOf(t, page["hasMore"]) {
			require.Empty(t, page["nextAfter"], "a final page offered a cursor")
			break
		}
		after = stringOf(t, page["nextAfter"])
		require.NotEmpty(t, after)
	}
	require.Len(t, seen, total, "paging did not reach every conversation")
}

// TestConversationDiscoveryRespectsPrivacy proves discovery is filtered the
// same way the snapshot is; a bigger page must not become a way around it.
func TestConversationDiscoveryRespectsPrivacy(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_discovery_privacy")
	f.enable(t, "alice")
	alice := f.connect(t, "alice", "Alice agent")
	bob := f.connect(t, "bob", "Bob agent")
	direct := f.post(t, "alice", "channels_dm", map[string]any{
		"instanceId": alice["instanceId"], "recipientInstanceId": bob["instanceId"],
	})
	public := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Public",
	})

	outsider := mapOf(t, f.post(t, "outsider", "channels_conversations", map[string]any{})["threads"])
	require.Contains(t, outsider, public["id"])
	require.NotContains(t, outsider, direct["id"], "discovery leaked a private conversation")

	participant := mapOf(t, f.post(t, "bob", "channels_conversations", map[string]any{})["threads"])
	require.Contains(t, participant, direct["id"])

	bad, status := f.try(t, "alice", "channels_conversations", map[string]any{"after": "not-a-cursor"})
	require.Equal(t, 400, status, "%v", bad)
}

// TestConversationCountsAreMaintained is the fix for counting a conversation's
// whole history on every list read. The count is now a maintained column, so
// the proof that matters is that it stays exact.
func TestConversationCountsAreMaintained(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_counts")
	f.enable(t, "alice")
	alice := f.connect(t, "alice", "Author")
	thread := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Counted",
	})
	require.Equal(t, int64(0), threadCount(t, f, "alice", thread["id"]), "a new conversation starts at zero")

	for index := range 4 {
		f.post(t, "alice", "channels_post", map[string]any{
			"instanceId": alice["instanceId"], "threadId": thread["id"],
			"text": "counted", "idempotencyKey": "counted-" + strconv.Itoa(index),
		})
	}
	require.Equal(t, int64(4), threadCount(t, f, "alice", thread["id"]))

	// An idempotent replay writes nothing, so it must not count.
	f.post(t, "alice", "channels_post", map[string]any{
		"instanceId": alice["instanceId"], "threadId": thread["id"],
		"text": "counted", "idempotencyKey": "counted-0",
	})
	require.Equal(t, int64(4), threadCount(t, f, "alice", thread["id"]), "a replay inflated the count")

	// Retention takes messages away, so it has to take them off the count too.
	err := repo.New(f.db).FixtureAgeMessages(t.Context(), repo.FixtureAgeMessagesParams{OrganizationID: testOrg, AgeDays: 200, ThroughSeq: 2})
	require.NoError(t, err)
	swept, err := f.handler.RunRetention(t.Context(), time.Now())
	require.NoError(t, err)
	require.Equal(t, 2, swept.Messages)
	require.Equal(t, int64(2), threadCount(t, f, "alice", thread["id"]), "retention left a stale count behind")
}

func threadCount(t *testing.T, f *fixture, who string, threadID any) int64 {
	t.Helper()
	threads := mapOf(t, f.snapshot(t, who)["threads"])
	thread := mapOf(t, threads[stringOf(t, threadID)])
	return intOf(t, thread["messageCount"])
}

// TestHistoryStalenessIsPerConversation is the fix for an organization-wide
// retention floor standing in for a per-conversation fact. A conversation
// created after a sweep has lost nothing and must not be told it has.
func TestHistoryStalenessIsPerConversation(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_stale_scope")
	f.enable(t, "alice")
	alice := f.connect(t, "alice", "Author")
	aged := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Aged",
	})
	for index := range 3 {
		f.post(t, "alice", "channels_post", map[string]any{
			"instanceId": alice["instanceId"], "threadId": aged["id"],
			"text": "old", "idempotencyKey": "old-" + strconv.Itoa(index),
		})
	}
	err := repo.New(f.db).FixtureAgeMessages(t.Context(), repo.FixtureAgeMessagesParams{OrganizationID: testOrg, AgeDays: 200, ThroughSeq: 2})
	require.NoError(t, err)
	swept, err := f.handler.RunRetention(t.Context(), time.Now())
	require.NoError(t, err)
	require.Equal(t, 2, swept.Messages)

	// A conversation that actually lost messages says so.
	trimmed := f.post(t, "alice", "channels_history", map[string]any{"threadId": aged["id"]})
	require.Equal(t, []int64{3}, messageIDs(t, trimmed["messages"]))
	require.False(t, boolOf(t, trimmed["hasMore"]))
	require.True(t, boolOf(t, trimmed["stale"]), "a conversation that lost history did not report it")

	// A conversation created afterwards has lost nothing, even though the
	// organization's retention floor is above zero.
	fresh := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Fresh",
	})
	f.post(t, "alice", "channels_post", map[string]any{
		"instanceId": alice["instanceId"], "threadId": fresh["id"],
		"text": "new", "idempotencyKey": "new-1",
	})
	page := f.post(t, "alice", "channels_history", map[string]any{"threadId": fresh["id"]})
	require.Len(t, messageIDs(t, page["messages"]), 1)
	require.False(t, boolOf(t, page["hasMore"]))
	require.False(t, boolOf(t, page["stale"]), "a conversation created after a sweep was told its history was deleted")

	// A conversation that never lost anything and has nothing above it either.
	empty := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Empty",
	})
	page = f.post(t, "alice", "channels_history", map[string]any{"threadId": empty["id"]})
	require.Empty(t, messageIDs(t, page["messages"]))
	require.False(t, boolOf(t, page["stale"]))
}
