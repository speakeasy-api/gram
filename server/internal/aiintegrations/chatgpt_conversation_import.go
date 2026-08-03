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
	"github.com/speakeasy-api/gram/server/internal/oops"
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
	WindowStart      time.Time
	LogPages         int
	LogFiles         int
	Events           int
	MessagesWritten  int64
	ChatsUpserted    int
	WatermarkReached time.Time
}

type ChatGPTConversationImportService struct {
	logger         *slog.Logger
	store          *Store
	guardianPolicy *guardian.Policy
	db             *pgxpool.Pool
	writer         *chat.ChatMessageWriter
	heartbeat      func(ctx context.Context, page int)
}

func NewChatGPTConversationImportService(logger *slog.Logger, store *Store, db *pgxpool.Pool, guardianPolicy *guardian.Policy, writer *chat.ChatMessageWriter, heartbeat func(ctx context.Context, page int)) *ChatGPTConversationImportService {
	if heartbeat == nil {
		panic("chatgpt conversation import service requires heartbeat")
	}
	return &ChatGPTConversationImportService{
		logger:         logger.With(attr.SlogComponent("aiintegrations.chatgpt_compliance")),
		store:          store,
		guardianPolicy: guardianPolicy,
		db:             db,
		writer:         writer,
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
		return oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider for chatgpt conversation import: %s", cfg.Provider)
	}
	if cfg.ExternalOrganizationID == nil {
		return oops.E(oops.CodeInvalid, nil, "external_organization_id (workspace id) is required for chatgpt_compliance")
	}

	progress := &ChatGPTConversationSyncProgress{
		WindowStart:      cfg.PollWatermarkAt,
		LogPages:         0,
		LogFiles:         0,
		Events:           0,
		MessagesWritten:  0,
		ChatsUpserted:    0,
		WatermarkReached: cfg.PollWatermarkAt,
	}

	source := &chatgptConversationSource{
		client:    codexapi.NewWorkspaceClient(s.guardianPolicy, *cfg.ExternalOrganizationID, codexapi.WithAPIKey(cfg.APIKey)),
		svc:       s,
		cfg:       cfg,
		pageLimit: chatgptCompliancePageLimit,
		users:     newConnectedUserResolver(s.db, cfg.OrganizationID),
		chatIDs:   map[string]uuid.UUID{},
		progress:  progress,
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
	// chatIDs caches conversation id -> chat row id for the run so each
	// conversation is upserted once, not once per message event.
	chatIDs  map[string]uuid.UUID
	progress *ChatGPTConversationSyncProgress
}

func (src *chatgptConversationSource) UpperBound(ctx context.Context, endTime time.Time) (time.Time, error) {
	after := chatgptUpperBoundStart(src.cfg, endTime)
	watermark := time.Time{}
	for {
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
	src.progress.LogPages++
	if page.LastEndTime.After(src.progress.WatermarkReached) {
		src.progress.WatermarkReached = page.LastEndTime
	}

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
	for _, file := range files {
		body, err := src.client.DownloadLog(ctx, file.ID)
		if err != nil {
			return err //nolint:wrapcheck // Preserve HTTPError classification upstream.
		}
		src.progress.LogFiles++

		events, err := parseChatGPTConversationEvents(file, body)
		if err != nil {
			return err
		}
		src.progress.Events += len(events)

		for _, event := range events {
			if err := src.writeEvent(ctx, event); err != nil {
				return err
			}
		}
		if file.EndTime.After(src.progress.WatermarkReached) {
			src.progress.WatermarkReached = file.EndTime
		}
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

// writeEvent upserts the event's conversation and persists its message.
// Events with no message or conversation identity are skipped: without an
// external message id the insert cannot deduplicate across replayed windows.
func (src *chatgptConversationSource) writeEvent(ctx context.Context, event chatgptConversationEvent) error {
	if event.Conversation.ID == "" || event.Message.ID == "" {
		return nil
	}
	role := strings.ToLower(strings.TrimSpace(event.Message.Author.Type))
	if role != "user" && role != "assistant" {
		return nil
	}

	chatID, err := src.upsertConversationChat(ctx, event)
	if err != nil {
		return err
	}

	userID, err := src.users.resolve(ctx, event.Actor.UserEmail)
	if err != nil {
		return err
	}

	createdAt := parseTimeOrDefault(event.Message.CreatedAt, time.Time{})
	if createdAt.IsZero() {
		createdAt = parseTimeOrDefault(event.Timestamp, time.Now())
	}

	content := renderChatGPTContent(event.Message.Content.Value)
	var contentRaw []byte
	if len(event.Message.Content.Value) > 0 && len(event.Message.Content.Value) <= maxInlineExternalContentSize {
		contentRaw = event.Message.Content.Value
	}

	written, err := src.svc.writer.WriteExternal(ctx, src.cfg.ProjectID, []chatrepo.CreateExternalChatMessageParams{{
		ChatID:            chatID,
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
	}})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "write chatgpt conversation message")
	}
	src.progress.MessagesWritten += written
	return nil
}

func (src *chatgptConversationSource) upsertConversationChat(ctx context.Context, event chatgptConversationEvent) (uuid.UUID, error) {
	if chatID, ok := src.chatIDs[event.Conversation.ID]; ok {
		return chatID, nil
	}

	createdAt := parseTimeOrDefault(event.Conversation.CreatedAt, time.Now())
	userID, err := src.users.resolve(ctx, event.Actor.UserEmail)
	if err != nil {
		return uuid.Nil, err
	}

	chatID, err := chatrepo.New(src.svc.db).UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
		ID:             uuid.New(),
		ProjectID:      src.cfg.ProjectID,
		OrganizationID: src.cfg.OrganizationID,
		// NULL when unresolved so the upsert's COALESCE preserves a user
		// resolved by an earlier event.
		UserID:         conv.ToPGTextEmpty(userID),
		ExternalUserID: conv.ToPGTextEmpty(event.Actor.UserID),
		ExternalChatID: conv.ToPGText(event.Conversation.ID),
		Title:          conv.ToPGTextEmpty(event.Conversation.Title),
		CreatedAt:      conv.ToPGTimestamptz(createdAt),
		UpdatedAt:      conv.ToPGTimestamptz(createdAt),
	})
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "upsert chatgpt compliance chat")
	}
	if _, err := chatrepo.New(src.svc.db).LinkAIIntegrationConfigChat(ctx, chatrepo.LinkAIIntegrationConfigChatParams{
		AiIntegrationConfigID: src.cfg.ID,
		ChatID:                chatID,
	}); err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "link chatgpt compliance chat")
	}

	src.chatIDs[event.Conversation.ID] = chatID
	src.progress.ChatsUpserted++
	return chatID, nil
}

func parseChatGPTConversationEvents(file codexapi.LogFile, body []byte) ([]chatgptConversationEvent, error) {
	if file.FileSHA256 != "" {
		sum := sha256.Sum256(body)
		actual := hex.EncodeToString(sum[:])
		if !strings.EqualFold(actual, file.FileSHA256) {
			return nil, oops.E(oops.CodeUnexpected, nil, "chatgpt compliance log sha256 mismatch for %s", file.ID)
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
			return nil, oops.E(oops.CodeUnexpected, err, "decode chatgpt compliance log event in %s", file.ID)
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
