package channels

import (
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/channels/repo"
)

// TestConversationsAndIsolation covers the product's access rules end to end
// against real PostgreSQL: who may read a private conversation, who may write
// to it, and that neither crosses an organization boundary.
func TestConversationsAndIsolation(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_isolation")
	f.enable(t, "alice")
	f.enable(t, "stranger")

	alice := f.connect(t, "alice", "Alice agent")
	bob := f.connect(t, "bob", "Bob agent")

	direct := f.post(t, "bob", "channels_dm", map[string]any{
		"instanceId":          bob["instanceId"],
		"recipientInstanceId": alice["instanceId"],
	})
	require.Equal(t, sortedEndpoints(t, []any{alice["instanceId"], bob["instanceId"]}), sortedEndpoints(t, direct["instanceIds"]), "a direct conversation records both endpoints")

	// The same pair of endpoints reuses one conversation rather than piling up
	// a new one on every call.
	again := f.post(t, "alice", "channels_dm", map[string]any{
		"instanceId":          alice["instanceId"],
		"recipientInstanceId": bob["instanceId"],
	})
	require.Equal(t, direct["id"], again["id"], "the same endpoint pair reused its conversation")

	sent := f.post(t, "bob", "channels_post", map[string]any{
		"instanceId": bob["instanceId"], "threadId": direct["id"],
		"text": "Private handoff", "idempotencyKey": "dm-1",
	})
	require.Equal(t, sent["id"], f.post(t, "bob", "channels_post", map[string]any{
		"instanceId": bob["instanceId"], "threadId": direct["id"],
		"text": "Private handoff", "idempotencyKey": "dm-1",
	})["id"], "a retry returned the original message")

	// A reused key with different content is a conflict, not a silent return
	// of an unrelated message.
	body, status := f.try(t, "bob", "channels_post", map[string]any{
		"instanceId": bob["instanceId"], "threadId": direct["id"],
		"text": "Different content", "idempotencyKey": "dm-1",
	})
	require.Equal(t, http.StatusBadRequest, status, "%v", body)

	require.Empty(t, messageIDs(t, f.post(t, "outsider", "channels_read", map[string]any{})["messages"]), "an uninvolved member read a private conversation")
	require.Empty(t, messageIDs(t, f.post(t, "stranger", "channels_read", map[string]any{})["messages"]), "another organization read this organization's messages")
	require.Len(t, messageIDs(t, f.post(t, "observer", "channels_read", map[string]any{})["messages"]), 1, "read-all could not observe the private conversation")

	// Observation is not participation: read-all never grants writing.
	observer := f.connect(t, "observer", "Observer agent")
	body, status = f.try(t, "observer", "channels_post", map[string]any{
		"instanceId": observer["instanceId"], "threadId": direct["id"],
		"text": "Not a participant", "idempotencyKey": "intrusion",
	})
	require.Equal(t, http.StatusForbidden, status, "%v", body)

	// A branch of a private conversation inherits its restrictions.
	branch := f.post(t, "bob", "channels_create_thread", map[string]any{
		"instanceId": bob["instanceId"], "title": "Branch", "parentId": direct["id"],
	})
	body, status = f.try(t, "outsider", "channels_history", map[string]any{"threadId": branch["id"]})
	require.Equal(t, http.StatusForbidden, status, "a private branch leaked to an uninvolved member: %v", body)

	// The outsider's own view holds nothing of the private conversation.
	outsiderThreads := mapOf(t, f.snapshot(t, "outsider")["threads"])
	require.NotContains(t, outsiderThreads, direct["id"])
	require.NotContains(t, outsiderThreads, branch["id"])
}

// TestSnapshotIsBoundedAndPaged proves the bootstrap payload stays bounded and
// that older history is reachable only through explicit paging.
func TestSnapshotIsBoundedAndPaged(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_bounded")
	f.enable(t, "alice")
	alice := f.connect(t, "alice", "Alice agent")
	thread := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Long thread",
	})
	const total = 120
	for index := range total {
		f.post(t, "alice", "channels_post", map[string]any{
			"instanceId": alice["instanceId"], "threadId": thread["id"],
			"text": "message", "idempotencyKey": "bulk-" + strconv.Itoa(index),
		})
	}

	snapshot := f.snapshot(t, "alice")
	tail := messageIDs(t, snapshot["messages"])
	require.Len(t, tail, 50, "the bootstrap tail is bounded")
	require.True(t, boolOf(t, snapshot["hasMore"]), "older messages exist beyond the tail")
	require.Equal(t, int64(total), intOf(t, snapshot["cursor"]), "the cursor names the newest message in the tail")
	for index := 1; index < len(tail); index++ {
		require.Greater(t, tail[index], tail[index-1], "the tail is ascending")
	}
	limits := mapOf(t, snapshot["limits"])
	require.Equal(t, int64(32768), intOf(t, limits["messageBytes"]), "snapshot publishes its limits")

	// Each conversation carries its newest message so a list needs no
	// per-conversation history call.
	threads := mapOf(t, snapshot["threads"])
	listed := mapOf(t, threads[stringOf(t, thread["id"])])
	require.Equal(t, int64(total), intOf(t, listed["messageCount"]))
	last := mapOf(t, listed["lastMessage"])
	require.Equal(t, int64(total), intOf(t, last["id"]), "a conversation carries its newest message")

	// Paging backwards walks the whole archive without ever asking for it all.
	seen := map[int64]bool{}
	before := int64(0)
	for range 10 {
		page := f.post(t, "alice", "channels_history", map[string]any{
			"threadId": thread["id"], "before": before, "limit": 25,
		})
		ids := messageIDs(t, page["messages"])
		require.LessOrEqual(t, len(ids), 25)
		for index := 1; index < len(ids); index++ {
			require.Greater(t, ids[index], ids[index-1], "history is ascending")
		}
		for _, id := range ids {
			require.False(t, seen[id], "history repeated message %d", id)
			seen[id] = true
		}
		if !boolOf(t, page["hasMore"]) {
			break
		}
		before = intOf(t, page["nextBefore"])
		require.NotZero(t, before)
	}
	require.Len(t, seen, total, "paging reached every message")
}

// TestForwardReadIsIncremental covers the cursor contract clients poll with.
func TestForwardReadIsIncremental(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_forward")
	f.enable(t, "alice")
	alice := f.connect(t, "alice", "Alice agent")
	thread := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Forward",
	})
	for index := range 5 {
		f.post(t, "alice", "channels_post", map[string]any{
			"instanceId": alice["instanceId"], "threadId": thread["id"],
			"text": "message", "idempotencyKey": "forward-" + strconv.Itoa(index),
		})
	}
	first := f.post(t, "alice", "channels_read", map[string]any{"cursor": 0, "limit": 2})
	require.Len(t, messageIDs(t, first["messages"]), 2)
	require.True(t, boolOf(t, first["hasMore"]), "more is already available")
	require.False(t, boolOf(t, first["stale"]))

	rest := f.post(t, "alice", "channels_read", map[string]any{"cursor": first["cursor"]})
	require.Len(t, messageIDs(t, rest["messages"]), 3)
	require.False(t, boolOf(t, rest["hasMore"]))

	caughtUp := f.post(t, "alice", "channels_read", map[string]any{"cursor": rest["cursor"]})
	require.Empty(t, messageIDs(t, caughtUp["messages"]))
	require.Equal(t, rest["cursor"], caughtUp["cursor"], "an idle read does not move the cursor")
}

// TestSubscriptionsDeliverAcrossHandlers proves the shared listener fans one
// write out to subscribers on a different handler instance, which is what a
// multi-replica deployment depends on.
func TestSubscriptionsDeliverAcrossHandlers(t *testing.T) {
	t.Parallel()
	db := newDatabase(t, "channels_fanout")
	writer := newFixtureOn(t, db)
	reader := newFixtureOn(t, db)

	writer.enable(t, "alice")
	author := writer.connect(t, "alice", "Author")
	thread := writer.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": author["instanceId"], "title": "Shared",
	})
	// A public thread notifies only its followers.
	reader.post(t, "bob", "channels_follow", map[string]any{"threadId": thread["id"], "follow": true})

	subscription := reader.listen(t, "bob", "cross-handler", "channels://messages")
	ack := subscription.next(t)
	require.Equal(t, "notifications/subscriptions/acknowledged", ack["method"])

	writer.post(t, "alice", "channels_post", map[string]any{
		"instanceId": author["instanceId"], "threadId": thread["id"],
		"text": "Written on another handler", "idempotencyKey": "fanout-1",
	})
	update := subscription.next(t)
	require.Equal(t, "notifications/resources/updated", update["method"])
	params := mapOf(t, update["params"])
	require.Equal(t, "channels://messages", params["uri"])
	require.Len(t, params, 2, "an invalidation carries a URI and metadata only, never content")
}

// TestNotificationsRespectFollowsAndMutes covers the delivery rules the tool
// descriptions promise.
func TestNotificationsRespectFollowsAndMutes(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_delivery")
	f.enable(t, "alice")
	alice := f.connect(t, "alice", "Alice agent")
	bob := f.connect(t, "bob", "Bob agent")
	public := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Public",
	})
	direct := f.post(t, "alice", "channels_dm", map[string]any{
		"instanceId": alice["instanceId"], "recipientInstanceId": bob["instanceId"],
	})

	subscription := f.listen(t, "bob", "delivery", "channels://messages")
	require.Equal(t, "notifications/subscriptions/acknowledged", subscription.next(t)["method"])

	// An unfollowed public thread is silent.
	f.post(t, "alice", "channels_post", map[string]any{
		"instanceId": alice["instanceId"], "threadId": public["id"],
		"text": "Unfollowed", "idempotencyKey": "public-1",
	})
	subscription.silent(t, 3*time.Second)

	// A direct conversation notifies without a follow.
	f.post(t, "alice", "channels_post", map[string]any{
		"instanceId": alice["instanceId"], "threadId": direct["id"],
		"text": "Direct", "idempotencyKey": "dm-1",
	})
	require.Equal(t, "notifications/resources/updated", subscription.next(t)["method"])

	// Muting suppresses notifications but never hides durable reads.
	f.post(t, "bob", "channels_follow", map[string]any{"threadId": direct["id"], "muted": true})
	f.post(t, "alice", "channels_post", map[string]any{
		"instanceId": alice["instanceId"], "threadId": direct["id"],
		"text": "Muted", "idempotencyKey": "dm-2",
	})
	subscription.silent(t, 3*time.Second)
	require.Len(t, messageIDs(t, f.post(t, "bob", "channels_read", map[string]any{"threadId": direct["id"]})["messages"]), 2,
		"muting hid messages from an explicit read")
}

// TestLeaseRenewalIsNotAnAnnouncement proves a heartbeat does not look like a
// new participant arriving.
func TestLeaseRenewalIsNotAnAnnouncement(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_presence")
	f.enable(t, "alice")
	alice := f.connect(t, "alice", "Alice agent")

	subscription := f.listen(t, "bob", "presence", "channels://presence")
	require.Equal(t, "notifications/subscriptions/acknowledged", subscription.next(t)["method"])

	f.post(t, "alice", "channels_presence", map[string]any{"instanceId": alice["instanceId"]})
	f.post(t, "alice", "channels_connect", map[string]any{
		"instanceId": alice["instanceId"], "resumeToken": alice["resumeToken"],
	})
	subscription.silent(t, 3*time.Second)

	// A genuinely new instance is announced.
	f.connect(t, "bob", "Bob agent")
	require.Equal(t, "notifications/resources/updated", subscription.next(t)["method"])
}

// TestLiveRevocationCompletesStreams proves authorization is rechecked during
// delivery, not only at admission.
func TestLiveRevocationCompletesStreams(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_revocation")
	f.enable(t, "alice")

	subscription := f.listen(t, "bob", "revocation", "channels://presence")
	require.Equal(t, "notifications/subscriptions/acknowledged", subscription.next(t)["method"])

	f.post(t, "alice", "channels_admin", map[string]any{"subjectId": "bob", "canConnect": false})
	completion := subscription.next(t)
	result := mapOf(t, completion["result"])
	require.Equal(t, "complete", result["resultType"], "revocation did not complete the stream: %v", completion)

	// Reads fail closed too, and the client is told it lost access rather than
	// that its request was malformed.
	body, status := f.try(t, "bob", "channels_read", map[string]any{})
	require.Equal(t, http.StatusForbidden, status, "%v", body)
}

// TestDisabledOrganizationStillConfigurable covers the administrator's view of
// a disabled organization: configuration, but no content.
func TestDisabledOrganizationStillConfigurable(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_disabled")
	body, status := f.try(t, "bob", "channels_read", map[string]any{})
	require.Equal(t, http.StatusForbidden, status, "channels is disabled by default: %v", body)

	snapshot := f.snapshot(t, "alice")
	require.Equal(t, false, snapshot["enabled"])
	require.Empty(t, snapshot["threads"])
	require.Empty(t, snapshot["messages"])
	require.NotNil(t, snapshot["grants"], "an administrator can still inspect configuration")

	f.enable(t, "alice")
	require.Equal(t, true, f.snapshot(t, "alice")["enabled"])
}

// TestDashboardSessionsAreSecretless covers the browser session rules.
func TestDashboardSessionsAreSecretless(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_dashboard")
	f.enable(t, "alice")

	// Reading creates nothing.
	f.snapshot(t, "alice")
	require.Empty(t, f.snapshot(t, "alice")["instances"], "browsing joined presence")

	session := f.post(t, "alice", "channels_dashboard_session", map[string]any{})
	require.Equal(t, "Alice Example on dashboard", session["label"], "the label comes from the account, not the request")
	require.Empty(t, session["resumeToken"], "a browser session never receives a resume secret")

	again := f.post(t, "alice", "channels_dashboard_session", map[string]any{})
	require.Equal(t, session["instanceId"], again["instanceId"], "re-entering the dashboard reclaimed the same session")

	// The operation is refused on MCP, and no token can resume the session.
	denied := f.rpc(t, "alice", "tools/call", map[string]any{
		"name": "channels_dashboard_session", "arguments": map[string]any{},
	})
	require.NotNil(t, denied["error"], "an MCP client opened a dashboard session")
	body, status := f.try(t, "alice", "channels_connect", map[string]any{
		"instanceId": session["instanceId"], "resumeToken": "guessed",
	})
	require.Equal(t, http.StatusForbidden, status, "%v", body)

	// Another account never touches it.
	bobSession := f.post(t, "bob", "channels_dashboard_session", map[string]any{})
	require.NotEqual(t, session["instanceId"], bobSession["instanceId"])
}

// TestResumeRequiresTheStoredSecret covers agent instance resumption.
func TestResumeRequiresTheStoredSecret(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_resume")
	f.enable(t, "alice")
	alice := f.post(t, "alice", "channels_connect", map[string]any{"label": "Agent", "note": "Introduction"})
	require.NotEmpty(t, alice["resumeToken"], "a new agent instance receives its secret once")

	resumed := f.post(t, "alice", "channels_connect", map[string]any{
		"instanceId": alice["instanceId"], "resumeToken": alice["resumeToken"],
	})
	require.Equal(t, alice["instanceId"], resumed["instanceId"])
	require.Empty(t, resumed["resumeToken"], "resuming never issues a replacement secret")
	require.Equal(t, "Introduction", resumed["note"], "a heartbeat erased the introduction")

	// Only the plaintext secret works, and only for its owner.
	for _, attempt := range []map[string]any{
		{"instanceId": alice["instanceId"], "resumeToken": "wrong"},
		{"instanceId": alice["instanceId"]},
	} {
		body, status := f.try(t, "alice", "channels_connect", attempt)
		require.Equal(t, http.StatusForbidden, status, "%v", body)
	}
	body, status := f.try(t, "bob", "channels_connect", map[string]any{
		"instanceId": alice["instanceId"], "resumeToken": alice["resumeToken"],
	})
	require.Equal(t, http.StatusForbidden, status, "another account resumed someone else's instance: %v", body)

	// Only the hash is stored.
	stored, err := repo.New(f.db).GetInstance(t.Context(), repo.GetInstanceParams{OrganizationID: testOrg, ID: mustUUID(t, alice["instanceId"])})
	require.NoError(t, err)
	require.NotEqual(t, alice["resumeToken"], stored.ResumeHash.String)
	require.Len(t, stored.ResumeHash.String, 64)
}

// TestStorageOwnershipIsTheDefaultProject pins the documented tenancy
// invariant: organization-scoped product behavior, default-project storage.
func TestStorageOwnershipIsTheDefaultProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_ownership")
	f.enable(t, "alice")
	alice := f.connect(t, "alice", "Agent")
	thread := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Owned",
	})
	f.post(t, "alice", "channels_post", map[string]any{
		"instanceId": alice["instanceId"], "threadId": thread["id"],
		"text": "owned", "idempotencyKey": "owned-1",
	})
	// Every other project-owned table: a direct conversation writes its
	// participants and endpoints, a follow writes a preference, and an
	// administrator writes an access exception.
	bob := f.connect(t, "bob", "Other agent")
	f.post(t, "alice", "channels_dm", map[string]any{
		"instanceId": alice["instanceId"], "recipientInstanceId": bob["instanceId"],
	})
	f.post(t, "alice", "channels_follow", map[string]any{"threadId": thread["id"], "follow": true})
	f.post(t, "alice", "channels_admin", map[string]any{"subjectId": "carol", "canConnect": true})

	rows, err := repo.New(f.db).FixtureCountRows(t.Context(), testOrg)
	require.NoError(t, err)
	require.Positive(t, rows.Participants)
	require.Positive(t, rows.Endpoints)
	require.Positive(t, rows.Preferences)
	require.Positive(t, rows.AccessExceptions)
	stray, err := repo.New(f.db).FixtureCountRowsOutsideDefaultProject(t.Context(), testOrg)
	require.NoError(t, err)
	require.Zero(t, stray, "rows were stored outside the canonical default project")
}

// sortedEndpoints normalizes a recorded endpoint list so a comparison does not
// depend on insertion order.
func sortedEndpoints(t *testing.T, value any) []string {
	t.Helper()
	items, ok := value.([]any)
	require.True(t, ok, "expected an endpoint list, got %T", value)
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		require.True(t, ok)
		out = append(out, text)
	}
	slices.Sort(out)
	return out
}

// Every MCP result carries the members MCP 2026-07-28 defines, and the
// cacheable methods also carry caching members scoped to the caller.
func TestMCPResultsCarryTheProtocolEnvelope(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_envelope")
	f.enable(t, "alice")

	discovered := mapOf(t, f.rpc(t, "alice", "server/discover", map[string]any{})["result"])
	require.Contains(t, discovered["supportedVersions"], "2026-07-28")
	require.Contains(t, mapOf(t, discovered["capabilities"]), "resources")

	for _, method := range []string{"server/discover", "tools/list", "resources/list"} {
		result := mapOf(t, f.rpc(t, "alice", method, map[string]any{})["result"])
		require.Equal(t, "complete", result["resultType"], method)
		require.Equal(t, "Speakeasy Channels", mapOf(t, mapOf(t, result["_meta"])["io.modelcontextprotocol/serverInfo"])["name"], method)
		require.Equal(t, "private", result["cacheScope"], method)
		require.InDelta(t, 0, result["ttlMs"], 0, method)
	}

	pinged := mapOf(t, f.rpc(t, "alice", "ping", map[string]any{})["result"])
	require.Equal(t, "complete", pinged["resultType"])
	require.NotContains(t, pinged, "cacheScope", "only cacheable methods carry caching members")
}

// Resuming an expired instance adds a live lease, so it is held to the
// organization's live-instance cap like a new connection.
func TestResumingAnExpiredInstanceRespectsTheInstanceCap(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_resume_cap")
	f.handler.limits.ActiveInstances = 1
	f.enable(t, "alice")
	first := f.connect(t, "alice", "First")
	require.NoError(t, repo.New(f.db).FixtureAgeInstances(t.Context(), repo.FixtureAgeInstancesParams{OrganizationID: testOrg, AgeDays: 1}))
	f.connect(t, "alice", "Second")

	body, status := f.try(t, "alice", "channels_connect", map[string]any{
		"instanceId": first["instanceId"], "resumeToken": first["resumeToken"],
	})
	require.Equal(t, http.StatusTooManyRequests, status, "%v", body)
}

// The snapshot reports the caller's own effective access, after any access
// exception, so a client can gate participation on what the server enforces.
func TestSnapshotReportsTheViewersEffectiveAccess(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_viewer")
	f.enable(t, "alice")

	require.Equal(t, map[string]any{"canConnect": true, "canReadAll": false, "canAdmin": true}, mapOf(t, f.snapshot(t, "alice")["viewer"]))
	require.Equal(t, map[string]any{"canConnect": true, "canReadAll": true, "canAdmin": true}, mapOf(t, f.snapshot(t, "observer")["viewer"]))

	// An exception that withdraws the administrator's own participation shows
	// in the snapshot the change returns.
	changed := f.post(t, "alice", "channels_admin", map[string]any{"subjectId": "alice", "canConnect": false})
	require.Equal(t, map[string]any{"canConnect": false, "canReadAll": false, "canAdmin": true}, mapOf(t, changed["viewer"]))
}
