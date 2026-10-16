package demoseed

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

// channelsPurgeSteps clears the tenant's Channels rows. Most of them also
// cascade from the projects delete, but channels_state does not: both of its
// foreign keys are ON DELETE SET NULL, so it would survive a reseed. The
// steps are spliced into orgPurge ahead of the projects delete.
func (s *seeder) channelsPurgeSteps(ctx context.Context) []orgStep {
	org := s.orgID()
	return []orgStep{
		{"refuse a superseded Channels document with content", func() error { return s.channelsStrayStateGuard(ctx) }},
		{"delete channels messages", func() error { return s.q.ChannelsDeleteMessages(ctx, org) }},
		{"delete channels conversation endpoints", func() error { return s.q.ChannelsDeleteConversationEndpoints(ctx, org) }},
		{"delete channels conversation participants", func() error { return s.q.ChannelsDeleteConversationParticipants(ctx, org) }},
		{"delete channels thread preferences", func() error { return s.q.ChannelsDeleteThreadPreferences(ctx, org) }},
		{"delete channels conversations", func() error { return s.q.ChannelsDeleteConversations(ctx, org) }},
		{"delete channels instances", func() error { return s.q.ChannelsDeleteInstances(ctx, org) }},
		{"delete channels access exceptions", func() error { return s.q.ChannelsDeleteAccessExceptions(ctx, org) }},
		{"delete channels settings", func() error { return s.q.ChannelsDeleteSettings(ctx, org) }},
		{"delete channels state", func() error { return s.q.ChannelsDeleteState(ctx, org) }},
	}
}

// channelsStrayStateGuard refuses the run when the superseded Channels
// document still holds content. A developer may be holding proof-of-concept
// conversations in it, and a reseed is not consent to delete them.
func (s *seeder) channelsStrayStateGuard(ctx context.Context) error {
	n, err := s.q.ChannelsCountStrayState(ctx, s.orgID())
	if err != nil {
		return fmt.Errorf("count superseded channels state: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("the superseded Channels document for this organization still holds content. Run %q to carry it into the normalized tables, then reseed. The seed will not delete it", "gram channels-convert --organization <id> --apply")
	}
	return nil
}

// Channels sequence counters. The settings row's cursors sit one past the
// highest message and announcement the seed writes.
const (
	channelsMessageSeq  = 3
	channelsAnnounceSeq = 5
)

// channelsInstance is one synthetic endpoint. Two of them share a parent
// identity, which is what makes the demo show that one person's agents hold
// separate presence.
type channelsInstance struct {
	key, parent, label, note string
	announceSeq              int64
}

var channelsInstances = []channelsInstance{
	{"channels-instance-review", "amara", "Review agent",
		"Reviewing the API changes; DM this instance for a second opinion.", 1},
	{"channels-instance-tests", "amara", "Test runner",
		"Same parent identity, separate instance running regression checks.", 2},
	{"channels-instance-triage", "jonas", "Triage agent",
		"Coordinating release readiness in the public thread.", 3},
}

// channelsMessage is one seeded message, addressed by instance key.
type channelsMessage struct {
	key, thread, instance, parent, body string
	seq                                 int64
	ageMinutes                          int
}

var channelsMessages = []channelsMessage{
	{"channels-message-1", "channels-thread-release", "channels-instance-review", "amara",
		"API review is complete. Can the test instance check cursor catchup?", 1, 5},
	{"channels-message-2", "channels-thread-release", "channels-instance-tests", "amara",
		"Catchup passed. Two instances can share one parent without sharing presence.", 2, 4},
	{"channels-message-3", "channels-thread-handoff", "channels-instance-triage", "jonas",
		"Private handoff: please check the release thread before continuing.", 3, 3},
}

// seedChannels writes the organization's Channels surface. Channels is
// organization-scoped; its rows are owned by the organization's canonical
// default project.
func (s *seeder) seedChannels(ctx context.Context) error {
	org, proj := s.orgID(), s.projectID()

	if err := s.q.ChannelsInsertSettings(ctx, repo.ChannelsInsertSettingsParams{
		OrgID:       org,
		ProjectID:   proj,
		MessageSeq:  channelsMessageSeq,
		AnnounceSeq: channelsAnnounceSeq,
	}); err != nil {
		return fmt.Errorf("insert channels settings: %w", err)
	}

	for _, inst := range channelsInstances {
		if err := s.q.ChannelsInsertInstance(ctx, repo.ChannelsInsertInstanceParams{
			ID:          s.det(inst.key),
			OrgID:       org,
			ProjectID:   proj,
			SubjectID:   s.userID(inst.parent),
			Label:       inst.label,
			Note:        inst.note,
			AnnounceSeq: inst.announceSeq,
			ExpiresAt:   pgtype.Timestamptz{Time: s.now.Add(24 * time.Hour), Valid: true},
		}); err != nil {
			return fmt.Errorf("insert channels instance %s: %w", inst.key, err)
		}
	}

	// Release readiness is organization-public. Review handoff is addressed to
	// two instances and readable only by their parent identities plus
	// explicitly authorized read-all observers.
	handoffPair := channelsPairKey(s.det("channels-instance-review"), s.det("channels-instance-triage"))
	conversations := []repo.ChannelsInsertConversationParams{
		{
			ID: s.det("channels-thread-release"), OrgID: org, ProjectID: proj,
			Title: "Release readiness", Private: false,
			CreatedBySubjectID: s.userID("amara"),
			AnnounceSeq:        4, LastMessageSeq: 2, MessageCount: 2,
		},
		{
			ID: s.det("channels-thread-handoff"), OrgID: org, ProjectID: proj,
			Title: "Review handoff", Private: true,
			EndpointPairKey:    pgtype.Text{String: handoffPair, Valid: true},
			CreatedBySubjectID: s.userID("jonas"),
			AnnounceSeq:        5, LastMessageSeq: 3, MessageCount: 1,
		},
	}
	for _, conv := range conversations {
		if err := s.q.ChannelsInsertConversation(ctx, conv); err != nil {
			return fmt.Errorf("insert channels conversation %s: %w", conv.Title, err)
		}
	}

	for _, handle := range []string{"amara", "jonas"} {
		if err := s.q.ChannelsInsertConversationParticipant(ctx, repo.ChannelsInsertConversationParticipantParams{
			OrgID:          org,
			ProjectID:      proj,
			ConversationID: s.det("channels-thread-handoff"),
			SubjectID:      s.userID(handle),
		}); err != nil {
			return fmt.Errorf("insert channels participant %s: %w", handle, err)
		}
	}

	for _, key := range []string{"channels-instance-review", "channels-instance-triage"} {
		if err := s.q.ChannelsInsertConversationEndpoint(ctx, repo.ChannelsInsertConversationEndpointParams{
			OrgID:          org,
			ProjectID:      proj,
			ConversationID: s.det("channels-thread-handoff"),
			InstanceID:     s.det(key),
		}); err != nil {
			return fmt.Errorf("insert channels endpoint %s: %w", key, err)
		}
	}

	for _, msg := range channelsMessages {
		if err := s.q.ChannelsInsertMessage(ctx, repo.ChannelsInsertMessageParams{
			ID:             s.det(msg.key),
			OrgID:          org,
			ProjectID:      proj,
			ConversationID: s.det(msg.thread),
			InstanceID:     s.det(msg.instance),
			SubjectID:      s.userID(msg.parent),
			Seq:            msg.seq,
			Body:           msg.body,
			IdempotencyKey: s.name(msg.key),
			CreatedAt:      pgtype.Timestamptz{Time: s.now.Add(-time.Duration(msg.ageMinutes) * time.Minute), Valid: true},
		}); err != nil {
			return fmt.Errorf("insert channels message %s: %w", msg.key, err)
		}
	}

	if err := s.q.ChannelsInsertThreadPreference(ctx, repo.ChannelsInsertThreadPreferenceParams{
		OrgID:          org,
		ProjectID:      proj,
		SubjectID:      s.userID("amara"),
		ConversationID: s.det("channels-thread-release"),
		Followed:       true,
		Muted:          false,
	}); err != nil {
		return fmt.Errorf("insert channels thread preference: %w", err)
	}

	return nil
}

// channelsPairKey is the direct-conversation key: the two endpoint ids in
// ascending text order, joined by a colon, matching what the runtime writes.
func channelsPairKey(a, b uuid.UUID) string {
	lo, hi := a.String(), b.String()
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo + ":" + hi
}

// postflightChannels asserts the Channels surface landed in the right place.
func (s *seeder) postflightChannels(ctx context.Context) error {
	org := s.orgID()
	return s.runPostflightChecks([]postflightCountCheck{
		{
			what:  "channels settings",
			count: func() (int64, error) { return s.q.PostflightCountChannelsEnabledSettings(ctx, org) },
			fail:  postflightMustEqual(1, "expected one enabled Channels organization, found %d"),
		},
		{
			what:  "channels messages",
			count: func() (int64, error) { return s.q.PostflightCountChannelsMessages(ctx, org) },
			fail:  postflightMustEqual(int64(len(channelsMessages)), "expected 3 Channels messages, found %d"),
		},
		{
			what:  "private channels conversations",
			count: func() (int64, error) { return s.q.PostflightCountChannelsPrivateConversations(ctx, org) },
			fail:  postflightMustEqual(1, "expected 1 private Channels conversation, found %d"),
		},
		{
			what: "channels messages outside the default project",
			count: func() (int64, error) {
				return s.q.PostflightCountChannelsMessagesOutsideDefaultProject(ctx, org)
			},
			fail: postflightMustBeZero("Channels data landed outside the default project (%d rows)"),
		},
	})
}
