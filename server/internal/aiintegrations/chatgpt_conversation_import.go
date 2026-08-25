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
	// chatgptConversationEventType is the Compliance Logs Platform file
	// family carrying ChatGPT conversation content. The feed is
	// workspace-scoped: one NDJSON event per message, files finalized on the
	// API's end_time watermark like COSTS files.
	chatgptConversationEventType = "CONVERSATION_MESSAGE"
	chatgptCompliancePageLimit   = 100
	// chatgptConversationSourceSlug is the canonical chat source for imported
	// ChatGPT conversations (see chat/sources.go). The feed's
	// author.client_type (e.g. desktop_web) is finer-grained than our
	// product-surface taxonomy, so every event maps to the one chatgpt
	// surface and the raw client type rides the message's user_agent column.
	chatgptConversationSourceSlug = "chatgpt"
)

// chatgptConversationEvent is one CONVERSATION_MESSAGE NDJSON event.
type chatgptConversationEvent struct {
	EventID   string `json:"event_id"`
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Actor     struct {
		Type      string `json:"type"`
		UserID    string `json:"user_id"`
		UserEmail string `json:"user_email"`
	} `json:"actor"`
	PreviousMessageID string `json:"previous_message_id"`
	Message           struct {
		ID        string `json:"id"`
		CreatedAt string `json:"created_at"`
		Author    struct {
			Type       string `json:"type"`
			ClientType string `json:"client_type"`
		} `json:"author"`
		Content struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"content"`
	} `json:"message"`
	Conversation struct {
		ID              string `json:"id"`
		Title           string `json:"title"`
		CreatedAt       string `json:"created_at"`
		IsPinned        bool   `json:"is_pinned"`
		IsTemporaryChat bool   `json:"is_temporary_chat"`
	} `json:"conversation"`
}

// ChatGPTConversationSyncProgress reports how far one sync run got.
type ChatGPTConversationSyncProgress struct {
	WindowStart     time.Time `json:"window_start"`
	LogPages        int       `json:"log_pages"`
	LogFiles        int       `json:"log_files"`
	Events          int       `json:"events"`
	MessagesWritten int64     `json:"messages_written"`
	ChatsUpserted   int       `json:"chats_upserted"`
	// TimestampFallbacks counts events whose timestamps failed RFC3339
	// parsing and fell back to import time — a canary for upstream format
	// changes that would otherwise silently rewrite history chronology.
	TimestampFallbacks int       `json:"timestamp_fallbacks"`
	WatermarkReached   time.Time `json:"watermark_reached"`
}

type ChatGPTConversationImportService struct {
	logger         *slog.Logger
	store          *Store
	guardianPolicy *guardian.Policy
	db             *pgxpool.Pool
	writer         *chat.ChatMessageWriter
	mirror         *ChatOTELMirror
	heartbeat      func(ctx context.Context, page int)
}

func NewChatGPTConversationImportService(logger *slog.Logger, store *Store, db *pgxpool.Pool, guardianPolicy *guardian.Policy, writer *chat.ChatMessageWriter, mirror *ChatOTELMirror, heartbeat func(ctx context.Context, page int)) *ChatGPTConversationImportService {
	if heartbeat == nil {
		panic("chatgpt conversation import service requires heartbeat")
	}
	return &ChatGPTConversationImportService{
		logger:         logger.With(attr.SlogComponent("aiintegrations.chatgpt_compliance")),
		store:          store,
		guardianPolicy: guardianPolicy,
		db:             db,
		writer:         writer,
		mirror:         mirror,
		heartbeat:      heartbeat,
	}
}

// SyncChatGPTConversations imports ChatGPT conversation-message compliance
// logs through the shared time-window poller and persists them as external
// chats + messages, the same tables the Anthropic compliance import writes.
// Replays are idempotent: chats upsert on the external chat id and messages
// land under ON CONFLICT (chat_id, external_message_id) DO NOTHING.
func (s *ChatGPTConversationImportService) SyncChatGPTConversations(ctx context.Context, cfg Config, endTime time.Time) error {
	if cfg.Provider != ProviderChatGPTCompliance {
		return fmt.Errorf("unsupported ai integration provider for chatgpt conversation import: %s", cfg.Provider)
	}
	if cfg.ExternalOrganizationID == nil {
		return fmt.Errorf("external_organization_id (workspace id) is required for chatgpt_compliance")
	}

	progress := &ChatGPTConversationSyncProgress{
		WindowStart:        cfg.PollWatermarkAt,
		LogPages:           0,
		LogFiles:           0,
		Events:             0,
		MessagesWritten:    0,
		ChatsUpserted:      0,
		TimestampFallbacks: 0,
		WatermarkReached:   cfg.PollWatermarkAt,
	}

	source := &chatgptConversationSource{
		client:     codexapi.NewWorkspaceClient(s.guardianPolicy, *cfg.ExternalOrganizationID, codexapi.WithAPIKey(cfg.APIKey)),
		svc:        s,
		cfg:        cfg,
		pageLimit:  chatgptCompliancePageLimit,
		users:      newConnectedUserResolver(s.db, cfg.OrganizationID),
		chatIDs:    map[string]uuid.UUID{},
		chatTitles: map[string]string{},
		progressMu: sync.Mutex{},
		progress:   progress,
	}

	runner := &timewindowpoller.Poller[[]codexapi.LogFile]{
		Store:    s.store,
		Schedule: ScheduleChatGPTCompliance,
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
		return newSyncError("sync chatgpt conversations", *progress,
			SyncStageError{Stage: "import_conversation_logs", Err: err},
		)
	}
	return nil
}

type chatgptConversationSource struct {
	client    codexComplianceClient
	svc       *ChatGPTConversationImportService
	cfg       Config
	pageLimit int
	users     *connectedUserResolver
	// chatIDs caches conversation id -> chat row id for the run so link rows
	// and id lookups happen once per conversation, not once per event.
	// chatTitles remembers the last title written per conversation so an
	// unchanged title does not re-upsert on every file.
	chatIDs    map[string]uuid.UUID
	chatTitles map[string]string
	// progressMu guards progress: the poller pipelines FetchPage (producer
	// goroutine) with ProcessPage (consumer goroutine), and both record
	// progress. The unsynchronized read in String() is safe because it only
	// runs after the poller's goroutines have joined.
	progressMu sync.Mutex
	progress   *ChatGPTConversationSyncProgress
}

func (src *chatgptConversationSource) UpperBound(ctx context.Context, endTime time.Time) (time.Time, error) {
	after := chatgptUpperBoundStart(src.cfg, endTime)
	watermark := time.Time{}
	for pageNum := 0; ; pageNum++ {
		// The poller only heartbeats before UpperBound; a 30-day first sync
		// over this per-message feed can page for longer than the activity's
		// 1-minute heartbeat timeout, so heartbeat per listing page.
		src.svc.heartbeat(ctx, pageNum)
		page, err := src.client.ListLogs(ctx, codexapi.ListLogsParams{
			EventType: chatgptConversationEventType,
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
			return time.Time{}, fmt.Errorf("chatgpt compliance logs page had has_more without last_end_time")
		}
		if err := validateCodexLastEndTimeAdvanced(after, page.LastEndTime); err != nil {
			return time.Time{}, err
		}
		after = page.LastEndTime
	}
}

func (src *chatgptConversationSource) FetchPage(ctx context.Context, start, end time.Time, pageToken string) (timewindowpoller.Page[[]codexapi.LogFile], error) {
	after := start
	if pageToken != "" {
		parsed, err := time.Parse(time.RFC3339Nano, pageToken)
		if err != nil {
			return timewindowpoller.Page[[]codexapi.LogFile]{Payload: nil, NextPage: "", HasMore: false}, fmt.Errorf("parse chatgpt compliance page token: %w", err)
		}
		after = parsed
	}

	page, err := src.client.ListLogs(ctx, codexapi.ListLogsParams{
		EventType: chatgptConversationEventType,
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
		if file.EventType != "" && file.EventType != chatgptConversationEventType {
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
			return timewindowpoller.Page[[]codexapi.LogFile]{Payload: nil, NextPage: "", HasMore: false}, fmt.Errorf("chatgpt compliance logs page had has_more without last_end_time")
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

func (src *chatgptConversationSource) ProcessPage(ctx context.Context, files []codexapi.LogFile) error {
	for i, file := range files {
		// The poller only heartbeats around FetchPage; a page holds up to
		// pageLimit dense per-message files, so heartbeat per file to stay
		// inside the activity's 1-minute heartbeat timeout.
		src.svc.heartbeat(ctx, i)

		body, err := src.client.DownloadLog(ctx, file.ID)
		if err != nil {
			return err //nolint:wrapcheck // Preserve HTTPError classification upstream.
		}

		events, err := parseChatGPTConversationEvents(file, body)
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

func (src *chatgptConversationSource) RetryAfter(err error) (time.Duration, bool) {
	var httpErr *codexapi.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	return 0, true
}

// conversationState carries per-conversation metadata accumulated across one
// file's events: the newest event (actor identity, creation time) and the
// newest non-empty title.
type conversationState struct {
	latest chatgptConversationEvent
	title  string
}

// writeFile persists one log file's events: each conversation is upserted
// with the file's newest metadata, then every user/assistant message lands
// in one batched write. Newest-event-wins metadata means an early untitled
// event cannot pin a conversation untitled, and full replays converge on the
// current title because files process in end_time order so the last upsert
// carries the newest state. Events with no message or conversation identity
// are skipped: without an external message id the insert cannot deduplicate
// across replayed windows.
func (src *chatgptConversationSource) writeFile(ctx context.Context, file codexapi.LogFile, events []chatgptConversationEvent) error {
	conversations := map[string]*conversationState{}
	order := make([]string, 0, len(events))
	for _, event := range events {
		if event.Conversation.ID == "" || event.Message.ID == "" {
			continue
		}
		state, ok := conversations[event.Conversation.ID]
		if !ok {
			state = &conversationState{latest: event, title: ""}
			conversations[event.Conversation.ID] = state
			order = append(order, event.Conversation.ID)
		}
		// Events within a file are chronological, so the last one seen is
		// the newest.
		state.latest = event
		if title := strings.TrimSpace(event.Conversation.Title); title != "" {
			state.title = title
		}
	}
	for _, conversationID := range order {
		if err := src.upsertConversationChat(ctx, conversations[conversationID]); err != nil {
			return err
		}
	}

	fallbacksBefore := src.timestampFallbacks()
	rows := make([]chatrepo.CreateExternalChatMessageParams, 0, len(events))
	// The actor email rides beside the rows for the OTEL mirror; the rows
	// themselves only store the provider user id.
	emails := make(map[string]string, len(events))
	for _, event := range events {
		if event.Conversation.ID == "" || event.Message.ID == "" {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(event.Message.Author.Type))
		if role != "user" && role != "assistant" {
			continue
		}

		userID, err := src.users.resolve(ctx, event.Actor.UserEmail)
		if err != nil {
			return err
		}
		emails[event.Message.ID] = event.Actor.UserEmail

		createdAt := src.eventCreatedAt(event)

		content := renderChatGPTContent(event.Message.Content.Value)
		var contentRaw []byte
		if len(event.Message.Content.Value) > 0 && len(event.Message.Content.Value) <= maxInlineExternalContentSize {
			contentRaw = event.Message.Content.Value
		}

		rows = append(rows, chatrepo.CreateExternalChatMessageParams{
			ChatID:            src.chatIDs[event.Conversation.ID],
			Role:              role,
			ProjectID:         src.cfg.ProjectID,
			Content:           content,
			ContentRaw:        contentRaw,
			ContentAssetUrl:   pgtype.Text{String: "", Valid: false},
			StorageError:      pgtype.Text{String: "", Valid: false},
			Model:             pgtype.Text{String: "", Valid: false},
			MessageID:         pgtype.Text{String: "", Valid: false},
			ToolCallID:        pgtype.Text{String: "", Valid: false},
			UserID:            conv.ToPGText(userID),
			ExternalUserID:    conv.ToPGText(event.Actor.UserID),
			ExternalMessageID: conv.ToPGText(event.Message.ID),
			FinishReason:      pgtype.Text{String: "", Valid: false},
			ToolCalls:         nil,
			PromptTokens:      0,
			CompletionTokens:  0,
			TotalTokens:       0,
			Origin:            pgtype.Text{String: "", Valid: false},
			// The feed has no browser user agent; the author's client type
			// (e.g. desktop_web) is the closest surface signal, so it rides
			// this column for later per-client analysis.
			UserAgent:   conv.ToPGTextEmpty(event.Message.Author.ClientType),
			IpAddress:   pgtype.Text{String: "", Valid: false},
			Source:      conv.ToPGText(chatgptConversationSourceSlug),
			ContentHash: nil,
			Generation:  0,
			CreatedAt:   conv.ToPGTimestamptz(createdAt),
		})
	}
	if fallbacks := src.timestampFallbacks() - fallbacksBefore; fallbacks > 0 {
		src.svc.logger.WarnContext(ctx, "chatgpt compliance event timestamps failed to parse; messages stamped with import time",
			attr.SlogFilePath(file.FileName),
			attr.SlogChatGPTComplianceTimestampFallbacks(fallbacks),
		)
	}
	if len(rows) == 0 {
		return nil
	}

	// WriteExternal inserts row by row, so a mid-batch failure still leaves
	// the earlier rows durable — record the partial count and mirror the
	// inserted rows before propagating the error, since a retry will neither
	// re-count nor re-offer them.
	inserted, err := src.svc.writer.WriteExternal(ctx, src.cfg.ProjectID, rows)
	src.progressMu.Lock()
	src.progress.MessagesWritten += int64(len(inserted))
	src.progressMu.Unlock()
	src.svc.mirror.PublishMessages(ctx, src.cfg, inserted, emails)
	if err != nil {
		return fmt.Errorf("write chatgpt conversation messages: %w", err)
	}
	return nil
}

// upsertConversationChat writes the conversation's chat row with the newest
// metadata seen for it. Re-upserts on later files refresh the title (any
// non-NULL incoming title overwrites via the query's COALESCE), while an
// empty title becomes NULL and preserves whatever is already stored.
func (src *chatgptConversationSource) upsertConversationChat(ctx context.Context, state *conversationState) error {
	conversationID := state.latest.Conversation.ID
	_, known := src.chatIDs[conversationID]
	if known && (state.title == "" || state.title == src.chatTitles[conversationID]) {
		return nil
	}

	createdAt := src.parseEventTime(state.latest.Conversation.CreatedAt, time.Now())
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
		ExternalChatID: conv.ToPGText(conversationID),
		Title:          conv.ToPGTextEmpty(state.title),
		CreatedAt:      conv.ToPGTimestamptz(createdAt),
		UpdatedAt:      conv.ToPGTimestamptz(createdAt),
		// Feed titles are authoritative: newest non-null title wins.
		PreferStoredTitle: false,
	})
	if err != nil {
		return fmt.Errorf("upsert chatgpt compliance chat: %w", err)
	}
	if !known {
		if _, err := chatrepo.New(src.svc.db).LinkAIIntegrationConfigChat(ctx, chatrepo.LinkAIIntegrationConfigChatParams{
			AiIntegrationConfigID: src.cfg.ID,
			ChatID:                chatID,
			ProjectID:             src.cfg.ProjectID,
		}); err != nil {
			return fmt.Errorf("link chatgpt compliance chat: %w", err)
		}
		src.progressMu.Lock()
		src.progress.ChatsUpserted++
		src.progressMu.Unlock()
	}

	src.chatIDs[conversationID] = chatID
	if state.title != "" {
		src.chatTitles[conversationID] = state.title
	}
	return nil
}

// eventCreatedAt resolves a message's timestamp chain (message.created_at →
// event.timestamp → import time), counting a fallback only when the final
// import-time default is used — the outcome that actually rewrites
// chronology. A malformed created_at rescued by a valid event timestamp is
// not a fallback, and two absent timestamps are.
func (src *chatgptConversationSource) eventCreatedAt(event chatgptConversationEvent) time.Time {
	for _, value := range []string{event.Message.CreatedAt, event.Timestamp} {
		if value == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, value); err == nil {
			return t.UTC()
		}
	}
	src.progressMu.Lock()
	src.progress.TimestampFallbacks++
	src.progressMu.Unlock()
	return time.Now().UTC()
}

// parseEventTime is parseTimeOrDefault with fallback accounting for
// single-value timestamps: absent is fine (the fallback is expected), but a
// present-yet-malformed value counts as a canary for feed format changes.
func (src *chatgptConversationSource) parseEventTime(value string, fallback time.Time) time.Time {
	if value == "" {
		return fallback.UTC()
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		src.progressMu.Lock()
		src.progress.TimestampFallbacks++
		src.progressMu.Unlock()
		return fallback.UTC()
	}
	return t.UTC()
}

func (src *chatgptConversationSource) timestampFallbacks() int {
	src.progressMu.Lock()
	defer src.progressMu.Unlock()
	return src.progress.TimestampFallbacks
}

func parseChatGPTConversationEvents(file codexapi.LogFile, body []byte) ([]chatgptConversationEvent, error) {
	if file.FileSHA256 != "" {
		sum := sha256.Sum256(body)
		actual := hex.EncodeToString(sum[:])
		if !strings.EqualFold(actual, file.FileSHA256) {
			return nil, fmt.Errorf("chatgpt compliance log sha256 mismatch for %s", file.ID)
		}
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	events := make([]chatgptConversationEvent, 0)
	for {
		var event chatgptConversationEvent
		if err := decoder.Decode(&event); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode chatgpt compliance log event in %s: %w", file.ID, err)
		}
		if event.Type != "" && event.Type != chatgptConversationEventType {
			continue
		}
		events = append(events, event)
	}
	return events, nil
}

// renderChatGPTContent flattens the message content value for display. The
// observed shape is a plain JSON string (content.type=text); anything else
// falls back to the raw JSON so no content is silently dropped.
func renderChatGPTContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return string(raw)
}

func chatgptUpperBoundStart(cfg Config, endTime time.Time) time.Time {
	if cfg.PollCheckpoint.Partial() {
		return cfg.PollCheckpoint.Watermark
	}
	if !cfg.PollWatermarkAt.IsZero() {
		return cfg.PollWatermarkAt
	}
	return endTime.UTC().Add(-chatgptComplianceInitialLookback)
}
