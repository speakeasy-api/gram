package channels

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/channels/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const legacyDocument = `{
  "enabled": true,
  "grants": {"carol": {"canConnect": true, "canReadAll": false}},
  "instances": {
    "legacy-review": {"id": "legacy-review", "subjectId": "alice", "resumeHash": "4f0d1e4a2b6c8d9e0f1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f70", "label": "Review agent", "note": "Reviewing", "expiresAt": "2099-01-01T00:00:00Z"},
    "legacy-triage": {"id": "legacy-triage", "subjectId": "bob", "label": "Triage agent", "expiresAt": "2099-01-01T00:00:00Z"},
    "legacy-browser": {"id": "legacy-browser", "subjectId": "alice", "label": "alice on dashboard", "origin": "dashboard", "expiresAt": "2099-01-01T00:00:00Z"}
  },
  "threads": {
    "legacy-public": {"id": "legacy-public", "title": "Release readiness"},
    "legacy-private": {"id": "legacy-private", "title": "Review handoff", "participants": ["alice", "bob"], "instanceIds": ["legacy-review", "legacy-triage"]}
  },
  "messages": [
    {"id": 1, "threadId": "legacy-public", "instanceId": "legacy-review", "subjectId": "alice", "text": "Public one", "idempotencyKey": "p1", "createdAt": "2026-01-01T00:00:00Z"},
    {"id": 2, "threadId": "legacy-private", "instanceId": "legacy-triage", "subjectId": "bob", "text": "Private one", "idempotencyKey": "p2", "createdAt": "2026-01-01T00:01:00Z"},
    {"id": 3, "threadId": "legacy-public", "instanceId": "vanished", "subjectId": "alice", "text": "Orphan", "idempotencyKey": "p3", "createdAt": "2026-01-01T00:02:00Z"}
  ],
  "preferences": {"alice": {"followed": ["legacy-public"], "muted": ["legacy-private"]}},
  "cursor": 3,
  "version": 4
}`

// TestConvertLegacyState proves the superseded document converts into the
// normalized tables without losing content, that repeating the conversion
// imports nothing new, and that the converted organization keeps working.
func TestConvertLegacyState(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_legacy")
	err := repo.New(f.db).FixtureInsertLegacyState(t.Context(), repo.FixtureInsertLegacyStateParams{OrganizationID: testOrg, State: []byte(legacyDocument)})
	require.NoError(t, err)

	logger := testenv.NewLogger(t)

	// A dry run reports exactly what an apply would do and writes nothing.
	preview, err := ConvertLegacyState(t.Context(), f.db, logger, ConversionOptions{DryRun: true})
	require.NoError(t, err)
	require.Equal(t, 3, preview.Instances)
	require.Equal(t, 2, preview.Conversations)
	require.Equal(t, 2, preview.Messages)
	require.Len(t, preview.Mappings, 5, "the preview reports every rewritten identifier")
	written, err := repo.New(f.db).FixtureCountRows(t.Context(), testOrg)
	require.NoError(t, err)
	require.Zero(t, written.Instances, "a dry run wrote rows")

	first, err := ConvertLegacyState(t.Context(), f.db, logger, ConversionOptions{DryRun: false})
	require.NoError(t, err)
	require.Equal(t, 1, first.Organizations)
	require.Equal(t, 3, first.Instances)
	require.Equal(t, 2, first.Conversations)
	require.Equal(t, 2, first.Messages)
	require.Equal(t, 1, first.SkippedMessages, "a message whose author is gone cannot be attributed")

	// The reported mapping is the documented one: recomputable from the old
	// identifier alone, so nobody has to keep a translation table.
	for _, mapping := range first.Mappings {
		require.Equal(t, LegacyIdentifier(mapping.OrganizationID, mapping.Kind, mapping.LegacyID), mapping.ID)
	}
	require.Equal(t, preview.Mappings, first.Mappings, "the preview and the apply rewrite identifiers identically")
	reviewAgent, err := repo.New(f.db).GetInstance(t.Context(), repo.GetInstanceParams{OrganizationID: testOrg, ID: LegacyIdentifier(testOrg, LegacyKindInstance, "legacy-review")})
	require.NoError(t, err)
	require.Equal(t, "Review agent", reviewAgent.Label, "the documented mapping does not locate the converted row")

	// A stored secret hash carries over; it is never re-issued as a token.
	require.Len(t, reviewAgent.ResumeHash.String, 64)
	browser, err := repo.New(f.db).GetInstance(t.Context(), repo.GetInstanceParams{OrganizationID: testOrg, ID: LegacyIdentifier(testOrg, LegacyKindInstance, "legacy-browser")})
	require.NoError(t, err)
	require.Equal(t, "dashboard", browser.Origin)
	require.False(t, browser.ResumeHash.Valid, "a converted browser session must stay secretless")

	// The legacy document is left in place so nothing is discarded.
	documents, err := repo.New(f.db).FixtureCountLegacyStates(t.Context(), testOrg)
	require.NoError(t, err)
	require.Equal(t, int64(1), documents)

	// Repeating the conversion is a no-op rather than a duplication.
	second, err := ConvertLegacyState(t.Context(), f.db, logger, ConversionOptions{DryRun: false})
	require.NoError(t, err)
	require.Equal(t, 1, second.Organizations)
	rows, err := repo.New(f.db).FixtureCountRows(t.Context(), testOrg)
	require.NoError(t, err)
	require.Equal(t, repo.FixtureCountRowsRow{
		Instances:        3,
		Conversations:    2,
		Messages:         2,
		Participants:     2,
		Endpoints:        2,
		Preferences:      2,
		AccessExceptions: 1,
	}, rows, "a repeated conversion duplicated rows")

	// Converted state is live: enablement, visibility, preferences and the
	// cursor all carried over.
	snapshot := f.snapshot(t, "alice")
	require.Equal(t, true, snapshot["enabled"])
	require.Len(t, snapshot["threads"], 2)
	require.Equal(t, int64(2), intOf(t, snapshot["cursor"]))
	preferences := mapOf(t, snapshot["preferences"])
	require.Len(t, preferences["followed"], 1)
	require.Len(t, preferences["muted"], 1)

	// The private conversation stayed private.
	require.Len(t, f.snapshot(t, "outsider")["threads"], 1, "a converted private conversation leaked")

	// A new message continues the converted cursor space instead of colliding
	// with an imported sequence.
	alice := f.connect(t, "alice", "After conversion")
	var publicID string
	for id, value := range mapOf(t, snapshot["threads"]) {
		if mapOf(t, value)["title"] == "Release readiness" {
			publicID = id
		}
	}
	require.NotEmpty(t, publicID)
	posted := f.post(t, "alice", "channels_post", map[string]any{
		"instanceId": alice["instanceId"], "threadId": publicID,
		"text": "after conversion", "idempotencyKey": "after-1",
	})
	require.Greater(t, intOf(t, posted["id"]), int64(3), "a converted organization reused a sequence")
}

// TestConversionRefusesANonEmptyTarget is the fix for an import that carried
// the document's own message sequence numbers into an organization the live
// service had already numbered. The collision used to surface as a logged
// error and a silently skipped organization.
func TestConversionRefusesANonEmptyTarget(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_legacy_conflict")
	logger := testenv.NewLogger(t)

	// Live data first: this organization is already using Channels.
	f.enable(t, "alice")
	alice := f.connect(t, "alice", "Live agent")
	thread := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Live",
	})
	live := f.post(t, "alice", "channels_post", map[string]any{
		"instanceId": alice["instanceId"], "threadId": thread["id"],
		"text": "live message", "idempotencyKey": "live-1",
	})
	require.Equal(t, int64(1), intOf(t, live["id"]), "the live service already issued sequence 1")

	err := repo.New(f.db).FixtureInsertLegacyState(t.Context(), repo.FixtureInsertLegacyStateParams{OrganizationID: testOrg, State: []byte(legacyDocument)})
	require.NoError(t, err)

	result, err := ConvertLegacyState(t.Context(), f.db, logger, ConversionOptions{DryRun: false, OrganizationID: testOrg})
	require.Error(t, err, "converting into live data was accepted")
	require.ErrorContains(t, err, "already holds")
	require.Zero(t, result.Organizations, "a refused organization must not be counted as converted")
	require.Len(t, result.Refused, 1)
	require.Equal(t, testOrg, result.Refused[0].OrganizationID)

	// Nothing was written, and the live message kept its sequence.
	require.Len(t, messageIDs(t, f.post(t, "alice", "channels_read", map[string]any{})["messages"]), 1)
	rows, err := repo.New(f.db).FixtureCountRows(t.Context(), testOrg)
	require.NoError(t, err)
	require.Equal(t, int64(1), rows.Instances, "a refused conversion wrote instances")
}

// TestConversionIsScopedToOneOrganization proves the blast radius of a
// conversion can be bounded to one tenant.
func TestConversionIsScopedToOneOrganization(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_legacy_scope")
	logger := testenv.NewLogger(t)
	for _, organization := range []string{testOrg, otherOrg} {
		err := repo.New(f.db).FixtureInsertLegacyState(t.Context(), repo.FixtureInsertLegacyStateParams{OrganizationID: organization, State: []byte(legacyDocument)})
		require.NoError(t, err)
	}

	scoped, err := ConvertLegacyState(t.Context(), f.db, logger, ConversionOptions{DryRun: false, OrganizationID: testOrg})
	require.NoError(t, err)
	require.Equal(t, 1, scoped.Organizations, "a scoped conversion touched more than one tenant")
	for _, mapping := range scoped.Mappings {
		require.Equal(t, testOrg, mapping.OrganizationID)
	}
	elsewhere, err := repo.New(f.db).FixtureCountRows(t.Context(), otherOrg)
	require.NoError(t, err)
	require.Zero(t, elsewhere.Instances, "a scoped conversion wrote into another tenant")

	// Unscoped picks up the rest, and the already-converted tenant stays a
	// no-op rather than a refusal.
	everything, err := ConvertLegacyState(t.Context(), f.db, logger, ConversionOptions{DryRun: false, OrganizationID: ""})
	require.NoError(t, err)
	require.Equal(t, 2, everything.Organizations)
	elsewhere, err = repo.New(f.db).FixtureCountRows(t.Context(), otherOrg)
	require.NoError(t, err)
	require.Equal(t, int64(3), elsewhere.Instances)
}

// TestConvertedConversationCountsAreExact proves a conversion lands the
// maintained count rather than leaving the list showing zero.
func TestConvertedConversationCountsAreExact(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_legacy_counts")
	err := repo.New(f.db).FixtureInsertLegacyState(t.Context(), repo.FixtureInsertLegacyStateParams{OrganizationID: testOrg, State: []byte(legacyDocument)})
	require.NoError(t, err)
	_, err = ConvertLegacyState(t.Context(), f.db, testenv.NewLogger(t), ConversionOptions{DryRun: false, OrganizationID: testOrg})
	require.NoError(t, err)

	for _, row := range mapOf(t, f.snapshot(t, "alice")["threads"]) {
		thread := mapOf(t, row)
		switch thread["title"] {
		case "Release readiness":
			require.Equal(t, int64(1), intOf(t, thread["messageCount"]))
		case "Review handoff":
			require.Equal(t, int64(1), intOf(t, thread["messageCount"]))
		}
	}
}
