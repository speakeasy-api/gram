package channels

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/channels/repo"
)

func TestPresenceSubscriptionDetectsNoteChanges(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_presence_note_revision")
	f.enable(t, "alice")
	instance := f.connect(t, "alice", "Agent")
	f.handler.limits.ReconcileInterval = 50 * time.Millisecond

	stream := f.listen(t, "bob", "presence-notes", "channels://presence")
	require.Equal(t, "notifications/subscriptions/acknowledged", stream.next(t)["method"])
	f.post(t, "alice", "channels_presence", map[string]any{
		"instanceId": instance["instanceId"], "note": "Reviewing the release",
	})
	updated := stream.next(t)
	require.Equal(t, "notifications/resources/updated", updated["method"])
	require.Equal(t, "channels://presence", mapOf(t, updated["params"])["uri"])

	// An unchanged note and a renewed lease are not new presence information.
	p := Principal{OrganizationID: testOrg, ProjectID: f.projectID(t, testOrg).String(), SubjectID: "bob", CanConnect: true}
	wanted := map[string]bool{"channels://presence": true}
	before, err := f.handler.revisions(t.Context(), p, wanted, 0)
	require.NoError(t, err)
	f.post(t, "alice", "channels_presence", map[string]any{
		"instanceId": instance["instanceId"], "note": "Reviewing the release",
	})
	after, err := f.handler.revisions(t.Context(), p, wanted, 0)
	require.NoError(t, err)
	require.False(t, before.changed(after, "channels://presence"), "lease heartbeats must not continually invalidate presence")
}

func TestPresenceSubscriptionDetectsEqualCountMembershipChanges(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_presence_membership_revision")
	f.enable(t, "alice")
	returning := f.connect(t, "alice", "Returning")
	departing := f.connect(t, "bob", "Departing")
	// Keep the maximum announcement sequence unchanged when the older
	// instances exchange places in the live set.
	f.connect(t, "outsider", "Newest")
	queries := repo.New(f.db)
	returningID := uuid.MustParse(stringOf(t, returning["instanceId"]))
	err := queries.FixtureExpireInstance(t.Context(), repo.FixtureExpireInstanceParams{
		OrganizationID: testOrg, ProjectID: f.projectID(t, testOrg), ID: returningID,
	})
	require.NoError(t, err)

	p := Principal{OrganizationID: testOrg, ProjectID: f.projectID(t, testOrg).String(), SubjectID: "bob", CanConnect: true}
	wanted := map[string]bool{"channels://presence": true}
	before, err := f.handler.revisions(t.Context(), p, wanted, 0)
	require.NoError(t, err)
	f.handler.limits.ReconcileInterval = 50 * time.Millisecond
	stream := f.listen(t, "bob", "presence-members", "channels://presence")
	require.Equal(t, "notifications/subscriptions/acknowledged", stream.next(t)["method"])

	// Commit both membership changes together so no reconciliation can
	// observe an intermediate count. Returning an existing instance does not
	// allocate a new announcement sequence.
	err = queries.FixtureSwapLiveInstances(t.Context(), repo.FixtureSwapLiveInstancesParams{
		OrganizationID: testOrg, ProjectID: f.projectID(t, testOrg), ReturningID: returningID,
		DepartingID: uuid.MustParse(stringOf(t, departing["instanceId"])),
	})
	require.NoError(t, err)
	after, err := f.handler.revisions(t.Context(), p, wanted, 0)
	require.NoError(t, err)
	require.Equal(t, before.presenceLive, after.presenceLive)
	require.Equal(t, before.presenceAnnounce, after.presenceAnnounce)
	require.True(t, before.changed(after, "channels://presence"))
	updated := stream.next(t)
	require.Equal(t, "notifications/resources/updated", updated["method"])
	require.Equal(t, "channels://presence", mapOf(t, updated["params"])["uri"])
}
