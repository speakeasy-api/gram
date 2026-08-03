package aiintegrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	codexapi "github.com/speakeasy-api/gram/server/internal/thirdparty/codex"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const chatgptConversationFixture = `{"event_id":"evt_1","type":"CONVERSATION_MESSAGE","principal":{"id":"ws_1","type":"CHATGPT_WORKSPACE"},"actor":{"type":"ACCOUNT_USER","user_id":"oai_user_1","user_email":"ada@example.com"},"timestamp":"2026-07-27T10:00:00Z","previous_message_id":"","message":{"id":"msg_1","created_at":"2026-07-27T10:00:00Z","author":{"type":"user","client_type":"desktop_web"},"content":{"type":"text","value":"What is our refund policy?"}},"conversation":{"id":"conv_1","title":"Refund policy","created_at":"2026-07-27T09:59:58Z","is_pinned":false,"is_temporary_chat":false}}
{"event_id":"evt_2","type":"CONVERSATION_MESSAGE","principal":{"id":"ws_1","type":"CHATGPT_WORKSPACE"},"actor":{"type":"ACCOUNT_USER","user_id":"oai_user_1","user_email":"ada@example.com"},"timestamp":"2026-07-27T10:00:05Z","previous_message_id":"msg_1","message":{"id":"msg_2","created_at":"2026-07-27T10:00:05Z","author":{"type":"assistant","client_type":"desktop_web"},"content":{"type":"text","value":"Our refund policy allows returns within 30 days."}},"conversation":{"id":"conv_1","title":"Refund policy","created_at":"2026-07-27T09:59:58Z","is_pinned":false,"is_temporary_chat":false}}
{"event_id":"evt_3","type":"CONVERSATION_MESSAGE","principal":{"id":"ws_1","type":"CHATGPT_WORKSPACE"},"actor":{"type":"ACCOUNT_USER","user_id":"oai_user_1","user_email":"ada@example.com"},"timestamp":"2026-07-27T10:00:06Z","previous_message_id":"msg_2","message":{"id":"msg_3","created_at":"2026-07-27T10:00:06Z","author":{"type":"tool","client_type":"desktop_web"},"content":{"type":"text","value":"tool output"}},"conversation":{"id":"conv_1","title":"Refund policy","created_at":"2026-07-27T09:59:58Z","is_pinned":false,"is_temporary_chat":false}}
`

func chatgptFixtureFile(body string) codexapi.LogFile {
	sum := sha256.Sum256([]byte(body))
	return codexapi.LogFile{
		ID:         "eclf_conv_1",
		EventType:  chatgptConversationEventType,
		EndTime:    time.Date(2026, 7, 27, 10, 19, 31, 0, time.UTC),
		FileName:   "CONVERSATION_MESSAGE_2026-07-27T10:19:31.jsonl",
		FileSize:   int64(len(body)),
		FileSHA256: hex.EncodeToString(sum[:]),
	}
}

func TestParseChatGPTConversationEventsVerifiesSHAAndSkipsForeignTypes(t *testing.T) {
	t.Parallel()

	file := chatgptFixtureFile(chatgptConversationFixture)
	events, err := parseChatGPTConversationEvents(file, []byte(chatgptConversationFixture))
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.Equal(t, "conv_1", events[0].Conversation.ID)
	require.Equal(t, "What is our refund policy?", renderChatGPTContent(events[0].Message.Content.Value))
	require.Equal(t, "user", events[0].Message.Author.Type)
	require.Equal(t, "assistant", events[1].Message.Author.Type)

	file.FileSHA256 = "not-the-right-hash"
	_, err = parseChatGPTConversationEvents(file, []byte(chatgptConversationFixture))
	require.Error(t, err)
	require.Contains(t, err.Error(), "sha256 mismatch")
}

func TestRenderChatGPTContentFallsBackToRawJSON(t *testing.T) {
	t.Parallel()

	require.Equal(t, "plain text", renderChatGPTContent([]byte(`"plain text"`)))
	require.JSONEq(t, `{"parts":["a"]}`, renderChatGPTContent([]byte(`{"parts":["a"]}`)))
	require.Empty(t, renderChatGPTContent(nil))
}

func TestChatGPTConversationProcessPageWritesChatAndMessagesIdempotently(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "ChatGPT Import Test Project",
		Slug:           "project-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	userRow, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          "user_" + uuid.NewString(),
		Email:       "ada@example.com",
		DisplayName: "Ada",
		PhotoUrl:    conv.ToPGTextEmpty(""),
		Admin:       false,
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(conn).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{
		OrganizationID: orgID,
		UserID:         conv.ToPGText(userRow.ID),
	}))

	workspaceID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderChatGPTCompliance, "chatgpt-key", true, true, &workspaceID, nil)
	cfg := created.Config
	cfg.ProjectID = project.ID

	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, nil)
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	svc := NewChatGPTConversationImportService(testenv.NewLogger(t), store, conn, nil, writer, func(context.Context, int) {})
	file := chatgptFixtureFile(chatgptConversationFixture)
	src := &chatgptConversationSource{
		client: &stubCodexComplianceClient{
			listPages:  nil,
			listParams: nil,
			downloads:  map[string][]byte{file.ID: []byte(chatgptConversationFixture)},
		},
		svc:       svc,
		cfg:       cfg,
		pageLimit: chatgptCompliancePageLimit,
		users:     newConnectedUserResolver(conn, orgID),
		chatIDs:   map[string]uuid.UUID{},
		progress:  &ChatGPTConversationSyncProgress{},
	}

	require.NoError(t, src.ProcessPage(ctx, []codexapi.LogFile{file}))

	chatID, ok := src.chatIDs["conv_1"]
	require.True(t, ok, "conversation must be upserted")
	chatRow, err := chatrepo.New(conn).GetChat(ctx, chatID)
	require.NoError(t, err)
	require.Equal(t, "Refund policy", chatRow.Title.String)
	require.Equal(t, "conv_1", chatRow.ExternalChatID.String)
	require.Equal(t, userRow.ID, chatRow.UserID.String)

	messages, err := chatrepo.New(conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: chatID, ProjectID: project.ID})
	require.NoError(t, err)
	// The tool-role event is skipped; only user + assistant land.
	require.Len(t, messages, 2)
	require.Equal(t, "user", messages[0].Role)
	require.Equal(t, "What is our refund policy?", messages[0].Content)
	require.Equal(t, "assistant", messages[1].Role)
	require.Equal(t, chatgptConversationSourceSlug, messages[0].Source.String)
	require.Equal(t, "msg_1", messages[0].ExternalMessageID.String)
	require.Equal(t, "desktop_web", messages[0].UserAgent.String)
	require.Equal(t, userRow.ID, messages[0].UserID.String)

	// Replaying the same file must not duplicate messages: the insert
	// dedupes on (chat_id, external_message_id).
	src.chatIDs = map[string]uuid.UUID{}
	require.NoError(t, src.ProcessPage(ctx, []codexapi.LogFile{file}))
	messages, err = chatrepo.New(conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: chatID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Len(t, messages, 2)
}
