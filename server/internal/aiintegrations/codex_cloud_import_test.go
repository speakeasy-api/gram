package aiintegrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
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
// the web client (counted and skipped at admission, so it can neither create
// a chat nor land a message row), and a foreign event type (dropped at
// parse).
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
		svc:            svc,
		cfg:            cfg,
		pageLimit:      codexCloudPageLimit,
		users:          newConnectedUserResolver(conn, orgID),
		chatIDs:        map[string]uuid.UUID{},
		titledSessions: map[string]bool{},
		progress:       &CodexCloudSyncProgress{},
	}

	require.NoError(t, src.ProcessPage(ctx, []codexapi.LogFile{file}))
	// ProcessPage must heartbeat per file: dense pages otherwise exceed the
	// activity's 1-minute heartbeat timeout.
	require.Positive(t, heartbeats)
	require.Equal(t, int64(2), src.progress.MessagesWritten)
	// The desktop-app event is counted and skipped, never imported.
	require.Equal(t, 1, src.progress.SkippedClients)
	require.NotContains(t, src.chatIDs, "99999999-8888-4777-8666-555555555555")
	// The unknown detail_type (SESSION_ARCHIVED) trips its own canary.
	require.Equal(t, 1, src.progress.SkippedDetails)

	chatID, ok := src.chatIDs["11111111-2222-4333-8444-555555555555"]
	require.True(t, ok, "web session must be upserted")
	chatRow, err := chatrepo.New(conn).GetChat(ctx, chatrepo.GetChatParams{ID: chatID, ProjectID: project.ID})
	require.NoError(t, err)
	// The feed carries no titles: the first prompt seeds the chat title.
	require.Equal(t, "Fix the flaky retry test in CI", chatRow.Title.String)
	require.Equal(t, "11111111-2222-4333-8444-555555555555", chatRow.ExternalChatID.String)
	require.Equal(t, userRow.ID, chatRow.UserID.String)
	// created_at comes from the window's earliest admitted event (the session
	// start), not the newest — a trailing lifecycle event must not skew it.
	require.Equal(t, time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC), chatRow.CreatedAt.Time.UTC())

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
	src.titledSessions = map[string]bool{}
	require.NoError(t, src.ProcessPage(ctx, []codexapi.LogFile{file}))
	messages, err = chatrepo.New(conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: chatID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Len(t, messages, 2)

	// A later poll window (fresh run: empty caches) sees only the session's
	// later turns and derives a MID-SESSION prompt as its "first". First-wins
	// title semantics must keep the original title — newest-wins would
	// retitle the chat on every window.
	laterWindow := `{"event_id":"cdx_6","type":"CODEX_LOG","actor":{"type":"ACCOUNT_USER","user_id":"oai_user_1","user_email":"grace@example.com"},"timestamp":"2026-07-28T10:10:00Z","client_id":"CODEX_WEB","event_details":{"detail_type":"PROMPT_SENT","session_id":"11111111-2222-4333-8444-555555555555","model":"gpt-5.5","prompt_text":"Also add a changelog entry"}}` + "\n"
	laterFile := codexCloudFixtureFile(laterWindow)
	laterFile.ID = "eclf_codex_cloud_2"
	src.chatIDs = map[string]uuid.UUID{}
	src.titledSessions = map[string]bool{}
	src.client = &stubCodexComplianceClient{
		listPages:  nil,
		listParams: nil,
		downloads:  map[string][]byte{laterFile.ID: []byte(laterWindow)},
	}
	require.NoError(t, src.ProcessPage(ctx, []codexapi.LogFile{laterFile}))
	messages, err = chatrepo.New(conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: chatID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Len(t, messages, 3)
	chatRow, err = chatrepo.New(conn).GetChat(ctx, chatrepo.GetChatParams{ID: chatID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, "Fix the flaky retry test in CI", chatRow.Title.String,
		"a mid-session prompt from a later window must not retitle the chat")
}

func newCodexCloudTestSource(cfg Config, client codexComplianceClient) *codexCloudSource {
	return &codexCloudSource{
		client:         client,
		svc:            &CodexCloudImportService{logger: nil, store: nil, guardianPolicy: nil, db: nil, writer: nil, heartbeat: func(context.Context, int) {}},
		cfg:            cfg,
		pageLimit:      codexCloudPageLimit,
		users:          nil,
		chatIDs:        map[string]uuid.UUID{},
		titledSessions: map[string]bool{},
		progress:       &CodexCloudSyncProgress{},
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

// TestCodexCloudTitleBackfillsWhenPromptArrivesInLaterFile: a session first
// seen through a response-only file (its opening prompt straddled a file
// boundary) is created untitled; when the prompt lands in a later file of the
// same run, the title-refresh re-upsert writes it instead of the known-cache
// skipping the session forever.
func TestCodexCloudTitleBackfillsWhenPromptArrivesInLaterFile(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Codex Cloud Title Backfill Project",
		Slug:           "project-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	workspaceID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderChatGPTCompliance, "chatgpt-key", true, true, &workspaceID, nil)
	cfg := created.Config
	cfg.ProjectID = project.ID

	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, nil)
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	responseOnly := `{"event_id":"cdx_r1","type":"CODEX_LOG","actor":{"type":"ACCOUNT_USER","user_id":"oai_user_1","user_email":"grace@example.com"},"timestamp":"2026-07-28T11:00:00Z","client_id":"CODEX_WEB","event_details":{"detail_type":"PROMPT_RESPONSE_RECEIVED","session_id":"22222222-3333-4444-8555-666666666666","model":"gpt-5.5","response_text":"Done, the migration is applied.","status":"completed"}}` + "\n"
	promptLater := `{"event_id":"cdx_p1","type":"CODEX_LOG","actor":{"type":"ACCOUNT_USER","user_id":"oai_user_1","user_email":"grace@example.com"},"timestamp":"2026-07-28T11:05:00Z","client_id":"CODEX_WEB","event_details":{"detail_type":"PROMPT_SENT","session_id":"22222222-3333-4444-8555-666666666666","model":"gpt-5.5","prompt_text":"Now update the rollback plan"}}` + "\n"

	fileA := codexCloudFixtureFile(responseOnly)
	fileA.ID = "eclf_backfill_a"
	fileB := codexCloudFixtureFile(promptLater)
	fileB.ID = "eclf_backfill_b"
	fileB.EndTime = fileA.EndTime.Add(time.Minute)

	svc := NewCodexCloudImportService(testenv.NewLogger(t), store, conn, nil, writer, func(context.Context, int) {})
	src := &codexCloudSource{
		client: &stubCodexComplianceClient{
			listPages:  nil,
			listParams: nil,
			downloads: map[string][]byte{
				fileA.ID: []byte(responseOnly),
				fileB.ID: []byte(promptLater),
			},
		},
		svc:            svc,
		cfg:            cfg,
		pageLimit:      codexCloudPageLimit,
		users:          newConnectedUserResolver(conn, orgID),
		chatIDs:        map[string]uuid.UUID{},
		titledSessions: map[string]bool{},
		progress:       &CodexCloudSyncProgress{},
	}

	require.NoError(t, src.ProcessPage(ctx, []codexapi.LogFile{fileA, fileB}))

	chatID, ok := src.chatIDs["22222222-3333-4444-8555-666666666666"]
	require.True(t, ok)
	chatRow, err := chatrepo.New(conn).GetChat(ctx, chatrepo.GetChatParams{ID: chatID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, "Now update the rollback plan", chatRow.Title.String)
	// The link row is created once; the title refresh only re-upserts.
	require.Equal(t, 1, src.progress.ChatsUpserted)
	// The session is now marked titled, so a third file's distinct prompt is
	// skipped rather than re-upserted: the query is first-wins, so the write
	// could not change the stored title anyway.
	require.True(t, src.titledSessions["22222222-3333-4444-8555-666666666666"])

	promptAgain := `{"event_id":"cdx_p2","type":"CODEX_LOG","actor":{"type":"ACCOUNT_USER","user_id":"oai_user_1","user_email":"grace@example.com"},"timestamp":"2026-07-28T11:10:00Z","client_id":"CODEX_WEB","event_details":{"detail_type":"PROMPT_SENT","session_id":"22222222-3333-4444-8555-666666666666","model":"gpt-5.5","prompt_text":"And note the owner"}}` + "\n"
	fileC := codexCloudFixtureFile(promptAgain)
	fileC.ID = "eclf_backfill_c"
	fileC.EndTime = fileB.EndTime.Add(time.Minute)
	src.client = &stubCodexComplianceClient{
		listPages:  nil,
		listParams: nil,
		downloads:  map[string][]byte{fileC.ID: []byte(promptAgain)},
	}
	require.NoError(t, src.ProcessPage(ctx, []codexapi.LogFile{fileC}))
	chatRow, err = chatrepo.New(conn).GetChat(ctx, chatrepo.GetChatParams{ID: chatID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, "Now update the rollback plan", chatRow.Title.String,
		"a later prompt in the same run must not retitle the chat")
	require.Equal(t, 1, src.progress.ChatsUpserted)
}

// TestCodexCloudMalformedTimestampCountsOncePerEvent: a session's opening
// event feeds both the chat row's created_at and its own message row, so its
// timestamp must be resolved once — resolving it per reader would report two
// TimestampFallbacks canaries for one bad upstream value.
func TestCodexCloudMalformedTimestampCountsOncePerEvent(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Codex Cloud Timestamp Canary Project",
		Slug:           "project-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	workspaceID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderChatGPTCompliance, "chatgpt-key", true, true, &workspaceID, nil)
	cfg := created.Config
	cfg.ProjectID = project.ID

	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, nil)
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	// A single admitted event whose timestamp is a unix epoch rather than
	// RFC3339 — the shape an upstream format change would produce.
	malformed := `{"event_id":"cdx_bad_ts","type":"CODEX_LOG","actor":{"type":"ACCOUNT_USER","user_id":"oai_user_1","user_email":"grace@example.com"},"timestamp":"1753694400","client_id":"CODEX_WEB","event_details":{"detail_type":"PROMPT_SENT","session_id":"33333333-4444-4555-8666-777777777777","model":"gpt-5.5","prompt_text":"Rerun the migration"}}` + "\n"
	file := codexCloudFixtureFile(malformed)
	file.ID = "eclf_bad_ts"

	svc := NewCodexCloudImportService(testenv.NewLogger(t), store, conn, nil, writer, func(context.Context, int) {})
	src := &codexCloudSource{
		client: &stubCodexComplianceClient{
			listPages:  nil,
			listParams: nil,
			downloads:  map[string][]byte{file.ID: []byte(malformed)},
		},
		svc:            svc,
		cfg:            cfg,
		pageLimit:      codexCloudPageLimit,
		users:          newConnectedUserResolver(conn, orgID),
		chatIDs:        map[string]uuid.UUID{},
		titledSessions: map[string]bool{},
		progress:       &CodexCloudSyncProgress{},
	}

	require.NoError(t, src.ProcessPage(ctx, []codexapi.LogFile{file}))
	require.Equal(t, int64(1), src.progress.MessagesWritten)
	require.Equal(t, 1, src.progress.TimestampFallbacks)

	chatID, ok := src.chatIDs["33333333-4444-4555-8666-777777777777"]
	require.True(t, ok)
	chatRow, err := chatrepo.New(conn).GetChat(ctx, chatrepo.GetChatParams{ID: chatID, ProjectID: project.ID})
	require.NoError(t, err)
	// Chat and message share the one resolved import-time fallback.
	require.WithinDuration(t, time.Now().UTC(), chatRow.CreatedAt.Time.UTC(), time.Minute)
}

// TestSyncCodexCloudSessionsRejectsMisconfiguredIntegrations: the cloud
// transcript feed is workspace-scoped and lives on chatgpt_compliance, while
// Codex cost data lives on codex_compliance. Pointing this importer at the
// wrong provider must fail loudly — running it anyway would import against the
// wrong credential and surface as confusing data rather than an error. The
// workspace id is likewise required: without it the client cannot scope its
// requests at all.
func TestSyncCodexCloudSessionsRejectsMisconfiguredIntegrations(t *testing.T) {
	t.Parallel()

	ctx, conn, store, _ := newStoreTestDB(t)
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, nil)
	t.Cleanup(func() { _ = shutdown(context.Background()) })
	svc := NewCodexCloudImportService(testenv.NewLogger(t), store, conn, nil, writer, func(context.Context, int) {})

	workspaceID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	now := time.Now().UTC()

	err := svc.SyncCodexCloudSessions(ctx, Config{
		Provider:               ProviderCodexCompliance,
		ExternalOrganizationID: &workspaceID,
	}, now)
	require.Error(t, err, "the codex cost provider must not drive the cloud transcript import")
	require.Contains(t, err.Error(), ProviderCodexCompliance)

	err = svc.SyncCodexCloudSessions(ctx, Config{
		Provider:               ProviderChatGPTCompliance,
		ExternalOrganizationID: nil,
	}, now)
	require.Error(t, err, "the workspace id scopes every request and cannot be absent")
	require.Contains(t, err.Error(), "external_organization_id")
}

// TestCodexCloudRetryAfterOnlyBacksOffOnRateLimits: a 429 must park the sync
// for the poller to retry, while any other failure surfaces. The match is an
// errors.As against the client's typed error, so wrapping changes upstream can
// silently turn a retryable rate limit into a hard failure — or worse, make
// every error look retryable and spin.
func TestCodexCloudRetryAfterOnlyBacksOffOnRateLimits(t *testing.T) {
	t.Parallel()

	src := &codexCloudSource{}

	_, retry := src.RetryAfter(&codexapi.HTTPError{StatusCode: http.StatusTooManyRequests})
	require.True(t, retry, "a 429 must be retried")

	// Still a rate limit after the poller wraps it on the way up.
	_, retry = src.RetryAfter(fmt.Errorf("fetch page: %w", &codexapi.HTTPError{StatusCode: http.StatusTooManyRequests}))
	require.True(t, retry, "wrapping must not hide the rate limit")

	for _, err := range []error{
		&codexapi.HTTPError{StatusCode: http.StatusInternalServerError},
		&codexapi.HTTPError{StatusCode: http.StatusUnauthorized},
		errors.New("connection reset"),
		nil,
	} {
		_, retry = src.RetryAfter(err)
		require.False(t, retry, "only a 429 is retryable: %v", err)
	}
}

// TestCodexCloudCountsEventsMissingIdentifiers: a web prompt/response event
// with no session id (or no event id) clears the client and detail-type
// filters and is then dropped. Uncounted, a feed that stopped emitting
// session_id would import nothing while every other canary read zero and the
// run reported success — indistinguishable from a window with no cloud
// activity. event_id matters doubly: it is the message dedupe key.
func TestCodexCloudCountsEventsMissingIdentifiers(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Codex Cloud Missing IDs Project",
		Slug:           "project-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	workspaceID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderChatGPTCompliance, "chatgpt-key", true, true, &workspaceID, nil)
	cfg := created.Config
	cfg.ProjectID = project.ID

	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, nil)
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	// Both clear the client and detail-type gates; one lacks session_id, the
	// other lacks event_id.
	noSession := `{"event_id":"cdx_ns","type":"CODEX_LOG","actor":{"type":"ACCOUNT_USER","user_id":"oai_user_1","user_email":"grace@example.com"},"timestamp":"2026-07-28T12:00:00Z","client_id":"CODEX_WEB","event_details":{"detail_type":"PROMPT_SENT","session_id":"","model":"gpt-5.5","prompt_text":"orphan prompt"}}` + "\n"
	noEventID := `{"event_id":"","type":"CODEX_LOG","actor":{"type":"ACCOUNT_USER","user_id":"oai_user_1","user_email":"grace@example.com"},"timestamp":"2026-07-28T12:01:00Z","client_id":"CODEX_WEB","event_details":{"detail_type":"PROMPT_SENT","session_id":"44444444-5555-4666-8777-888888888888","model":"gpt-5.5","prompt_text":"unidentified prompt"}}` + "\n"
	body := noSession + noEventID

	file := codexCloudFixtureFile(body)
	file.ID = "eclf_missing_ids"

	svc := NewCodexCloudImportService(testenv.NewLogger(t), store, conn, nil, writer, func(context.Context, int) {})
	src := &codexCloudSource{
		client:         &stubCodexComplianceClient{downloads: map[string][]byte{file.ID: []byte(body)}},
		svc:            svc,
		cfg:            cfg,
		pageLimit:      codexCloudPageLimit,
		users:          newConnectedUserResolver(conn, orgID),
		chatIDs:        map[string]uuid.UUID{},
		titledSessions: map[string]bool{},
		progress:       &CodexCloudSyncProgress{},
	}

	require.NoError(t, src.ProcessPage(ctx, []codexapi.LogFile{file}))

	require.Equal(t, 2, src.progress.SkippedMissingIDs,
		"an event dropped for a missing identifier must be counted, not silently discarded")
	require.Zero(t, src.progress.SkippedClients)
	require.Zero(t, src.progress.SkippedDetails)
	require.Zero(t, src.progress.ChatsUpserted, "an unidentifiable event must not create a chat")
	require.Zero(t, src.progress.MessagesWritten)

	// The canary must be visible in the failure report operators read.
	require.Contains(t, src.progress.String(), "skipped_missing_ids=2")
}
