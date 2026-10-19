package platformmcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	evidencefixtures "github.com/speakeasy-api/gram/server/internal/platformmcp/fixtures"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestChatEvidenceParticipantSnapshotsAndMessageFiltering(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	chat := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_assistant_owner", "shared-participants", "Shared fixture", uuid.NullUUID{})
	message := seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "user", content: "investigation_needle", at: f.base})
	result := seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "tool", content: "investigation_needle result", at: f.base.Add(time.Second)})
	err := evidencefixtures.New(f.db).SeedPlatformChatEvidenceParticipantFixture(ctx, evidencefixtures.SeedPlatformChatEvidenceParticipantFixtureParams{ProjectID: f.project.ID, ChatID: chat, MessageID: message, Provider: "slack", ProviderUserID: "UEXAMPLE01", ProviderTeamID: "TEXAMPLE01", UserID: "user_fixture_sender", DisplayName: "Recorded sender"})
	require.NoError(t, err)
	input := f.search("investigation_needle")
	input.Participant = &ChatEvidenceParticipantFilter{UserID: "user_fixture_sender"}
	page, err := f.service.Search(ctx, f.principal, input)
	require.NoError(t, err)
	require.Len(t, page.Hits, 1)
	hit := page.Hits[0]
	require.Equal(t, message.String(), hit.MessageID)
	require.Equal(t, "user_fixture_assistant_owner", hit.OwnerUserID)
	require.Len(t, hit.Participants, 1)
	require.Equal(t, "recorded_participant_snapshot", hit.Participants[0].Provenance)
	require.Equal(t, "Recorded sender", hit.Participants[0].DisplayName)
	require.NotNil(t, hit.Participants[0].RecordedAt)
	require.Nil(t, hit.Participants[0].CurrentIdentity)
	input.OwnerUserID = "user_fixture_sender"
	page, err = f.service.Search(ctx, f.principal, input)
	require.NoError(t, err)
	require.Empty(t, page.Hits)
	input.OwnerUserID = ""
	input.Query = ""
	input.Participant = &ChatEvidenceParticipantFilter{Provider: "slack", ProviderUserID: "UEXAMPLE01", ProviderTeamID: "TEXAMPLE01"}
	page, err = f.service.Search(ctx, f.principal, input)
	require.NoError(t, err)
	require.Len(t, page.Hits, 1)
	input.Participant.ProviderTeamID = "TEXAMPLE02"
	page, err = f.service.Search(ctx, f.principal, input)
	require.NoError(t, err)
	require.Empty(t, page.Hits)
	read, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chat.String(), MessageID: message.String(), ContextSize: new(1)})
	require.NoError(t, err)
	require.Len(t, read.Messages, 2)
	require.Equal(t, result.String(), read.Messages[1].MessageID)
	require.Empty(t, read.Messages[1].Participants)
	require.Equal(t, hit.Participants, read.Messages[0].Participants)

	// A corrupt cross-project or cross-chat participant must never attach to an
	// admitted message merely because it names that message ID.
	other, err := projectsrepo.New(f.db).CreateProject(ctx, projectsrepo.CreateProjectParams{OrganizationID: f.principal.OrganizationID, Name: "Other participant fixture", Slug: "other-participant-fixture"})
	require.NoError(t, err)
	otherChat := seedRecallChat(t, ctx, f.db, other.ID, f.principal.OrganizationID, "user_fixture_other_owner", "other-participants", "Other", uuid.NullUUID{})
	err = evidencefixtures.New(f.db).SeedPlatformChatEvidenceParticipantFixture(ctx, evidencefixtures.SeedPlatformChatEvidenceParticipantFixtureParams{ProjectID: other.ID, ChatID: otherChat, MessageID: message, Provider: "slack", ProviderUserID: "UEXAMPLE02", DisplayName: "Hidden fixture"})
	require.NoError(t, err)
	read, err = f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chat.String(), MessageID: message.String(), ContextSize: new(0)})
	require.NoError(t, err)
	require.Len(t, read.Messages[0].Participants, 1)
	require.Equal(t, hit.Participants, read.Messages[0].Participants)
}

func TestChatEvidenceSlackClaimsAndTimestampProvenance(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	chat := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_assistant_owner", "provider-time", "Current metadata", uuid.NullUUID{})
	eventAt := f.base.AddDate(-1, 0, 0).Add(123456 * time.Microsecond)
	header := fmt.Sprintf("<message-context>\nEventID: fixture:EvEXAMPLE01\nEventType: message\nUserID: UEXAMPLE01\nTimestamp: %d.123456\n</message-context>\n\ninvestigation_needle", eventAt.Unix())
	message := seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "user", content: header, at: f.base, source: "assistants"})
	for i, seed := range []recallMessageSeed{
		{role: "assistant", content: header, source: "assistants"},
		{role: "user", content: "Quoted context:\n" + header, source: "assistants"},
		{role: "user", content: header, source: "codex"},
		{role: "user", content: strings.Replace(header, "fixture:EvEXAMPLE01", "teams:EXAMPLE01", 1), source: "assistants"},
		{role: "user", content: "ordinary body", toolCalls: fmt.Sprintf(`[{"function":{"name":"fixture","arguments":%q}}]`, header), source: "assistants"},
	} {
		seed.at = f.base.Add(time.Duration(i+1) * time.Second)
		seedRecallMessage(t, ctx, f.db, chat, f.project.ID, seed)
	}
	input := f.search("investigation_needle")
	input.Participant = &ChatEvidenceParticipantFilter{Provider: "slack", ProviderUserID: "UEXAMPLE01"}
	page, err := f.service.Search(ctx, f.principal, input)
	require.NoError(t, err)
	require.Len(t, page.Hits, 1)
	hit := page.Hits[0]
	require.Equal(t, message.String(), hit.MessageID)
	require.Equal(t, f.base, hit.At)
	require.Equal(t, "chat_messages.created_at", hit.AtSource)
	require.Equal(t, []ChatEvidenceEventTime{{At: eventAt, Provider: "slack", Path: "content.leading_message_context.Timestamp", Trust: "captured_content_claim"}}, hit.ProviderEventTimes)
	require.Len(t, hit.Participants, 1)
	require.Equal(t, "captured_content_claim", hit.Participants[0].Provenance)
	require.Empty(t, hit.Participants[0].UserID)
	require.Nil(t, hit.Participants[0].CurrentIdentity)
	read, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chat.String(), MessageID: message.String(), ContextSize: new(0)})
	require.NoError(t, err)
	require.Equal(t, hit.ProviderEventTimes, read.Messages[0].ProviderEventTimes)
	input.From = eventAt.Add(-time.Minute).Format(time.RFC3339Nano)
	input.To = eventAt.Add(time.Minute).Format(time.RFC3339Nano)
	page, err = f.service.Search(ctx, f.principal, input)
	require.NoError(t, err)
	require.Empty(t, page.Hits, "provider claims must not change the stored-time filter")
	for _, invalid := range []string{
		strings.Replace(header, "Timestamp: ", "Timestamp: bad\nTimestamp: ", 1),
		strings.Replace(header, ".123456", ".notdigits", 1),
		strings.Replace(header, ".123456", ".1234567890", 1),
		strings.Replace(header, "UserID: UEXAMPLE01", "UserID: invalid", 1),
	} {
		_, eventTime := parseSlackEvidenceHeader(slackEvidenceHeader(invalid, "user", "assistants"))
		require.Nil(t, eventTime)
	}
}

func TestChatEvidenceCurrentIdentityRequiresSeparateGrant(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	q := evidencefixtures.New(f.db)
	chat := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_assistant_owner", "directory-resolution", "Shared fixture", uuid.NullUUID{})
	header := fmt.Sprintf("<message-context>\nEventID: fixture:EvEXAMPLE01\nEventType: message\nUserID: UEXAMPLE01\nTimestamp: %d.000001\n</message-context>\n\nresolution_needle", f.base.Unix())
	message := seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "user", content: header, at: f.base, source: "assistants"})
	err := q.SeedPlatformChatEvidenceDirectoryFixture(ctx, evidencefixtures.SeedPlatformChatEvidenceDirectoryFixtureParams{OrganizationID: f.principal.OrganizationID, TeamID: "TEXAMPLE01", ProviderUserID: "UEXAMPLE01", MappedUserID: f.principal.UserID, DisplayName: "Current fixture name", ObservedAt: timestamp(f.base.Add(time.Hour))})
	require.NoError(t, err)
	input := f.search("resolution_needle")
	input.ResolveParticipantIdentities = true
	_, err = f.service.Search(ctx, f.principal, input)
	var denied *ExternalAuthorizationError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, "org:admin", denied.RequiredScope)
	readInput := ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chat.String(), MessageID: message.String(), ContextSize: new(0), ResolveParticipantIdentities: true}
	_, err = f.service.Read(ctx, f.principal, readInput)
	require.ErrorAs(t, err, &denied)
	adminCtx := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource), authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)})
	input.Participant = &ChatEvidenceParticipantFilter{UserID: f.principal.UserID}
	page, err := f.service.Search(adminCtx, f.principal, input)
	require.NoError(t, err)
	require.Len(t, page.Hits, 1)
	p := page.Hits[0].Participants[0]
	require.Empty(t, p.UserID, "current identity must not rewrite recorded attribution")
	require.Empty(t, p.DisplayName)
	require.Equal(t, "captured_content_claim", p.Provenance)
	require.NotNil(t, p.CurrentIdentity)
	require.Equal(t, f.principal.UserID, p.CurrentIdentity.UserID)
	require.Equal(t, "Current fixture name", p.CurrentIdentity.DisplayName)
	require.Equal(t, f.base.Add(time.Hour), p.CurrentIdentity.DirectoryObservedAt)
	read, err := f.service.Read(adminCtx, f.principal, readInput)
	require.NoError(t, err)
	require.Equal(t, p, read.Messages[0].Participants[0])
	for _, state := range []string{"conflict", "deleted_user", "deleted_membership", "revoked_mapping"} {
		err = q.SetPlatformChatEvidenceDirectoryStateFixture(ctx, evidencefixtures.SetPlatformChatEvidenceDirectoryStateFixtureParams{OrganizationID: f.principal.OrganizationID, UserID: f.principal.UserID, State: state, TeamID: ""})
		require.NoError(t, err)
		read, err = f.service.Read(adminCtx, f.principal, readInput)
		require.NoError(t, err)
		require.NotNil(t, read.Messages[0].Participants[0].CurrentIdentity)
		require.Empty(t, read.Messages[0].Participants[0].CurrentIdentity.UserID, state)
	}
	err = q.SetPlatformChatEvidenceDirectoryStateFixture(ctx, evidencefixtures.SetPlatformChatEvidenceDirectoryStateFixtureParams{OrganizationID: f.principal.OrganizationID, UserID: f.principal.UserID, State: "", TeamID: ""})
	require.NoError(t, err)
	input.ResolveParticipantIdentities = false
	page, err = f.service.Search(adminCtx, f.principal, input)
	require.NoError(t, err)
	require.Empty(t, page.Hits, "directory enrichment must be explicit even for administrators")
	err = q.SeedPlatformChatEvidenceDirectoryFixture(ctx, evidencefixtures.SeedPlatformChatEvidenceDirectoryFixtureParams{OrganizationID: f.principal.OrganizationID, TeamID: "TEXAMPLE02", ProviderUserID: "UEXAMPLE01", MappedUserID: "", DisplayName: "Ambiguous fixture", ObservedAt: timestamp(f.base)})
	require.NoError(t, err)
	read, err = f.service.Read(adminCtx, f.principal, readInput)
	require.NoError(t, err)
	require.Nil(t, read.Messages[0].Participants[0].CurrentIdentity, "do not choose an arbitrary workspace")
	err = q.SetPlatformChatEvidenceDirectoryStateFixture(ctx, evidencefixtures.SetPlatformChatEvidenceDirectoryStateFixtureParams{OrganizationID: f.principal.OrganizationID, UserID: f.principal.UserID, State: "disconnected", TeamID: "TEXAMPLE02"})
	require.NoError(t, err)
	read, err = f.service.Read(adminCtx, f.principal, readInput)
	require.NoError(t, err)
	require.NotNil(t, read.Messages[0].Participants[0].CurrentIdentity, "disconnected workspaces cannot create ambiguity")
	err = q.SetPlatformChatEvidenceDirectoryStateFixture(ctx, evidencefixtures.SetPlatformChatEvidenceDirectoryStateFixtureParams{OrganizationID: f.principal.OrganizationID, UserID: f.principal.UserID, State: "disconnected", TeamID: ""})
	require.NoError(t, err)
	read, err = f.service.Read(adminCtx, f.principal, readInput)
	require.NoError(t, err)
	require.Nil(t, read.Messages[0].Participants[0].CurrentIdentity, "disconnected snapshots are not current directory data")
}

func TestChatEvidenceParticipantLimitsAndCursorScope(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	chat := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_owner", "participant-bound", "Fixture", uuid.NullUUID{})
	for i := range 2 {
		message := seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "user", content: "participant_needle", at: f.base.Add(time.Duration(i) * time.Second)})
		for n := range chatEvidenceParticipantLimit + 1 {
			err := evidencefixtures.New(f.db).SeedPlatformChatEvidenceParticipantFixture(ctx, evidencefixtures.SeedPlatformChatEvidenceParticipantFixtureParams{ProjectID: f.project.ID, ChatID: chat, MessageID: message, Provider: "fixture", ProviderUserID: fmt.Sprintf("EXAMPLE%02d", n), DisplayName: strings.Repeat("n", 257)})
			require.NoError(t, err)
		}
	}
	input := f.search("participant_needle")
	input.Participant = &ChatEvidenceParticipantFilter{Provider: "fixture", ProviderUserID: "EXAMPLE16"}
	input.Limit = 1
	page, err := f.service.Search(ctx, f.principal, input)
	require.NoError(t, err)
	require.Len(t, page.Hits, 1)
	require.Len(t, page.Hits[0].Participants, chatEvidenceParticipantLimit)
	require.True(t, page.Hits[0].ParticipantsTruncated)
	require.Equal(t, "[WITHHELD]", page.Hits[0].Participants[0].DisplayName)
	require.NotEmpty(t, page.NextCursor)
	input.Cursor = page.NextCursor
	input.Participant.ProviderUserID = "EXAMPLE01"
	_, err = f.service.Search(ctx, f.principal, input)
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
	input.Participant.ProviderUserID = "EXAMPLE16"
	page, err = f.service.Search(ctx, f.principal, input)
	require.NoError(t, err)
	require.Len(t, page.Hits, 1)
	for _, filter := range []*ChatEvidenceParticipantFilter{{}, {Provider: "slack"}, {ProviderUserID: "UEXAMPLE01"}, {UserID: "fixture", ProviderTeamID: "TEXAMPLE01"}} {
		input.Cursor = ""
		input.Participant = filter
		_, err = f.service.Search(ctx, f.principal, input)
		require.ErrorIs(t, err, errChatEvidenceInvalid)
	}
}

func TestChatEvidenceCoverageCountersDoNotDependOnMatches(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	chat := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_owner", "coverage-reasons", "Fixture", uuid.NullUUID{})
	seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "tool", content: strings.Repeat("x", 131073), at: f.base, contentAssetURL: "https://assets.example.test/offloaded"})
	seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "assistant", content: "ordinary", toolCalls: strings.Repeat("[", chatEvidenceMaxDepth+2) + `"unsearched_fixture"` + strings.Repeat("]", chatEvidenceMaxDepth+2), at: f.base})
	seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "user", content: "A literal [WITHHELD] marker is ordinary text", at: f.base})
	var first ChatSearchProgress
	for i, query := range []string{"not_present_fixture", "unsearched_fixture"} {
		page, err := f.service.Search(ctx, f.principal, f.search(query))
		require.NoError(t, err)
		require.Empty(t, page.Hits)
		require.True(t, page.Progress.Complete)
		require.Equal(t, 2, page.Progress.WithheldRecords)
		require.Equal(t, 1, page.Progress.OffloadedMessages)
		require.Equal(t, 3, page.Progress.MessagesExamined)
		if i == 0 {
			first = page.Progress
		} else {
			require.Equal(t, first, page.Progress)
		}
	}
}

func TestChatEvidenceDepthBoundaryCoverage(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	chat := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_owner", "depth-boundary", "Fixture", uuid.NullUUID{})
	for _, depth := range []int{chatEvidenceMaxDepth - 1, chatEvidenceMaxDepth, chatEvidenceMaxDepth + 1} {
		content := strings.Repeat("[", depth) + `"depth_fixture"` + strings.Repeat("]", depth)
		_, withheld := canonicalEvidenceFieldProjection(content)
		safe, err := f.service.redact(ctx, content, 0)
		require.NoError(t, err)
		require.Equal(t, depth >= chatEvidenceMaxDepth, withheld)
		require.Equal(t, withheld, strings.Contains(safe, "[WITHHELD]"))
		seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "tool", content: content, at: f.base})
	}
	for _, query := range []string{"absent_fixture", "depth_fixture"} {
		page, err := f.service.Search(ctx, f.principal, f.search(query))
		require.NoError(t, err)
		require.Equal(t, 2, page.Progress.WithheldRecords)
	}
}

func TestChatEvidenceParticipantWorkBudgetContinues(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	chat := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_owner", "attribution-budget", "Fixture", uuid.NullUUID{})
	err := evidencefixtures.New(f.db).SeedPlatformChatEvidenceParticipantBatchFixture(ctx, evidencefixtures.SeedPlatformChatEvidenceParticipantBatchFixtureParams{ProjectID: f.project.ID, ChatID: chat, At: timestamp(f.base), MessageCount: 120, ParticipantCount: 17})
	require.NoError(t, err)
	input := f.search("no_such_fixture_text")
	input.Participant = &ChatEvidenceParticipantFilter{Provider: "fixture", ProviderUserID: "EXAMPLE16"}
	total := 0
	for i := range 4 {
		page, err := f.service.Search(ctx, f.principal, input)
		require.NoError(t, err)
		require.Empty(t, page.Hits)
		require.LessOrEqual(t, page.Progress.MessagesExamined, chatSearchParticipantScanLimit)
		require.Equal(t, page.Progress.MessagesExamined, page.Progress.ParticipantFilterIncompleteRecords)
		total += page.Progress.MessagesExamined
		if page.Progress.Complete {
			break
		}
		require.Less(t, i, 3)
		require.Equal(t, "attribution_budget", page.Progress.StopReason)
		input.Cursor = page.NextCursor
	}
	require.Equal(t, 120, total)
}

func TestChatEvidenceParticipantMetadataRedactsBeforeFiltering(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	chat := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_owner", "participant-redaction", "Fixture", uuid.NullUUID{})
	message := seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "user", content: "participant_fixture", at: f.base})
	// Synthetic credential-shaped data is never usable against a provider.
	secret := "ghp_" + strings.Join([]string{"q1W2e3R4t5Y6", "u7I8o9P0a1S2", "d3F4g5H6j7K8"}, "")
	err := evidencefixtures.New(f.db).SeedPlatformChatEvidenceParticipantFixture(ctx, evidencefixtures.SeedPlatformChatEvidenceParticipantFixtureParams{ProjectID: f.project.ID, ChatID: chat, MessageID: message, Provider: "fixture", ProviderUserID: "EXAMPLE01", UserID: secret, DisplayName: secret})
	require.NoError(t, err)
	read, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chat.String(), MessageID: message.String(), ContextSize: new(0)})
	require.NoError(t, err)
	require.Len(t, read.Messages[0].Participants, 1)
	require.NotContains(t, read.Messages[0].Participants[0].UserID, secret)
	require.NotContains(t, read.Messages[0].Participants[0].DisplayName, secret)
	input := f.search("participant_fixture")
	input.Participant = &ChatEvidenceParticipantFilter{UserID: secret}
	page, err := f.service.Search(ctx, f.principal, input)
	require.NoError(t, err)
	require.Empty(t, page.Hits, "raw credential-shaped metadata cannot create a participant match")
}

func TestChatEvidenceSlackEventIDForms(t *testing.T) {
	t.Parallel()
	for _, eventID := range []string{"EvEXAMPLE01", "fixture:EvEXAMPLE01", "EvA"} {
		t.Run(eventID, func(t *testing.T) {
			t.Parallel()
			header := fmt.Sprintf("<message-context>\nEventID: %s\nEventType: message\nUserID: UEXAMPLE01\nTimestamp: 1767614400.123456\n</message-context>\nbody", eventID)
			participant, eventTime := parseSlackEvidenceHeader(slackEvidenceHeader(header, "user", "assistants"))
			require.Equal(t, "UEXAMPLE01", participant)
			require.NotNil(t, eventTime)
		})
	}
}

func TestChatEvidenceLiteralWithheldMarkerAndNumericCounters(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	safe, withheld, err := f.service.evidenceText(ctx, `A literal [WITHHELD] marker`, nil, nil)
	require.NoError(t, err)
	require.False(t, withheld)
	require.Equal(t, `A literal [WITHHELD] marker`, safe)
	safe, withheld, err = f.service.evidenceText(ctx, `{"max_tokens":4096,"prompt_tokens":123,"completion_tokens":45,"total_tokens":168,"access_token":"fixture-credential","token":17,"input_tokens":"fixture-credential"}`, nil, nil)
	require.NoError(t, err)
	require.False(t, withheld)
	for _, value := range []string{`"max_tokens":4096`, `"prompt_tokens":123`, `"completion_tokens":45`, `"total_tokens":168`, `"token":"[REDACTED]"`} {
		require.Contains(t, safe, value)
	}
	require.NotContains(t, safe, "fixture-credential")
}

func TestChatEvidenceInlineAttachmentsPreserveAdjacentText(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	ordinary := `{"type":"file","operation":"delete","path":"config.json"}`
	candidate, hidden := canonicalEvidenceProjection("", []byte(ordinary), nil)
	require.False(t, hidden)
	require.Contains(t, candidate, "config.json")
	safe, withheld, err := f.service.evidenceText(ctx, "", []byte(ordinary), nil)
	require.NoError(t, err)
	require.False(t, withheld)
	require.Contains(t, safe, `"operation":"delete"`)
	payload := `{"content":[{"type":"text","text":"visible_fixture_text"},{"type":"image_url","image_url":{"url":"https://example.test/hidden_fixture_image"}},{"type":"image","source":{"type":"base64","data":"hidden_fixture_base64"}},{"type":"file","file_id":"hidden_fixture_file"}]}`
	nested, err := json.Marshal(map[string]string{"result": payload})
	require.NoError(t, err)
	for _, raw := range []string{payload, string(nested)} {
		candidate, hidden := canonicalEvidenceProjection("", nil, []byte(raw))
		require.True(t, hidden)
		safe, withheld, err := f.service.evidenceText(ctx, "", nil, []byte(raw))
		require.NoError(t, err)
		require.True(t, withheld)
		require.Contains(t, safe, "visible_fixture_text")
		require.Contains(t, candidate, "visible_fixture_text")
		for _, omitted := range []string{"hidden_fixture_image", "hidden_fixture_base64", "hidden_fixture_file"} {
			require.NotContains(t, candidate, omitted)
			require.NotContains(t, safe, omitted)
		}
	}
	chat := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_owner", "inline-parts", "Fixture", uuid.NullUUID{})
	message := seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "tool", content: payload, at: f.base})
	for _, query := range []string{"hidden_fixture_image", "visible_fixture_text", "absent_fixture"} {
		page, err := f.service.Search(ctx, f.principal, f.search(query))
		require.NoError(t, err)
		require.Equal(t, 1, page.Progress.WithheldRecords)
		if query == "visible_fixture_text" {
			require.Len(t, page.Hits, 1)
		} else {
			require.Empty(t, page.Hits)
		}
	}
	read, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chat.String(), MessageID: message.String(), ContextSize: new(0)})
	require.NoError(t, err)
	require.True(t, read.Messages[0].Withheld)
	require.NotContains(t, read.Messages[0].Text, "hidden_fixture")
}

func TestChatEvidenceUnicodeQueryLimit(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	chat := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_owner", "unicode-limit", "Fixture", uuid.NullUUID{})
	seedRecallMessage(t, ctx, f.db, chat, f.project.ID, recallMessageSeed{role: "user", content: strings.Repeat("🙂", 256), at: f.base})
	page, err := f.service.Search(ctx, f.principal, f.search(strings.Repeat("🙂", 256)))
	require.NoError(t, err)
	require.Len(t, page.Hits, 1)
	_, err = f.service.Search(ctx, f.principal, f.search(strings.Repeat("🙂", 257)))
	require.ErrorIs(t, err, errChatEvidenceInvalid)
}
