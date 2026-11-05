package chat_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
)

func seedSlackParticipantThread(t *testing.T, ti *chatTestInstance, ctx context.Context, source string) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	q := repo.New(ti.conn)
	assistantID, err := q.SeedAssistant(ctx, repo.SeedAssistantParams{ProjectID: ti.projectID, OrganizationID: ti.orgID, Name: "Project Assistant"})
	require.NoError(t, err)
	chatID := seedChat(t, ctx, ti, "demo_assistant_creator", "", "Slack invocation")
	threadID, err := assistantsrepo.New(ti.conn).UpsertAssistantThread(ctx, assistantsrepo.UpsertAssistantThreadParams{AssistantID: assistantID, ProjectID: ti.projectID, ChatID: chatID, CorrelationID: uuid.NewString(), SourceKind: source, SourceRefJson: []byte(`{}`)})
	require.NoError(t, err)
	return assistantID, threadID, chatID
}

func seedSlackParticipantEvent(t *testing.T, ti *chatTestInstance, assistantID, threadID uuid.UUID, payload string) uuid.UUID {
	t.Helper()
	require.True(t, json.Valid([]byte(payload)))
	id, err := assistantsrepo.New(ti.conn).InsertAssistantThreadEvent(t.Context(), assistantsrepo.InsertAssistantThreadEventParams{AssistantID: assistantID, AssistantThreadID: threadID, ProjectID: ti.projectID, EventID: uuid.NewString(), CorrelationID: uuid.NewString(), Status: "completed", NormalizedPayloadJson: []byte(payload), SourcePayloadJson: []byte(`{}`)})
	require.NoError(t, err)
	return id
}

func TestAssistantSlackParticipantsListAndLoad(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	assistantID, threadID, chatID := seedSlackParticipantThread(t, ti, ctx, "speakeasy_slack")
	seedTagDirectory(t, ti, "T_INVOKING", "U_FIRST", "Invoking Person")
	seedTagDirectory(t, ti, "T_OTHER_WORKSPACE", "U_SECOND", "Wrong workspace person")
	sq := slackrepo.New(ti.conn)
	require.NoError(t, sq.CreateSlackMappingPersonForTest(ctx, slackrepo.CreateSlackMappingPersonForTestParams{OrganizationID: ti.orgID, UserID: "demo_invoking_user"}))
	_, err := sq.CreateSlackMappingForTest(ctx, slackrepo.CreateSlackMappingForTestParams{OrganizationID: ti.orgID, SlackTeamID: "T_INVOKING", SlackUserID: "U_FIRST", UserID: "demo_invoking_user"})
	require.NoError(t, err)
	_, err = ti.conn.Exec(ctx, "UPDATE assistants SET created_by_user_id = 'demo_assistant_creator' WHERE id = $1", assistantID)
	require.NoError(t, err)
	// Context and backfilled history are not invocation identities. Exact keys
	// and the server's source stamp must win over thread defaults.
	seedSlackParticipantEvent(t, ti, assistantID, threadID, `{"team_id":"T_INVOKING","user_id":"U_FIRST","context":[{"user_id":"U_CONTEXT"}],"backfill":[{"user_id":"U_HISTORY"}]}`)
	seedSlackParticipantEvent(t, ti, assistantID, threadID, `{"team_id":"T_INVOKING","user_id":"U_FIRST"}`)
	seedSlackParticipantEvent(t, ti, assistantID, threadID, `{"_gram_source_kind":"slack","team_id":"T_INVOKING","user_id":"U_SECOND"}`)
	for _, payload := range []string{
		`{"_gram_source_kind":"dashboard","team_id":"T_INVOKING","user_id":"U_NOT_SLACK"}`,
		`{"_gram_source_kind":"","team_id":"T_INVOKING","user_id":"U_EMPTY_STAMP"}`,
		`{"Team_id":"T_INVOKING","User_id":"U_WRONG_CASE"}`,
		`{"team_id":"T_INVOKING","context":[{"user_id":"U_CONTEXT"}]}`,
		`{"team_id":"T_INVOKING","user_id":42}`,
	} {
		seedSlackParticipantEvent(t, ti, assistantID, threadID, payload)
	}
	// Give the session a real transcript entry so normal listing includes it.
	messageID, err := repo.New(ti.conn).SeedChatMessage(ctx, repo.SeedChatMessageParams{ChatID: chatID, ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true}, CreatedAt: conv.ToPGTimestamptz(time.Now())})
	require.NoError(t, err)
	// An existing snapshot for the same workspace and sender must not double
	// count the invoker.
	require.NoError(t, repo.New(ti.conn).RecordSlackMessageParticipant(ctx, repo.RecordSlackMessageParticipantParams{ProjectID: ti.projectID, MessageID: messageID, TeamID: "T_INVOKING", ProviderUserID: "U_FIRST"}))
	loaded, err := ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.NoError(t, err)
	require.Equal(t, "demo_assistant_creator", conv.PtrValOr(loaded.UserID, ""))
	require.Len(t, loaded.Participants, 2)
	require.Equal(t, "U_FIRST", loaded.Participants[0].ProviderUserID)
	require.Equal(t, "demo_invoking_user", conv.PtrValOr(loaded.Participants[0].UserID, ""))
	require.Equal(t, "Invoking Person", conv.PtrValOr(loaded.Participants[0].DisplayName, ""))
	require.Equal(t, "U_SECOND", loaded.Participants[1].ProviderUserID)
	require.Equal(t, "U_SECOND", conv.PtrValOr(loaded.Participants[1].DisplayName, ""))
	require.Nil(t, loaded.Participants[1].UserID)
	require.Len(t, loaded.Messages[0].Participants, 1)
	require.Equal(t, "U_FIRST", loaded.Messages[0].Participants[0].ProviderUserID)
	listed, err := ti.service.ListChats(ctx, defaultPayload())
	require.NoError(t, err)
	require.Len(t, listed.Chats, 1)
	require.Equal(t, loaded.Participants, listed.Chats[0].Participants)
	require.Equal(t, "demo_assistant_creator", conv.PtrValOr(listed.Chats[0].UserID, ""))
	stored, err := repo.New(ti.conn).GetChat(ctx, repo.GetChatParams{ProjectID: ti.projectID, ID: chatID})
	require.NoError(t, err)
	require.Equal(t, "demo_assistant_creator", stored.UserID.String)
	// A historical snapshot can predate mapping. Deduplicate by Slack identity,
	// not its optional Speakeasy user ID; message attribution stays untouched.
	_, err = ti.conn.Exec(ctx, "UPDATE chat_message_participants SET user_id = NULL WHERE message_id = $1", messageID)
	require.NoError(t, err)
	loaded, err = ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.NoError(t, err)
	require.Len(t, loaded.Participants, 2)
	require.Nil(t, loaded.Messages[0].Participants[0].UserID)
}

func TestAssistantSlackParticipantsIsolationAndDeletion(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	assistantID, threadID, chatID := seedSlackParticipantThread(t, ti, ctx, "slack")
	eventID := seedSlackParticipantEvent(t, ti, assistantID, threadID, `{"team_id":"T_SHARED","user_id":"U_SHARED"}`)
	// Same Slack IDs in another organization must not resolve the local sender.
	otherOrg := "org-" + uuid.NewString()
	_, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: otherOrg, Name: "Other synthetic organization", Slug: otherOrg})
	require.NoError(t, err)
	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{OrganizationID: otherOrg, Name: "Other project", Slug: "other-project"})
	require.NoError(t, err)
	other := *ti
	other.orgID, other.projectID = otherOrg, project.ID
	seedTagDirectory(t, &other, "T_SHARED", "U_SHARED", "Other tenant person")
	sq := slackrepo.New(ti.conn)
	require.NoError(t, sq.CreateSlackMappingPersonForTest(ctx, slackrepo.CreateSlackMappingPersonForTestParams{OrganizationID: otherOrg, UserID: "demo_other_tenant"}))
	_, err = sq.CreateSlackMappingForTest(ctx, slackrepo.CreateSlackMappingForTestParams{OrganizationID: otherOrg, SlackTeamID: "T_SHARED", SlackUserID: "U_SHARED", UserID: "demo_other_tenant"})
	require.NoError(t, err)
	q := repo.New(ti.conn)
	params := repo.ListAssistantSlackParticipantsParams{ProjectID: ti.projectID, ChatIds: []uuid.UUID{chatID}}
	rows, err := q.ListAssistantSlackParticipants(ctx, params)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "U_SHARED", rows[0].DisplayName)
	require.False(t, rows[0].UserID.Valid)
	// Explicit chat-ID admission and project scope are both required.
	for _, denied := range []repo.ListAssistantSlackParticipantsParams{
		{ProjectID: ti.projectID, ChatIds: []uuid.UUID{uuid.New()}},
		{ProjectID: project.ID, ChatIds: []uuid.UUID{chatID}},
		{ProjectID: ti.projectID, ChatIds: nil},
	} {
		hidden, err := q.ListAssistantSlackParticipants(ctx, denied)
		require.NoError(t, err)
		require.Empty(t, hidden)
	}
	// An explicit Slack stamp works even on a thread originating elsewhere.
	_, err = ti.conn.Exec(ctx, "UPDATE assistant_threads SET source_kind = 'dashboard' WHERE id = $1", threadID)
	require.NoError(t, err)
	hidden, err := q.ListAssistantSlackParticipants(ctx, params)
	require.NoError(t, err)
	require.Empty(t, hidden)
	_, err = ti.conn.Exec(ctx, `UPDATE assistant_thread_events SET normalized_payload_json = normalized_payload_json || '{"_gram_source_kind":"speakeasy_slack"}'::jsonb WHERE id = $1`, eventID)
	require.NoError(t, err)
	rows, err = q.ListAssistantSlackParticipants(ctx, params)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	for _, target := range []struct {
		table string
		id    uuid.UUID
	}{
		{"assistant_thread_events", eventID}, {"assistant_threads", threadID}, {"assistants", assistantID}, {"chats", chatID},
	} {
		_, err = ti.conn.Exec(ctx, "UPDATE "+target.table+" SET deleted_at = clock_timestamp() WHERE id = $1", target.id)
		require.NoError(t, err)
		hidden, err := q.ListAssistantSlackParticipants(ctx, params)
		require.NoError(t, err)
		require.Empty(t, hidden, target.table)
		_, err = ti.conn.Exec(ctx, "UPDATE "+target.table+" SET deleted_at = NULL WHERE id = $1", target.id)
		require.NoError(t, err)
	}
}
