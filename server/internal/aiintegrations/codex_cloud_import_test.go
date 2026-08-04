package aiintegrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
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

// The fixture exercises the importer's edge handling: a web prompt/response
// pair (imported), a CODEX_DESKTOP_APP event (counted and skipped — device
// surface pending the unified-app verification), an unknown detail_type on
// the web client (admitted as an event but skipped from message rows), and a
// foreign event type (dropped at parse).
const codexCloudFixture = `{"event_id":"cdx_1","type":"CODEX_LOG","workspace_id":"ws_1","principal":{"id":"ws_1","type":"CHATGPT_WORKSPACE"},"actor":{"type":"ACCOUNT_USER","user_id":"oai_user_1","user_email":"grace@example.com"},"timestamp":"2026-07-28T10:00:00Z","client_id":"CODEX_WEB","event_details":{"detail_type":"PROMPT_SENT","session_id":"11111111-2222-4333-8444-555555555555","model":"gpt-5.5","prompt_text":"Fix the flaky retry test in CI"}}
{"event_id":"cdx_2","type":"CODEX_LOG","workspace_id":"ws_1","principal":{"id":"ws_1","type":"CHATGPT_WORKSPACE"},"actor":{"type":"ACCOUNT_USER","user_id":"oai_user_1","user_email":"grace@example.com"},"timestamp":"2026-07-28T10:00:20Z","client_id":"CODEX_WEB","event_details":{"detail_type":"PROMPT_RESPONSE_RECEIVED","session_id":"11111111-2222-4333-8444-555555555555","model":"gpt-5.5","response_text":"I updated the retry helper to poll instead of sleeping.","status":"completed","service_tier":"default","reasoning_effort":"medium","token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":25,"reasoning_output_tokens":10}}}
{"event_id":"cdx_3","type":"CODEX_LOG","workspace_id":"ws_1","principal":{"id":"ws_1","type":"CHATGPT_WORKSPACE"},"actor":{"type":"ACCOUNT_USER","user_id":"oai_user_2","user_email":"lin@example.com"},"timestamp":"2026-07-28T10:01:00Z","client_id":"CODEX_DESKTOP_APP","event_details":{"detail_type":"PROMPT_SENT","session_id":"99999999-8888-4777-8666-555555555555","model":"gpt-5.5","prompt_text":"desktop prompt"}}
{"event_id":"cdx_4","type":"CODEX_LOG","workspace_id":"ws_1","principal":{"id":"ws_1","type":"CHATGPT_WORKSPACE"},"actor":{"type":"ACCOUNT_USER","user_id":"oai_user_1","user_email":"grace@example.com"},"timestamp":"2026-07-28T10:02:00Z","client_id":"CODEX_WEB","event_details":{"detail_type":"SESSION_ARCHIVED","session_id":"11111111-2222-4333-8444-555555555555","model":"gpt-5.5"}}
{"event_id":"cdx_5","type":"AUDIT_LOG","workspace_id":"ws_1","principal":{"id":"ws_1","type":"CHATGPT_WORKSPACE"},"actor":{"type":"ACCOUNT_USER","user_id":"oai_user_1","user_email":"grace@example.com"},"timestamp":"2026-07-28T10:03:00Z","client_id":"CODEX_WEB","event_details":{"detail_type":"","session_id":"","model":""}}
`

func codexCloudFixtureFile(body string) codexapi.LogFile {
	sum := sha256.Sum256([]byte(body))
	return codexapi.LogFile{
		ID:         "eclf_codex_cloud_1",
		EventType:  codexCloudEventType,
		EndTime:    time.Date(2026, 7, 28, 10, 19, 31, 0, time.UTC),
		FileName:   "CODEX_LOG_2026-07-28T10:19:31.jsonl",
		FileSize:   int64(len(body)),
		FileSHA256: hex.EncodeToString(sum[:]),
	}
}

func TestParseCodexCloudEventsVerifiesSHAAndSkipsForeignTypes(t *testing.T) {
	t.Parallel()

	file := codexCloudFixtureFile(codexCloudFixture)
	events, err := parseCodexCloudEvents(file, []byte(codexCloudFixture))
	require.NoError(t, err)
	// The AUDIT_LOG line is dropped at parse; everything else survives.
	require.Len(t, events, 4)
	require.Equal(t, "cdx_1", events[0].EventID)
	require.Equal(t, codexCloudDetailPromptSent, events[0].EventDetails.DetailType)

	file.FileSHA256 = "not-the-right-hash"
	_, err = parseCodexCloudEvents(file, []byte(codexCloudFixture))
	require.Error(t, err)
	require.Contains(t, err.Error(), "sha256 mismatch")
}

func TestCodexCloudChatTitleTruncatesByRunes(t *testing.T) {
	t.Parallel()

	require.Empty(t, codexCloudChatTitle("   "))
	require.Equal(t, "Fix the CI", codexCloudChatTitle("  Fix the CI  "))

	long := strings.Repeat("界", 100)
	title := codexCloudChatTitle(long)
	require.Len(t, []rune(title), codexCloudTitleMaxRunes)
}

func TestCodexCloudProcessPageWritesChatAndMessagesIdempotently(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Codex Cloud Import Test Project",
		Slug:           "project-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	userRow, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          "user_" + uuid.NewString(),
		Email:       "grace@example.com",
		DisplayName: "Grace",
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
	svc := NewCodexCloudImportService(testenv.NewLogger(t), store, conn, nil, writer, func(context.Context, int) { heartbeats++ })
	file := codexCloudFixtureFile(codexCloudFixture)
	src := &codexCloudSource{
		client: &stubCodexComplianceClient{
			listPages:  nil,
			listParams: nil,
			downloads:  map[string][]byte{file.ID: []byte(codexCloudFixture)},
		},
		svc:       svc,
		cfg:       cfg,
		pageLimit: codexCloudPageLimit,
		users:     newConnectedUserResolver(conn, orgID),
		chatIDs:   map[string]uuid.UUID{},
		progress:  &CodexCloudSyncProgress{},
	}

	require.NoError(t, src.ProcessPage(ctx, []codexapi.LogFile{file}))
	// ProcessPage must heartbeat per file: dense pages otherwise exceed the
	// activity's 1-minute heartbeat timeout.
	require.Positive(t, heartbeats)
	require.Equal(t, int64(2), src.progress.MessagesWritten)
	// The desktop-app event is counted and skipped, never imported.
	require.Equal(t, 1, src.progress.SkippedClients)
	require.NotContains(t, src.chatIDs, "99999999-8888-4777-8666-555555555555")

	chatID, ok := src.chatIDs["11111111-2222-4333-8444-555555555555"]
	require.True(t, ok, "web session must be upserted")
	chatRow, err := chatrepo.New(conn).GetChat(ctx, chatID)
	require.NoError(t, err)
	// The feed carries no titles: the first prompt seeds the chat title.
	require.Equal(t, "Fix the flaky retry test in CI", chatRow.Title.String)
	require.Equal(t, "11111111-2222-4333-8444-555555555555", chatRow.ExternalChatID.String)
	require.Equal(t, userRow.ID, chatRow.UserID.String)

	messages, err := chatrepo.New(conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: chatID, ProjectID: project.ID})
	require.NoError(t, err)
	// The unknown detail_type event is skipped; only prompt + response land.
	require.Len(t, messages, 2)
	require.Equal(t, "user", messages[0].Role)
	require.Equal(t, "Fix the flaky retry test in CI", messages[0].Content)
	require.Equal(t, "assistant", messages[1].Role)
	require.Equal(t, "I updated the retry helper to poll instead of sleeping.", messages[1].Content)
	require.Equal(t, "gpt-5.5", messages[1].Model.String)
	require.Equal(t, "completed", messages[1].FinishReason.String)
	require.Equal(t, codexCloudSourceSlug, messages[0].Source.String)
	require.Equal(t, "cdx_1", messages[0].ExternalMessageID.String)
	require.Equal(t, codexCloudClientWeb, messages[0].UserAgent.String)
	require.Equal(t, userRow.ID, messages[0].UserID.String)
	// Per-turn token_usage is deliberately dropped: cloud tokens meter via
	// the compliance COSTS promotion, not imported transcripts.
	require.Zero(t, messages[1].PromptTokens)
	require.Zero(t, messages[1].CompletionTokens)
	require.Zero(t, messages[1].TotalTokens)

	// Replaying the same file must not duplicate messages: the insert
	// dedupes on (chat_id, external_message_id).
	src.chatIDs = map[string]uuid.UUID{}
	require.NoError(t, src.ProcessPage(ctx, []codexapi.LogFile{file}))
	messages, err = chatrepo.New(conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: chatID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Len(t, messages, 2)
	// The replay re-sends the same prompt-derived title, never a stale one.
	chatRow, err = chatrepo.New(conn).GetChat(ctx, chatID)
	require.NoError(t, err)
	require.Equal(t, "Fix the flaky retry test in CI", chatRow.Title.String)
}

func newCodexCloudTestSource(cfg Config, client codexComplianceClient) *codexCloudSource {
	return &codexCloudSource{
		client:    client,
		svc:       &CodexCloudImportService{logger: nil, store: nil, guardianPolicy: nil, db: nil, writer: nil, heartbeat: func(context.Context, int) {}},
		cfg:       cfg,
		pageLimit: codexCloudPageLimit,
		users:     nil,
		chatIDs:   map[string]uuid.UUID{},
		progress:  &CodexCloudSyncProgress{},
	}
}

// TestCodexCloudEventCreatedAtCountsFallbacks: a valid event timestamp is
// used directly; a malformed or absent one falls back to import time and
// counts the canary that flags upstream format changes.
func TestCodexCloudEventCreatedAtCountsFallbacks(t *testing.T) {
	t.Parallel()

	source := newCodexCloudTestSource(chatgptConversationConfig(), nil)
	event := codexCloudEvent{}

	event.Timestamp = "2026-07-28T10:00:00Z"
	require.Equal(t, time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC), source.eventCreatedAt(event))
	require.Zero(t, source.progress.TimestampFallbacks)

	// Malformed: import time is used and the canary counts it.
	event.Timestamp = "1753694400"
	got := source.eventCreatedAt(event)
	require.WithinDuration(t, time.Now().UTC(), got, time.Minute)
	require.Equal(t, 1, source.progress.TimestampFallbacks)

	// Absent entirely: also a counted import-time fallback.
	event.Timestamp = ""
	got = source.eventCreatedAt(event)
	require.WithinDuration(t, time.Now().UTC(), got, time.Minute)
	require.Equal(t, 2, source.progress.TimestampFallbacks)
}
