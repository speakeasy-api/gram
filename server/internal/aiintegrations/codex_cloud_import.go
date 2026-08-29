package aiintegrations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/timewindowpoller"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	codexapi "github.com/speakeasy-api/gram/server/internal/thirdparty/codex"
)

const (
	// codexCloudEventType is the Compliance Logs Platform file family carrying
	// Codex cloud task transcripts. The feed is workspace-scoped like
	// CONVERSATION_MESSAGE: one NDJSON event per prompt or response, files
	// finalized on the API's end_time watermark.
	codexCloudEventType = "CODEX_LOG"
	codexCloudPageLimit = 100
	// codexCloudSourceSlug is the canonical chat source for imported Codex
	// cloud sessions (see chat/sources.go) — distinct from "codex", which tags
	// live device sessions captured by hooks/OTEL.
	codexCloudSourceSlug = "codex-web"
	// codexCloudClientWeb is the client_id marking cloud web-task events —
	// the only client this import admits. The feed also carries DEVICE
	// clients (CODEX_CLI became its dominant client in early August 2026,
	// plus CODEX_DESKTOP_APP and CODEX_SDK_TS): those sessions are hook/OTEL
	// captured live, and importing them here would create a duplicate chat
	// for every device session (same session-id UUID space). This allowlist
	// is the sole guard against that duplication, so SkippedClients running
	// hot is expected, not a fault.
	codexCloudClientWeb = "CODEX_WEB"
	// Codex cloud event shapes: a prompt submission and the model's reply.
	codexCloudDetailPromptSent       = "PROMPT_SENT"
	codexCloudDetailResponseReceived = "PROMPT_RESPONSE_RECEIVED"
	// codexCloudTitleMaxRunes bounds prompt-derived chat titles, matching the
	// live-capture convention (canonicalChatTitle).
	codexCloudTitleMaxRunes = 80
)

// codexCloudEvent is one CODEX_LOG NDJSON event.
type codexCloudEvent struct {
	EventID   string `json:"event_id"`
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	ClientID  string `json:"client_id"`
	Actor     struct {
		Type      string `json:"type"`
		UserID    string `json:"user_id"`
		UserEmail string `json:"user_email"`
	} `json:"actor"`
	EventDetails struct {
		DetailType   string `json:"detail_type"`
		SessionID    string `json:"session_id"`
		Model        string `json:"model"`
		PromptText   string `json:"prompt_text"`
		ResponseText string `json:"response_text"`
		Status       string `json:"status"`
	} `json:"event_details"`
}

// CodexCloudSyncProgress reports how far one sync run got.
type CodexCloudSyncProgress struct {
	WindowStart     time.Time `json:"window_start"`
	LogPages        int       `json:"log_pages"`
	LogFiles        int       `json:"log_files"`
	Events          int       `json:"events"`
	MessagesWritten int64     `json:"messages_written"`
	ChatsUpserted   int       `json:"chats_upserted"`
	// SkippedClients counts events dropped because their client_id is not
	// CODEX_WEB (see codexCloudClientWeb) — a canary for new cloud surfaces
	// appearing in the feed.
	SkippedClients int `json:"skipped_clients"`
	// SkippedDetails counts web events dropped because their detail_type is
	// not a known prompt/response shape — a canary for new event kinds
	// (tool calls, lifecycle events) that would otherwise import as silently
	// incomplete transcripts.
	SkippedDetails int `json:"skipped_details"`
	// SkippedMissingIDs counts prompt/response events dropped because they
	// carry no session id or event id — a canary for the feed dropping the
	// fields the import is keyed on. Without it a feed that stopped emitting
	// session_id would import nothing while every other counter read zero and
	// the run reported success, which is indistinguishable from a quiet
	// window. event_id is also the message dedupe key, so losing it silently
	// would take replay safety with it.
	SkippedMissingIDs int `json:"skipped_missing_ids"`
	// TimestampFallbacks counts events whose timestamps failed RFC3339
	// parsing and fell back to import time — a canary for upstream format
	// changes that would otherwise silently rewrite history chronology.
	TimestampFallbacks int       `json:"timestamp_fallbacks"`
	WatermarkReached   time.Time `json:"watermark_reached"`
}

type CodexCloudImportService struct {
	logger         *slog.Logger
	store          *Store
	guardianPolicy *guardian.Policy
	db             *pgxpool.Pool
	writer         *chat.ChatMessageWriter
	heartbeat      func(ctx context.Context, page int)
}

func NewCodexCloudImportService(logger *slog.Logger, store *Store, db *pgxpool.Pool, guardianPolicy *guardian.Policy, writer *chat.ChatMessageWriter, heartbeat func(ctx context.Context, page int)) *CodexCloudImportService {
	if heartbeat == nil {
		panic("codex cloud import service requires heartbeat")
	}
	return &CodexCloudImportService{
		logger:         logger.With(attr.SlogComponent("aiintegrations.codex_cloud")),
		store:          store,
		guardianPolicy: guardianPolicy,
		db:             db,
		writer:         writer,
		heartbeat:      heartbeat,
	}
}

// SyncCodexCloudSessions imports Codex cloud task transcripts (CODEX_LOG
// compliance files) through the shared time-window poller and persists them
// as external chats + messages, mirroring the ChatGPT conversation import.
// Replays are idempotent: chats upsert on the session id and messages land
// under ON CONFLICT (chat_id, external_message_id) DO NOTHING. Token counts
// in the feed are deliberately NOT persisted anywhere metering reads: cloud
// tokens are metered from the compliance COSTS promotion, and carrying them
// here too would double count.
func (s *CodexCloudImportService) SyncCodexCloudSessions(ctx context.Context, cfg Config, endTime time.Time) error {
	if cfg.Provider != ProviderChatGPTCompliance {
		return fmt.Errorf("unsupported ai integration provider for codex cloud import: %s", cfg.Provider)
	}
	if cfg.ExternalOrganizationID == nil {
		return fmt.Errorf("external_organization_id (workspace id) is required for codex cloud import")
	}

	progress := &CodexCloudSyncProgress{
		WindowStart:        cfg.PollWatermarkAt,
		LogPages:           0,
		LogFiles:           0,
		Events:             0,
		MessagesWritten:    0,
		ChatsUpserted:      0,
		SkippedClients:     0,
		SkippedDetails:     0,
		SkippedMissingIDs:  0,
		TimestampFallbacks: 0,
		WatermarkReached:   cfg.PollWatermarkAt,
	}

	source := &codexCloudSource{
		client:         codexapi.NewWorkspaceClient(s.guardianPolicy, *cfg.ExternalOrganizationID, codexapi.WithAPIKey(cfg.APIKey)),
		svc:            s,
		cfg:            cfg,
		pageLimit:      codexCloudPageLimit,
		users:          newConnectedUserResolver(s.db, cfg.OrganizationID),
		chatIDs:        map[string]uuid.UUID{},
		titledSessions: map[string]bool{},
		progressMu:     sync.Mutex{},
		progress:       progress,
	}

	runner := &timewindowpoller.Poller[[]codexapi.LogFile]{
		Store:    s.store,
		Schedule: ScheduleCodexCloudSessions,
		State: timewindowpoller.SyncState{
			SyncID:      cfg.SyncID,
			WatermarkAt: cfg.PollWatermarkAt,
			Checkpoint:  cfg.PollCheckpoint,
		},
		Source:  source,
		EndTime: endTime,
		Heartbeat: func(ctx context.Context, page int) {
			s.heartbeat(ctx, page)
		},
		InitialLookback: chatgptComplianceInitialLookback,
		MaxWindow:       0,
		Granularity:     0,
		ResumeOffset:    0,
	}
	if err := runner.Do(ctx); err != nil {
		return newSyncError("sync codex cloud sessions", *progress,
			SyncStageError{Stage: "import_codex_cloud_logs", Err: err},
		)
	}
	return nil
}

// codexCloudSource pages CODEX_LOG compliance files. Its listing state
// machine is a deliberate copy of chatgptConversationSource's — the shared
// pagination suite (logfile_source_pagination_test.go) runs the same protocol
// edge cases against every copy so a fix cannot silently miss one.
type codexCloudSource struct {
	client    codexComplianceClient
	svc       *CodexCloudImportService
	cfg       Config
	pageLimit int
	users     *connectedUserResolver
	// chatIDs caches session id -> chat row id for the run so link rows and
	// id lookups happen once per session, not once per event. titledSessions
	// marks the sessions that have already been offered a title this run, so
	// a prompt arriving in a later file than the session's first upsert still
	// lands its title (the feed itself carries none) while later prompts for
	// the same session skip a re-upsert the first-wins query would ignore.
	chatIDs        map[string]uuid.UUID
	titledSessions map[string]bool
	// progressMu guards progress: the poller pipelines FetchPage (producer
	// goroutine) with ProcessPage (consumer goroutine), and both record
	// progress.
	progressMu sync.Mutex
	progress   *CodexCloudSyncProgress
}

func (src *codexCloudSource) UpperBound(ctx context.Context, endTime time.Time) (time.Time, error) {
	// Same resume-precedence and lookback as the sibling workspace feed; one
	// helper keeps the checkpoint/watermark semantics from drifting.
	after := chatgptUpperBoundStart(src.cfg, endTime)
	watermark := time.Time{}
	for pageNum := 0; ; pageNum++ {
		// The poller only heartbeats before UpperBound; a 30-day first sync
		// can page for longer than the activity's 1-minute heartbeat timeout,
		// so heartbeat per listing page.
		src.svc.heartbeat(ctx, pageNum)
		page, err := src.client.ListLogs(ctx, codexapi.ListLogsParams{
			EventType: codexCloudEventType,
			After:     after,
			Limit:     src.pageLimit,
		})
		if err != nil {
			return time.Time{}, err //nolint:wrapcheck // Preserve HTTPError classification upstream.
		}
		if page.LastEndTime.After(watermark) {
			watermark = page.LastEndTime
		}
		if !page.HasMore {
			if watermark.IsZero() {
				return after, nil
			}
			return watermark, nil
		}
		if page.LastEndTime.IsZero() {
			return time.Time{}, fmt.Errorf("codex cloud logs page had has_more without last_end_time")
		}
		if err := validateCodexLastEndTimeAdvanced(after, page.LastEndTime); err != nil {
			return time.Time{}, err
		}
		after = page.LastEndTime
	}
}

func (src *codexCloudSource) FetchPage(ctx context.Context, start, end time.Time, pageToken string) (timewindowpoller.Page[[]codexapi.LogFile], error) {
	after := start
	if pageToken != "" {
		parsed, err := time.Parse(time.RFC3339Nano, pageToken)
		if err != nil {
			return timewindowpoller.Page[[]codexapi.LogFile]{Payload: nil, NextPage: "", HasMore: false}, fmt.Errorf("parse codex cloud page token: %w", err)
		}
		after = parsed
	}

	page, err := src.client.ListLogs(ctx, codexapi.ListLogsParams{
		EventType: codexCloudEventType,
		After:     after,
		Limit:     src.pageLimit,
	})
	if err != nil {
		return timewindowpoller.Page[[]codexapi.LogFile]{Payload: nil, NextPage: "", HasMore: false}, err //nolint:wrapcheck // Preserve HTTPError classification upstream.
	}
	src.progressMu.Lock()
	src.progress.LogPages++
	if page.LastEndTime.After(src.progress.WatermarkReached) {
		src.progress.WatermarkReached = page.LastEndTime
	}
	src.progressMu.Unlock()

	files := make([]codexapi.LogFile, 0, len(page.Data))
	for _, file := range page.Data {
		if file.EventType != "" && file.EventType != codexCloudEventType {
			continue
		}
		if file.EndTime.After(end) {
			return timewindowpoller.Page[[]codexapi.LogFile]{Payload: files, NextPage: "", HasMore: false}, nil
		}
		files = append(files, file)
	}

	nextPage := ""
	if page.HasMore {
		if page.LastEndTime.IsZero() {
			return timewindowpoller.Page[[]codexapi.LogFile]{Payload: nil, NextPage: "", HasMore: false}, fmt.Errorf("codex cloud logs page had has_more without last_end_time")
		}
		if err := validateCodexLastEndTimeAdvanced(after, page.LastEndTime); err != nil {
			return timewindowpoller.Page[[]codexapi.LogFile]{Payload: nil, NextPage: "", HasMore: false}, err
		}
		nextPage = page.LastEndTime.UTC().Format(time.RFC3339Nano)
	}
	return timewindowpoller.Page[[]codexapi.LogFile]{
		Payload:  files,
		NextPage: nextPage,
		HasMore:  page.HasMore,
	}, nil
}

func (src *codexCloudSource) ProcessPage(ctx context.Context, files []codexapi.LogFile) error {
	for i, file := range files {
		// The poller only heartbeats around FetchPage; a page holds up to
		// pageLimit dense per-event files, so heartbeat per file to stay
		// inside the activity's 1-minute heartbeat timeout.
		src.svc.heartbeat(ctx, i)

		body, err := src.client.DownloadLog(ctx, file.ID)
		if err != nil {
			return err //nolint:wrapcheck // Preserve HTTPError classification upstream.
		}

		events, err := parseCodexCloudEvents(file, body)
		if err != nil {
			return err
		}
		if err := src.writeFile(ctx, file, events); err != nil {
			return err
		}

		src.progressMu.Lock()
		src.progress.LogFiles++
		src.progress.Events += len(events)
		if file.EndTime.After(src.progress.WatermarkReached) {
			src.progress.WatermarkReached = file.EndTime
		}
		src.progressMu.Unlock()
	}
	return nil
}

func (src *codexCloudSource) RetryAfter(err error) (time.Duration, bool) {
	var httpErr *codexapi.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	return 0, true
}

// codexCloudSessionState carries per-session metadata accumulated across one
// file's events: the earliest admitted event's resolved timestamp (which
// approximates the session start within the window), the newest event
// (freshest actor identity), and the first prompt text seen, which seeds the
// chat title.
type codexCloudSessionState struct {
	firstCreatedAt time.Time
	latest         codexCloudEvent
	firstPrompt    string
}

// writeFile persists one log file's web-task events: each session is upserted
// as a chat, then every prompt/response lands in one batched write. The feed
// carries no conversation titles, so a session's title derives from the first
// prompt seen for it (truncated by runes, matching live capture); a prompt
// arriving in a later file still lands its title via a title-refresh
// re-upsert, and the upsert's COALESCE preserves stored titles on replays.
// Only prompt/response events on the web client are admitted: non-web clients
// (CODEX_DESKTOP_APP) and unknown detail types are counted and skipped, so a
// window carrying only lifecycle events can never create an empty, untitled
// chat.
func (src *codexCloudSource) writeFile(ctx context.Context, file codexapi.LogFile, events []codexCloudEvent) error {
	sessions := map[string]*codexCloudSessionState{}
	order := make([]string, 0, len(events))
	skippedClients, skippedDetails := 0, 0
	admitted := make([]codexCloudEvent, 0, len(events))
	// Each admitted event's timestamp is resolved exactly once, here, and then
	// reused by both the chat row and the event's message row: resolving it
	// per reader would count a single malformed value twice in the
	// TimestampFallbacks canary.
	admittedAt := make([]time.Time, 0, len(events))
	fallbacksBefore := src.timestampFallbacks()
	skippedMissingIDs := 0
	for _, event := range events {
		if !strings.EqualFold(strings.TrimSpace(event.ClientID), codexCloudClientWeb) {
			skippedClients++
			continue
		}
		if event.EventDetails.DetailType != codexCloudDetailPromptSent &&
			event.EventDetails.DetailType != codexCloudDetailResponseReceived {
			skippedDetails++
			continue
		}
		if event.EventDetails.SessionID == "" || event.EventID == "" {
			skippedMissingIDs++
			continue
		}
		createdAt := src.eventCreatedAt(event)
		admitted = append(admitted, event)
		admittedAt = append(admittedAt, createdAt)
		state, ok := sessions[event.EventDetails.SessionID]
		if !ok {
			state = &codexCloudSessionState{firstCreatedAt: createdAt, latest: event, firstPrompt: ""}
			sessions[event.EventDetails.SessionID] = state
			order = append(order, event.EventDetails.SessionID)
		}
		// Events within a file are chronological, so the last one seen is the
		// newest.
		state.latest = event
		if state.firstPrompt == "" && event.EventDetails.DetailType == codexCloudDetailPromptSent {
			state.firstPrompt = strings.TrimSpace(event.EventDetails.PromptText)
		}
	}
	if skippedClients > 0 || skippedDetails > 0 || skippedMissingIDs > 0 {
		src.progressMu.Lock()
		src.progress.SkippedClients += skippedClients
		src.progress.SkippedDetails += skippedDetails
		src.progress.SkippedMissingIDs += skippedMissingIDs
		src.progressMu.Unlock()
	}
	if fallbacks := src.timestampFallbacks() - fallbacksBefore; fallbacks > 0 {
		src.svc.logger.WarnContext(ctx, "codex cloud event timestamps failed to parse; messages stamped with import time",
			attr.SlogFilePath(file.FileName),
			attr.SlogCodexCloudTimestampFallbacks(fallbacks),
		)
	}
	for _, sessionID := range order {
		if err := src.upsertSessionChat(ctx, sessionID, sessions[sessionID]); err != nil {
			return err
		}
	}

	rows := make([]chat.ExternalMessageWrite, 0, len(admitted))
	for i, event := range admitted {
		var role, content string
		switch event.EventDetails.DetailType {
		case codexCloudDetailPromptSent:
			role, content = "user", event.EventDetails.PromptText
		case codexCloudDetailResponseReceived:
			role, content = "assistant", event.EventDetails.ResponseText
		default:
			// Unreachable: admission above only passes the two shapes.
			continue
		}

		userID, err := src.users.resolve(ctx, event.Actor.UserEmail)
		if err != nil {
			return err
		}

		rows = append(rows, chat.ExternalMessageWrite{
			Params: chatrepo.CreateExternalChatMessageParams{
				ID:                uuid.Nil,
				ChatID:            src.chatIDs[event.EventDetails.SessionID],
				Role:              role,
				ProjectID:         src.cfg.ProjectID,
				Content:           content,
				ContentRaw:        nil,
				ContentAssetUrl:   pgtype.Text{String: "", Valid: false},
				StorageError:      pgtype.Text{String: "", Valid: false},
				Model:             conv.ToPGTextEmpty(event.EventDetails.Model),
				MessageID:         pgtype.Text{String: "", Valid: false},
				ToolCallID:        pgtype.Text{String: "", Valid: false},
				UserID:            conv.ToPGText(userID),
				ExternalUserID:    conv.ToPGText(event.Actor.UserID),
				ExternalMessageID: conv.ToPGText(event.EventID),
				FinishReason:      conv.ToPGTextEmpty(event.EventDetails.Status),
				ToolCalls:         nil,
				// Per-turn token_usage from the feed is deliberately dropped:
				// cloud tokens meter through the compliance COSTS promotion, and
				// recording them here as well would double count.
				PromptTokens:     0,
				CompletionTokens: 0,
				TotalTokens:      0,
				Origin:           pgtype.Text{String: "", Valid: false},
				// The client_id (CODEX_WEB) is the closest surface signal the
				// feed carries; it rides this column for per-client analysis.
				UserAgent:   conv.ToPGTextEmpty(event.ClientID),
				IpAddress:   pgtype.Text{String: "", Valid: false},
				Source:      conv.ToPGText(codexCloudSourceSlug),
				ContentHash: nil,
				Generation:  0,
				CreatedAt:   conv.ToPGTimestamptz(admittedAt[i]),
			},
			UserEmail: event.Actor.UserEmail,
			Provider:  codexProviderOpenAI,
		})
	}
	if len(rows) == 0 {
		return nil
	}

	// WriteExternal inserts row by row, so a mid-batch failure still leaves
	// the earlier rows durable — record the partial count before propagating
	// the error so failure details describe the replay-safe work accurately.
	written, err := src.svc.writer.WriteExternal(ctx, src.cfg.ProjectID, rows)
	src.progressMu.Lock()
	src.progress.MessagesWritten += written
	src.progressMu.Unlock()
	if err != nil {
		return fmt.Errorf("write codex cloud messages: %w", err)
	}
	return nil
}

// upsertSessionChat writes the session's chat row with a prompt-derived title
// (the feed itself carries none). A session already upserted this run is
// re-upserted only once a title first becomes available — a PROMPT_SENT can
// land in a later file than the session's first-seen (response-only) window —
// and the query's COALESCE preserves stored titles on replays.
// The chat's created_at comes from the window's earliest admitted event, the
// closest available approximation of the session start.
//
// The title is deliberately a raw truncated prompt, not the DefaultChatTitle
// sentinel the async LLM titler replaces: external imports never enter that
// pipeline today, and a sentinel here would leave every chat titled "New
// Chat" indefinitely. If imports ever join the titler, this divergence must
// be revisited (a raw-prompt title reads as deliberately set and is skipped).
func (src *codexCloudSource) upsertSessionChat(ctx context.Context, sessionID string, state *codexCloudSessionState) error {
	title := codexCloudChatTitle(state.firstPrompt)
	_, known := src.chatIDs[sessionID]
	// A title already offered this run is terminal: the query is first-wins,
	// so no later prompt can change what is stored and re-upserting for one
	// would only burn a write.
	if known && (title == "" || src.titledSessions[sessionID]) {
		return nil
	}

	createdAt := state.firstCreatedAt
	userID, err := src.users.resolve(ctx, state.latest.Actor.UserEmail)
	if err != nil {
		return err
	}

	chatID, err := chatrepo.New(src.svc.db).UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
		ID:             uuid.New(),
		ProjectID:      src.cfg.ProjectID,
		OrganizationID: src.cfg.OrganizationID,
		// NULL when unresolved so the upsert's COALESCE preserves a user
		// resolved by an earlier event.
		UserID:         conv.ToPGTextEmpty(userID),
		ExternalUserID: conv.ToPGTextEmpty(state.latest.Actor.UserID),
		ExternalChatID: conv.ToPGText(sessionID),
		Title:          conv.ToPGTextEmpty(title),
		CreatedAt:      conv.ToPGTimestamptz(createdAt),
		UpdatedAt:      conv.ToPGTimestamptz(createdAt),
		// First-wins: the title is DERIVED from the window's first prompt, and
		// a later poll window would derive a mid-session prompt as its
		// "first" — newest-wins would retitle the chat on every window.
		PreferStoredTitle: true,
	})
	if err != nil {
		return fmt.Errorf("upsert codex cloud chat: %w", err)
	}
	if !known {
		if _, err := chatrepo.New(src.svc.db).LinkAIIntegrationConfigChat(ctx, chatrepo.LinkAIIntegrationConfigChatParams{
			AiIntegrationConfigID: src.cfg.ID,
			ChatID:                chatID,
			ProjectID:             src.cfg.ProjectID,
		}); err != nil {
			return fmt.Errorf("link codex cloud chat: %w", err)
		}
		src.progressMu.Lock()
		src.progress.ChatsUpserted++
		src.progressMu.Unlock()
	}

	src.chatIDs[sessionID] = chatID
	if title != "" {
		src.titledSessions[sessionID] = true
	}
	return nil
}

// eventCreatedAt resolves an admitted event's timestamp, counting a fallback
// to import time — the outcome that rewrites chronology — as a canary for
// upstream format changes. Every event is expected to carry a timestamp, so
// absence counts too. Called once per admitted event (see writeFile), whose
// result both the chat row and the message row read, so one bad value cannot
// count twice.
func (src *codexCloudSource) eventCreatedAt(event codexCloudEvent) time.Time {
	if event.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339, event.Timestamp); err == nil {
			return t.UTC()
		}
	}
	src.progressMu.Lock()
	src.progress.TimestampFallbacks++
	src.progressMu.Unlock()
	return time.Now().UTC()
}

func (src *codexCloudSource) timestampFallbacks() int {
	src.progressMu.Lock()
	defer src.progressMu.Unlock()
	return src.progress.TimestampFallbacks
}

func parseCodexCloudEvents(file codexapi.LogFile, body []byte) ([]codexCloudEvent, error) {
	if file.FileSHA256 != "" {
		sum := sha256.Sum256(body)
		actual := hex.EncodeToString(sum[:])
		if !strings.EqualFold(actual, file.FileSHA256) {
			return nil, fmt.Errorf("codex cloud log sha256 mismatch for %s", file.ID)
		}
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	events := make([]codexCloudEvent, 0)
	for {
		var event codexCloudEvent
		if err := decoder.Decode(&event); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode codex cloud log event in %s: %w", file.ID, err)
		}
		if event.Type != "" && event.Type != codexCloudEventType {
			continue
		}
		events = append(events, event)
	}
	return events, nil
}

// codexCloudChatTitle derives a chat title from a session's first prompt,
// truncated by runes so multi-byte text stays valid. Empty when no prompt was
// seen — the upsert then sends NULL and preserves any stored title.
func codexCloudChatTitle(prompt string) string {
	return conv.TruncateString(strings.TrimSpace(prompt), codexCloudTitleMaxRunes)
}
