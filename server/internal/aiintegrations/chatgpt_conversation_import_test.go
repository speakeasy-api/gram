package aiintegrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/timewindowpoller"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	codexapi "github.com/speakeasy-api/gram/server/internal/thirdparty/codex"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// The fixture exercises the importer's edge handling: the first event
// predates title generation (empty title — the newest non-empty title must
// still win), the third event has a non-message role (skipped from message
// rows), and the fourth carries a foreign event type (dropped at parse).
const chatgptConversationFixture = `{"event_id":"evt_1","type":"CONVERSATION_MESSAGE","principal":{"id":"ws_1","type":"CHATGPT_WORKSPACE"},"actor":{"type":"ACCOUNT_USER","user_id":"oai_user_1","user_email":"ada@example.com"},"timestamp":"2026-07-27T10:00:00Z","previous_message_id":"","message":{"id":"msg_1","created_at":"2026-07-27T10:00:00Z","author":{"type":"user","client_type":"desktop_web"},"content":{"type":"text","value":"What is our refund policy?"}},"conversation":{"id":"conv_1","title":"","created_at":"2026-07-27T09:59:58Z","is_pinned":false,"is_temporary_chat":false}}
{"event_id":"evt_2","type":"CONVERSATION_MESSAGE","principal":{"id":"ws_1","type":"CHATGPT_WORKSPACE"},"actor":{"type":"ACCOUNT_USER","user_id":"oai_user_1","user_email":"ada@example.com"},"timestamp":"2026-07-27T10:00:05Z","previous_message_id":"msg_1","message":{"id":"msg_2","created_at":"2026-07-27T10:00:05Z","author":{"type":"assistant","client_type":"desktop_web"},"content":{"type":"text","value":"Our refund policy allows returns within 30 days."}},"conversation":{"id":"conv_1","title":"Refund policy","created_at":"2026-07-27T09:59:58Z","is_pinned":false,"is_temporary_chat":false}}
{"event_id":"evt_3","type":"CONVERSATION_MESSAGE","principal":{"id":"ws_1","type":"CHATGPT_WORKSPACE"},"actor":{"type":"ACCOUNT_USER","user_id":"oai_user_1","user_email":"ada@example.com"},"timestamp":"2026-07-27T10:00:06Z","previous_message_id":"msg_2","message":{"id":"msg_3","created_at":"2026-07-27T10:00:06Z","author":{"type":"tool","client_type":"desktop_web"},"content":{"type":"text","value":"tool output"}},"conversation":{"id":"conv_1","title":"Refund policy","created_at":"2026-07-27T09:59:58Z","is_pinned":false,"is_temporary_chat":false}}
{"event_id":"evt_4","type":"CONVERSATION_DELETED","principal":{"id":"ws_1","type":"CHATGPT_WORKSPACE"},"actor":{"type":"ACCOUNT_USER","user_id":"oai_user_1","user_email":"ada@example.com"},"timestamp":"2026-07-27T10:00:07Z","previous_message_id":"","message":{"id":"","created_at":"","author":{"type":"","client_type":""},"content":{"type":"","value":""}},"conversation":{"id":"conv_1","title":"","created_at":"","is_pinned":false,"is_temporary_chat":false}}
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
	// The CONVERSATION_DELETED fixture line is dropped at parse.
	require.Len(t, events, 3)
	for _, event := range events {
		require.Equal(t, chatgptConversationEventType, event.Type)
	}
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

	heartbeats := 0
	svc := NewChatGPTConversationImportService(testenv.NewLogger(t), store, conn, nil, writer, func(context.Context, int) { heartbeats++ })
	file := chatgptFixtureFile(chatgptConversationFixture)
	src := &chatgptConversationSource{
		client: &stubCodexComplianceClient{
			listPages:  nil,
			listParams: nil,
			downloads:  map[string][]byte{file.ID: []byte(chatgptConversationFixture)},
		},
		svc:        svc,
		cfg:        cfg,
		pageLimit:  chatgptCompliancePageLimit,
		users:      newConnectedUserResolver(conn, orgID),
		chatIDs:    map[string]uuid.UUID{},
		chatTitles: map[string]string{},
		progress:   &ChatGPTConversationSyncProgress{},
	}

	require.NoError(t, src.ProcessPage(ctx, []codexapi.LogFile{file}))
	// ProcessPage must heartbeat per file: dense pages otherwise exceed the
	// activity's 1-minute heartbeat timeout.
	require.Positive(t, heartbeats)
	require.Equal(t, int64(2), src.progress.MessagesWritten)

	chatID, ok := src.chatIDs["conv_1"]
	require.True(t, ok, "conversation must be upserted")
	chatRow, err := chatrepo.New(conn).GetChat(ctx, chatrepo.GetChatParams{ID: chatID, ProjectID: project.ID})
	require.NoError(t, err)
	// The first event predates title generation (empty title); the newest
	// non-empty title in the file must win.
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
	src.chatTitles = map[string]string{}
	require.NoError(t, src.ProcessPage(ctx, []codexapi.LogFile{file}))
	messages, err = chatrepo.New(conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: chatID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Len(t, messages, 2)
	// The replay re-upserts with the same newest title, never a stale one.
	chatRow, err = chatrepo.New(conn).GetChat(ctx, chatrepo.GetChatParams{ID: chatID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, "Refund policy", chatRow.Title.String)
}

func chatgptConversationConfig() Config {
	workspaceID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	return Config{
		ID:                     uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		SyncID:                 uuid.MustParse("55555555-5555-5555-5555-555555555555"),
		OrganizationID:         "org_gram",
		Provider:               ProviderChatGPTCompliance,
		ProjectID:              uuid.MustParse("66666666-6666-6666-6666-666666666666"),
		ExternalOrganizationID: &workspaceID,
		BillingMode:            "",
		APIKey:                 "chatgpt-key",
		Enabled:                true,
		PollWatermarkAt:        time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC),
		PollCheckpoint:         timewindowpoller.CompletedCheckpoint(time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)),
		NextPollAfter:          time.Time{},
		LastPollError:          "",
		LastPollFailedAt:       time.Time{},
		LastPollSuccessAt:      time.Time{},
		ConsecutiveFailures:    0,
		LastCursor:             "",
		CreatedAt:              time.Time{},
		UpdatedAt:              time.Time{},
	}
}

func newChatGPTTestSource(cfg Config, client codexComplianceClient) *chatgptConversationSource {
	return &chatgptConversationSource{
		client:     client,
		svc:        &ChatGPTConversationImportService{logger: nil, store: nil, guardianPolicy: nil, db: nil, writer: nil, heartbeat: func(context.Context, int) {}},
		cfg:        cfg,
		pageLimit:  chatgptCompliancePageLimit,
		users:      nil,
		chatIDs:    map[string]uuid.UUID{},
		chatTitles: map[string]string{},
		progress:   &ChatGPTConversationSyncProgress{},
	}
}

// Pagination edge cases for this source (a deliberate copy of
// codexCostSource's state machine) are covered together with the codex source
// in logfile_source_pagination_test.go.

func TestEventCreatedAtCountsOnlyImportTimeFallbacks(t *testing.T) {
	t.Parallel()

	source := newChatGPTTestSource(chatgptConversationConfig(), nil)
	event := chatgptConversationEvent{}

	// Valid message timestamp: used directly, no fallback counted.
	event.Message.CreatedAt = "2026-07-27T10:00:00Z"
	event.Timestamp = "2026-07-27T10:00:05Z"
	require.Equal(t, time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC), source.eventCreatedAt(event))
	require.Zero(t, source.progress.TimestampFallbacks)

	// Malformed created_at rescued by a valid event timestamp: not a
	// fallback — chronology is preserved.
	event.Message.CreatedAt = "1753612800"
	require.Equal(t, time.Date(2026, 7, 27, 10, 0, 5, 0, time.UTC), source.eventCreatedAt(event))
	require.Zero(t, source.progress.TimestampFallbacks)

	// Both timestamps unusable: import time is used and the canary counts it.
	event.Timestamp = ""
	got := source.eventCreatedAt(event)
	require.WithinDuration(t, time.Now().UTC(), got, time.Minute)
	require.Equal(t, 1, source.progress.TimestampFallbacks)

	// Both absent entirely: also a counted import-time fallback.
	event.Message.CreatedAt = ""
	got = source.eventCreatedAt(event)
	require.WithinDuration(t, time.Now().UTC(), got, time.Minute)
	require.Equal(t, 2, source.progress.TimestampFallbacks)
}
