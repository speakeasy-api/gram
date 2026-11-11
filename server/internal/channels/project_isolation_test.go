package channels

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// projectIdentities gives the same subjects access to two projects in the same
// organization. Identity equality must not make project-owned data visible.
func projectIdentities(t *testing.T, f *fixture) (string, string) {
	t.Helper()
	first := f.projectID(t, testOrg)
	second := f.projectIDBySlug(t, testOrg, "other")
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, who := range []string{"alice", "bob", "outsider", "observer"} {
		p := f.identitie[who]
		p.ProjectID = first.String()
		f.identitie[who] = p
		p.ProjectID = second.String()
		f.identitie[who+"-other"] = p
	}
	return first.String(), second.String()
}

func TestProjectsIsolateChannelsCore(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_project_core")
	projectIdentities(t, f)
	f.enable(t, "alice")

	// Enabling one project does not enable the organization's other project.
	body, status := f.try(t, "alice-other", "channels_connect", map[string]any{"label": "Disabled"})
	require.Equal(t, http.StatusForbidden, status, "%v", body)
	f.enable(t, "alice-other")
	alice := f.connect(t, "alice", "Same label")
	other := f.connect(t, "alice-other", "Same label")
	thread := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Project-local discussion",
	})
	otherThread := f.post(t, "alice-other", "channels_create_thread", map[string]any{
		"instanceId": other["instanceId"], "title": "Project-local discussion",
	})
	firstMessage := f.post(t, "alice", "channels_post", map[string]any{
		"instanceId": alice["instanceId"], "threadId": thread["id"], "text": "First project", "idempotencyKey": "same-key",
	})
	secondMessage := f.post(t, "alice-other", "channels_post", map[string]any{
		"instanceId": other["instanceId"], "threadId": otherThread["id"], "text": "Second project", "idempotencyKey": "same-key",
	})
	require.Equal(t, firstMessage["id"], secondMessage["id"], "each project allocates an independent message sequence")

	// Project-local availability must remain independent after both contain data.
	f.post(t, "alice", "channels_admin", map[string]any{"enabled": false})
	f.post(t, "alice-other", "channels_history", map[string]any{"threadId": otherThread["id"]})
	f.enable(t, "alice") // Restore availability before the parallel isolation checks.

	for _, test := range []struct {
		who                        string
		local, foreign             map[string]any
		localThread, foreignThread map[string]any
		message                    map[string]any
	}{
		{"alice", alice, other, thread, otherThread, firstMessage},
		{"alice-other", other, alice, otherThread, thread, secondMessage},
	} {
		t.Run(test.who, func(t *testing.T) {
			t.Parallel()
			snapshot := f.snapshot(t, test.who)
			require.Contains(t, mapOf(t, snapshot["instances"]), test.local["instanceId"])
			require.NotContains(t, mapOf(t, snapshot["instances"]), test.foreign["instanceId"])
			require.Contains(t, mapOf(t, snapshot["threads"]), test.localThread["id"])
			require.NotContains(t, mapOf(t, snapshot["threads"]), test.foreignThread["id"])
			require.Equal(t, []int64{intOf(t, test.message["id"])}, messageIDs(t, f.post(t, test.who, "channels_read", map[string]any{})["messages"]))
			discovery := f.post(t, test.who, "channels_conversations", map[string]any{})
			require.Contains(t, mapOf(t, discovery["threads"]), test.localThread["id"])
			require.NotContains(t, mapOf(t, discovery["threads"]), test.foreignThread["id"])

			// Even the owner of both projects cannot use an ID or resume secret
			// obtained in one project against the other's endpoint.
			for _, attempt := range []struct {
				operation string
				args      map[string]any
			}{
				{"channels_history", map[string]any{"threadId": test.foreignThread["id"]}},
				{"channels_follow", map[string]any{"threadId": test.foreignThread["id"], "follow": true}},
				{"channels_post", map[string]any{"instanceId": test.local["instanceId"], "threadId": test.foreignThread["id"], "text": "Cross-project write", "idempotencyKey": "foreign"}},
				{"channels_presence", map[string]any{"instanceId": test.foreign["instanceId"], "note": "Cross-project renewal"}},
				{"channels_connect", map[string]any{"instanceId": test.foreign["instanceId"], "resumeToken": test.foreign["resumeToken"]}},
				{"channels_dm", map[string]any{"instanceId": test.local["instanceId"], "recipientInstanceId": test.foreign["instanceId"]}},
				{"channels_create_thread", map[string]any{"instanceId": test.local["instanceId"], "parentId": test.foreignThread["id"], "title": "Cross-project branch"}},
			} {
				body, status := f.try(t, test.who, attempt.operation, attempt.args)
				require.Equal(t, http.StatusForbidden, status, "%s: %v", attempt.operation, body)
			}
		})
	}
}

func TestChannelsRejectsInvalidProjectScope(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_project_invalid_scope")
	projectIdentities(t, f)
	f.enable(t, "alice")
	foreign := f.projectID(t, otherOrg)
	for _, projectID := range []string{"", "not-a-project-id", uuid.NewString(), foreign.String()} {
		f.mu.Lock()
		p := f.identitie["alice"]
		p.ProjectID = projectID
		f.identitie["invalid-project"] = p
		f.mu.Unlock()
		body, status := f.try(t, "invalid-project", "channels_connect", map[string]any{"label": "Must not fall back"})
		require.GreaterOrEqual(t, status, http.StatusBadRequest, "project %q accepted: %v", projectID, body)
	}
	require.Empty(t, mapOf(t, f.snapshot(t, "alice")["instances"]), "invalid scopes wrote to the default project")
}

func TestProjectsIsolateChannelsAccessExceptions(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_project_exceptions")
	projectIdentities(t, f)
	f.enable(t, "alice")
	f.enable(t, "alice-other")

	// A denial for one subject in the first project must not deny the same
	// subject in the second, nor appear in its administrator's grant list.
	f.post(t, "alice", "channels_admin", map[string]any{"subjectId": "bob", "canConnect": false})
	body, status := f.try(t, "bob", "channels_connect", map[string]any{"label": "Denied"})
	require.Equal(t, http.StatusForbidden, status, "%v", body)
	f.connect(t, "bob-other", "Still permitted")
	require.Contains(t, mapOf(t, f.snapshot(t, "alice")["grants"]), "bob")
	require.NotContains(t, mapOf(t, f.snapshot(t, "alice-other")["grants"]), "bob")

	// The same subject can have an independent override in each project.
	f.post(t, "alice-other", "channels_admin", map[string]any{"subjectId": "bob", "canConnect": true})
	f.post(t, "alice", "channels_admin", map[string]any{"subjectId": "bob", "remove": true})
	f.connect(t, "bob", "Restored")
	require.Contains(t, mapOf(t, f.snapshot(t, "alice-other")["grants"]), "bob")

	// A read-all exception is also project-local; private participation cannot
	// be inferred from having the same subject ID in another project.
	for _, who := range []string{"alice", "alice-other"} {
		author := f.connect(t, who, "Private author")
		peer := "bob"
		if who == "alice-other" {
			peer = "bob-other"
		}
		recipient := f.connect(t, peer, "Private recipient")
		direct := f.post(t, who, "channels_dm", map[string]any{"instanceId": author["instanceId"], "recipientInstanceId": recipient["instanceId"]})
		f.post(t, who, "channels_post", map[string]any{"instanceId": author["instanceId"], "threadId": direct["id"], "text": "Private content", "idempotencyKey": "private"})
	}
	f.post(t, "alice", "channels_admin", map[string]any{"subjectId": "outsider", "canConnect": true, "canReadAll": true})
	require.Len(t, messageIDs(t, f.post(t, "outsider", "channels_read", map[string]any{})["messages"]), 1)
	require.Empty(t, messageIDs(t, f.post(t, "outsider-other", "channels_read", map[string]any{})["messages"]))
	f.post(t, "alice-other", "channels_admin", map[string]any{"subjectId": "outsider", "canConnect": true, "canReadAll": true})
	f.post(t, "alice", "channels_admin", map[string]any{"subjectId": "outsider", "remove": true})
	require.Empty(t, messageIDs(t, f.post(t, "outsider", "channels_read", map[string]any{})["messages"]))
	require.Len(t, messageIDs(t, f.post(t, "outsider-other", "channels_read", map[string]any{})["messages"]), 1)
}

func TestProjectsIsolateChannelsSubscriptions(t *testing.T) {
	t.Parallel()
	db := newDatabase(t, "channels_project_streams")
	writer, reader := newFixtureOn(t, db), newFixtureOn(t, db)
	projectIdentities(t, writer)
	projectIdentities(t, reader)
	writer.enable(t, "alice")
	writer.enable(t, "alice-other")
	firstAuthor := writer.connect(t, "alice", "First author")
	secondAuthor := writer.connect(t, "alice-other", "Second author")
	firstPeer := writer.connect(t, "bob", "First recipient")
	secondPeer := writer.connect(t, "bob-other", "Second recipient")
	firstDM := writer.post(t, "alice", "channels_dm", map[string]any{"instanceId": firstAuthor["instanceId"], "recipientInstanceId": firstPeer["instanceId"]})
	secondDM := writer.post(t, "alice-other", "channels_dm", map[string]any{"instanceId": secondAuthor["instanceId"], "recipientInstanceId": secondPeer["instanceId"]})

	// Both streams have the same subject and request ID, but not the same
	// project. Cancellation must check project authority, including tokens.
	first := reader.listen(t, "bob", "same-request", "channels://messages")
	second := reader.listen(t, "bob-other", "same-request", "channels://messages")
	firstToken := subscriptionToken(t, first.next(t))
	secondToken := subscriptionToken(t, second.next(t))
	require.NotEmpty(t, firstToken)
	require.NotEmpty(t, secondToken)
	reader.cancel(t, "bob-other", "same-request", firstToken)
	first.silent(t, time.Second)
	second.silent(t, time.Second)

	writer.post(t, "alice", "channels_post", map[string]any{"instanceId": firstAuthor["instanceId"], "threadId": firstDM["id"], "text": "First project update", "idempotencyKey": "stream"})
	require.Equal(t, "notifications/resources/updated", first.next(t)["method"])
	second.silent(t, 2*time.Second)
	writer.post(t, "alice-other", "channels_post", map[string]any{"instanceId": secondAuthor["instanceId"], "threadId": secondDM["id"], "text": "Second project update", "idempotencyKey": "stream"})
	require.Equal(t, "notifications/resources/updated", second.next(t)["method"])
	first.silent(t, 2*time.Second)

	// A request-ID-only cancellation is unambiguous inside its project even
	// though the same account has an identically named stream elsewhere.
	reader.cancel(t, "bob", "same-request", "")
	require.Equal(t, "complete", mapOf(t, first.next(t)["result"])["resultType"])
	second.silent(t, time.Second)
	reader.cancel(t, "bob-other", "same-request", secondToken)
	require.Equal(t, "complete", mapOf(t, second.next(t)["result"])["resultType"])
}
