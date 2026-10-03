package chat_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func tagWriter(t *testing.T, ti *chatTestInstance) *chat.ChatMessageWriter {
	t.Helper()
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { require.NoError(t, shutdown(t.Context())) })
	return writer
}

func tagWrite(t *testing.T, ti *chatTestInstance, chatID uuid.UUID, content string) chat.MessageWrite {
	t.Helper()
	write := chat.MessageWrite{}
	write.Params = minimalChatMessageParams(chatID, ti.projectID)
	write.Params.Content = content
	write.Params.Source = conv.ToPGText("claude-code")
	write.Params.UserID = conv.ToPGText("demo_device_owner")
	write.Params.ExternalUserID = conv.ToPGText("demo_device_identity")
	return write
}

func seedTagDirectory(t *testing.T, ti *chatTestInstance, team, sender, name string) {
	t.Helper()
	q := slackrepo.New(ti.conn)
	_, err := q.CreateSlackDirectoryConnection(t.Context(), slackrepo.CreateSlackDirectoryConnectionParams{
		OrganizationID: ti.orgID, SlackTeamID: team, SlackTeamName: conv.ToPGText("Demo Workspace"),
		CredentialsEncrypted: pgtype.Text{}, GrantedScopes: []string{}, Generation: uuid.New(),
	})
	require.NoError(t, err)
	require.NoError(t, q.UpsertSlackDirectoryMembershipBatch(t.Context(), slackrepo.UpsertSlackDirectoryMembershipBatchParams{
		OrganizationID: ti.orgID, SlackTeamID: team, LastSeenAt: conv.ToPGTimestamptz(time.Now()),
		UserIds: []string{sender}, DisplayNames: []string{name}, Emails: []string{""},
		Statuses: []string{"active"}, MemberTypes: []string{"person"}, ProviderUpdatedAts: []pgtype.Timestamptz{{}},
	}))
}

func TestClaudeTagParticipantsArePerMessageSnapshots(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "", "", "Demo conversation")
	seedTagDirectory(t, ti, "T_DEMO_ONE", "U_DEMO_ONE", "First Person")
	seedTagDirectory(t, ti, "T_DEMO_TWO", "U_DEMO_TWO", "Second Person")
	q := slackrepo.New(ti.conn)
	require.NoError(t, q.CreateSlackMappingPersonForTest(ctx, slackrepo.CreateSlackMappingPersonForTestParams{OrganizationID: ti.orgID, UserID: "demo_mapped_person"}))
	_, err := q.CreateSlackMappingForTest(ctx, slackrepo.CreateSlackMappingForTestParams{OrganizationID: ti.orgID, SlackTeamID: "T_DEMO_ONE", SlackUserID: "U_DEMO_ONE", UserID: "demo_mapped_person"})
	require.NoError(t, err)
	writer := tagWriter(t, ti)
	writes := []chat.MessageWrite{
		tagWrite(t, ti, chatID, `<standing_owner_message sender="U_DEMO_ONE">First ask</standing_owner_message>`),
		tagWrite(t, ti, chatID, `<standing_owner_message sender="U_DEMO_TWO">Second ask</standing_owner_message>`),
	}
	n, err := writer.Write(ctx, ti.projectID, writes)
	require.NoError(t, err)
	require.Equal(t, int64(2), n)
	rows, err := repo.New(ti.conn).ListChatParticipants(ctx, repo.ListChatParticipantsParams{ProjectID: ti.projectID, ChatIds: []uuid.UUID{chatID}})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	if rows[0].MessageID.UUID != writes[0].Params.ID {
		rows[0], rows[1] = rows[1], rows[0]
	}
	require.Equal(t, writes[0].Params.ID, rows[0].MessageID.UUID)
	require.Equal(t, "demo_mapped_person", rows[0].UserID.String)
	require.Equal(t, "First Person", rows[0].DisplayName.String)
	require.Equal(t, writes[1].Params.ID, rows[1].MessageID.UUID)
	require.False(t, rows[1].UserID.Valid)
	require.NoError(t, q.RevokeSlackIdentityMapping(ctx, slackrepo.RevokeSlackIdentityMappingParams{OrganizationID: ti.orgID, SlackTeamID: "T_DEMO_ONE", SlackUserID: "U_DEMO_ONE"}))
	rows, err = repo.New(ti.conn).ListChatParticipants(ctx, repo.ListChatParticipantsParams{ProjectID: ti.projectID, ChatIds: []uuid.UUID{chatID}})
	require.NoError(t, err)
	if rows[0].MessageID.UUID != writes[0].Params.ID {
		rows[0], rows[1] = rows[1], rows[0]
	}
	require.Equal(t, "demo_mapped_person", rows[0].UserID.String)
	messages, err := repo.New(ti.conn).ListChatMessages(ctx, repo.ListChatMessagesParams{ProjectID: ti.projectID, ChatID: chatID})
	require.NoError(t, err)
	require.Equal(t, "demo_device_owner", messages[0].UserID.String)
	require.Equal(t, "demo_device_identity", messages[0].ExternalUserID.String)
	stored, err := repo.New(ti.conn).GetChat(ctx, repo.GetChatParams{ID: chatID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.Equal(t, "claude-tag", stored.SessionSurface.String)
	loaded, err := ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.NoError(t, err)
	require.Len(t, loaded.Participants, 2)
	require.Len(t, loaded.Messages, 2)
	require.Equal(t, "U_DEMO_ONE", loaded.Messages[0].Participants[0].ProviderUserID)
	require.Equal(t, "U_DEMO_TWO", loaded.Messages[1].Participants[0].ProviderUserID)
	later := tagWrite(t, ti, chatID, "ordinary follow-up")
	_, err = writer.Write(ctx, ti.projectID, []chat.MessageWrite{later})
	require.NoError(t, err)
	loaded, err = ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.NoError(t, err)
	require.Equal(t, "claude-tag", conv.PtrValOr(loaded.Source, ""))
	payload := defaultPayload()
	payload.Source = conv.PtrEmpty("claude-tag")
	listed, err := ti.service.ListChats(ctx, payload)
	require.NoError(t, err)
	require.Len(t, listed.Chats, 1)
	require.Len(t, listed.Chats[0].Participants, 2)

	require.NoError(t, q.UpsertSlackDirectoryMembershipBatch(ctx, slackrepo.UpsertSlackDirectoryMembershipBatchParams{
		OrganizationID: ti.orgID, SlackTeamID: "T_DEMO_ONE", LastSeenAt: conv.ToPGTimestamptz(time.Now()),
		UserIds: []string{"U_DEMO_ONE"}, DisplayNames: []string{"Renamed Person"}, Emails: []string{""},
		Statuses: []string{"active"}, MemberTypes: []string{"person"}, ProviderUpdatedAts: []pgtype.Timestamptz{{}},
	}))
	_, err = writer.Write(ctx, ti.projectID, []chat.MessageWrite{tagWrite(t, ti, chatID, `<standing_owner_message sender="U_DEMO_ONE">Follow-up</standing_owner_message>`)})
	require.NoError(t, err)
	loaded, err = ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.NoError(t, err)
	require.Len(t, loaded.Participants, 2)
	require.Equal(t, "First Person", conv.PtrValOr(loaded.Messages[0].Participants[0].DisplayName, ""))
	require.Equal(t, "Renamed Person", conv.PtrValOr(loaded.Participants[0].DisplayName, ""))

	hidden, err := repo.New(ti.conn).ListChatParticipants(ctx, repo.ListChatParticipantsParams{ProjectID: uuid.New(), ChatIds: []uuid.UUID{chatID}})
	require.NoError(t, err)
	require.Empty(t, hidden)
}

func TestClaudeTagAmbiguousDirectoryDoesNotGuess(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "", "", "Demo conversation")
	seedTagDirectory(t, ti, "T_DEMO_ONE", "U_DEMO_SHARED", "First Person")
	seedTagDirectory(t, ti, "T_DEMO_TWO", "U_DEMO_SHARED", "Other Person")
	_, err := tagWriter(t, ti).Write(ctx, ti.projectID, []chat.MessageWrite{tagWrite(t, ti, chatID, `<standing_owner_message sender="U_DEMO_SHARED">ask</standing_owner_message>`)})
	require.NoError(t, err)
	rows, err := repo.New(ti.conn).ListChatParticipants(ctx, repo.ListChatParticipantsParams{ProjectID: ti.projectID, ChatIds: []uuid.UUID{chatID}})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "U_DEMO_SHARED", rows[0].ProviderUserID)
	require.False(t, rows[0].ProviderTeamID.Valid)
	require.False(t, rows[0].DisplayName.Valid)
}

func TestClaudeTagHelperLinkDirectionAndCycleRejection(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	parentID := seedChat(t, ctx, ti, "", "", "Parent")
	childID := seedChat(t, ctx, ti, "", "", "Helper")
	writer := tagWriter(t, ti)
	deliver := func(receiver, sender uuid.UUID) {
		t.Helper()
		_, err := writer.Write(ctx, ti.projectID, []chat.MessageWrite{tagWrite(t, ti, receiver, `<cross-session-message from-session="`+sender.String()+`" standing-audience="parent">Done.</cross-session-message>`)})
		require.NoError(t, err)
	}
	deliver(parentID, childID)
	deliver(parentID, childID)
	deliver(childID, parentID)
	links, err := repo.New(ti.conn).ListChatSessionLinks(ctx, repo.ListChatSessionLinksParams{ProjectID: ti.projectID, ChatIds: []uuid.UUID{parentID}, ExternalUserID: "", UserID: ""})
	require.NoError(t, err)
	require.Len(t, links, 1)
	require.Equal(t, parentID, links[0].ParentChatID)
	require.Equal(t, childID, links[0].ChildChatID.UUID)
	require.Equal(t, "subagent", links[0].Kind)
	child, err := repo.New(ti.conn).GetChat(ctx, repo.GetChatParams{ID: childID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.Equal(t, "claude-tag", child.CapturedSurface)
}

func TestClaudeTagMetadataRollsBackWithMessage(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "", "", "Demo conversation")
	tx := testenv.BeginTx(t, ctx, ti.conn)
	_, err := tagWriter(t, ti).WriteInTx(ctx, tx, []chat.MessageWrite{tagWrite(t, ti, chatID, `<standing_owner_message sender="U_DEMO_ONE">ask</standing_owner_message>`)})
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))
	rows, err := repo.New(ti.conn).ListChatParticipants(ctx, repo.ListChatParticipantsParams{ProjectID: ti.projectID, ChatIds: []uuid.UUID{chatID}})
	require.NoError(t, err)
	require.Empty(t, rows)
	stored, err := repo.New(ti.conn).GetChat(ctx, repo.GetChatParams{ID: chatID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.False(t, stored.SessionSurface.Valid)
}

func TestClaudeTagHelperLinkSurvivesCaptureOrder(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	parentID := seedChat(t, ctx, ti, "", "", "Parent")
	nativeHelperID := "session_demo_uncaptured_helper"
	childID := chat.SessionIDToChatID(nativeHelperID)
	writer := tagWriter(t, ti)
	_, err := writer.Write(ctx, ti.projectID, []chat.MessageWrite{tagWrite(t, ti, parentID, `<wake><channel id="C_DEMO" name="demo-team"><message from="human" author-id="U_DEMO_ONE">hello</message></channel></wake>`)})
	require.NoError(t, err)
	_, err = writer.Write(ctx, ti.projectID, []chat.MessageWrite{tagWrite(t, ti, parentID,
		`<cross-session-message from-session="`+nativeHelperID+`" standing-audience="parent">Done.</cross-session-message>`)})
	require.NoError(t, err)
	q := repo.New(ti.conn)
	links, err := q.ListChatSessionLinks(ctx, repo.ListChatSessionLinksParams{ProjectID: ti.projectID, ChatIds: []uuid.UUID{parentID}, ExternalUserID: "", UserID: ""})
	require.NoError(t, err)
	require.Len(t, links, 1)
	require.Equal(t, childID, links[0].ChildChatID.UUID)
	require.False(t, links[0].ChildCaptured)
	_, err = q.UpsertChat(ctx, repo.UpsertChatParams{ID: childID, ProjectID: ti.projectID, OrganizationID: ti.orgID, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{}, Title: conv.ToPGText("Helper")})
	require.NoError(t, err)
	helperWrite := tagWrite(t, ti, childID, "Helper started")
	helperWrite.Params.Source = conv.ToPGText("claude-code-web")
	_, err = writer.Write(ctx, ti.projectID, []chat.MessageWrite{helperWrite})
	require.NoError(t, err)
	child, err := q.GetChat(ctx, repo.GetChatParams{ID: childID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.Equal(t, "claude-tag", child.SessionSurface.String)
	links, err = q.ListChatSessionLinks(ctx, repo.ListChatSessionLinksParams{ProjectID: ti.projectID, ChatIds: []uuid.UUID{parentID}, ExternalUserID: "", UserID: ""})
	require.NoError(t, err)
	require.Len(t, links, 1)
	require.True(t, links[0].ChildCaptured)
	loaded, err := ti.service.LoadChat(ctx, loadPayload(childID.String()))
	require.NoError(t, err)
	require.Equal(t, "claude-tag", conv.PtrValOr(loaded.Source, ""))
	require.Equal(t, "C_DEMO", conv.PtrValOr(loaded.SlackChannelID, ""))
	require.Equal(t, "demo-team", conv.PtrValOr(loaded.SlackChannelName, ""))
	hiddenChannels, err := q.ListChatSlackChannels(ctx, repo.ListChatSlackChannelsParams{ProjectID: ti.projectID, ChatIds: []uuid.UUID{childID}, ExternalUserID: "hidden_parent_owner", UserID: ""})
	require.NoError(t, err)
	require.Len(t, hiddenChannels, 1)
	require.Empty(t, hiddenChannels[0].SlackChannelID)
	grandchildID := seedChat(t, ctx, ti, "", "", "Nested helper")
	_, err = writer.Write(ctx, ti.projectID, []chat.MessageWrite{tagWrite(t, ti, childID, `<cross-session-message from-session="`+grandchildID.String()+`" standing-audience="parent">Nested helper done.</cross-session-message>`)})
	require.NoError(t, err)
	loaded, err = ti.service.LoadChat(ctx, loadPayload(grandchildID.String()))
	require.NoError(t, err)
	require.Equal(t, "C_DEMO", conv.PtrValOr(loaded.SlackChannelID, ""))
	require.Equal(t, "demo-team", conv.PtrValOr(loaded.SlackChannelName, ""))
}

func TestClaudeTagWakeOverridesReportedSourceAndPersistsChannel(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"Claude In Slack", "claude-code-web", "claude-desktop", "Claude Chat Desktop", "Claude Cowork", "cowork"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			ti := newTestChatService(t)
			ctx := initSessionCtx(t, ti)
			id := seedChat(t, ctx, ti, "", "", "Demo channel session")
			seedTagDirectory(t, ti, "T_DEMO", "U_DEMO_ONE", "First Person")
			write := tagWrite(t, ti, id, "<session-context nonce=\"demo\">\nChannel: #demo-team (id: `C_DEMO`)\nWorkspace: `T_DEMO`\n</session-context nonce=\"demo\">\n"+`<wake><channel id="C_DEMO"><message from="human" author-id="U_DEMO_ONE">first</message><message from="human" author-id="U_DEMO_TWO" trigger="true">second</message><message from="sibling" author-id="B_DEMO">bot</message></channel></wake>`)
			write.Params.Source = conv.ToPGText(source)
			_, err := tagWriter(t, ti).Write(ctx, ti.projectID, []chat.MessageWrite{write})
			require.NoError(t, err)
			loaded, err := ti.service.LoadChat(ctx, loadPayload(id.String()))
			require.NoError(t, err)
			require.Equal(t, "claude-tag", conv.PtrValOr(loaded.Source, ""))
			require.Equal(t, "T_DEMO", conv.PtrValOr(loaded.SlackTeamID, ""))
			require.Equal(t, "C_DEMO", conv.PtrValOr(loaded.SlackChannelID, ""))
			require.Equal(t, "demo-team", conv.PtrValOr(loaded.SlackChannelName, ""))
			require.Len(t, loaded.Messages[0].Participants, 2)
			listed, err := ti.service.ListChats(ctx, defaultPayload())
			require.NoError(t, err)
			require.Equal(t, "C_DEMO", conv.PtrValOr(listed.Chats[0].SlackChannelID, ""))
		})
	}
}

func TestClaudeTagEnvelopeDoesNotOverrideNonClaudeSource(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	id := seedChat(t, ctx, ti, "", "", "Other agent")
	write := tagWrite(t, ti, id, `<standing_owner_message sender="U_DEMO_ONE">example</standing_owner_message>`)
	write.Params.Source = conv.ToPGText("codex")
	_, err := tagWriter(t, ti).Write(ctx, ti.projectID, []chat.MessageWrite{write})
	require.NoError(t, err)
	got, err := repo.New(ti.conn).GetChat(ctx, repo.GetChatParams{ID: id, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.False(t, got.SessionSurface.Valid)
}

func TestClaudeTagEnvelopeSupportsClaudeProxy(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	id := seedChat(t, ctx, ti, "", "", "Claude proxy")
	write := tagWrite(t, ti, id, `<standing_owner_message sender="U_DEMO_ONE">hello</standing_owner_message>`)
	write.Params.Source = conv.ToPGText("litellm")
	write.Params.UserAgent = conv.ToPGText("claude-code")
	_, err := tagWriter(t, ti).Write(ctx, ti.projectID, []chat.MessageWrite{write})
	require.NoError(t, err)
	got, err := repo.New(ti.conn).GetChat(ctx, repo.GetChatParams{ID: id, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.Equal(t, "claude-tag", got.SessionSurface.String)
}
