// Package chatevents lands created chat messages in agent_events by logging
// each one through the OTel pipeline, so every capture lane reaches analytics
// through the same transform as a producer's own telemetry.
package chatevents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/log"
	"golang.org/x/sync/errgroup"

	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	"github.com/speakeasy-api/gram/server/internal/assets/blobio"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/otelpub"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

// Scope is the instrumentation scope the bridge logs under; the dialect that
// reads these records matches on it.
const Scope = "github.com/speakeasy-api/gram/server/internal/chatevents"

// EventName names every record the bridge logs.
const EventName = "gram.chat.message"

// MessageSource resolves a tenant's persisted messages in a single batch.
type MessageSource interface {
	LoadMessages(ctx context.Context, organizationID string, projectID uuid.UUID, ids []uuid.UUID) ([]repo.ListMessagesForAgentEventsRow, error)
}

// Repository reads current message state through the tenant-pinned query.
type Repository struct {
	db repo.DBTX
}

func NewRepository(db repo.DBTX) *Repository {
	return &Repository{db: db}
}

func (r *Repository) LoadMessages(ctx context.Context, organizationID string, projectID uuid.UUID, ids []uuid.UUID) ([]repo.ListMessagesForAgentEventsRow, error) {
	rows, err := repo.New(r.db).ListMessagesForAgentEvents(ctx, repo.ListMessagesForAgentEventsParams{ProjectID: projectID, OrganizationID: organizationID, Ids: ids})
	if err != nil {
		return nil, fmt.Errorf("load messages for agent events: %w", err)
	}
	return rows, nil
}

// Handler logs each created user or assistant message as one log record.
type Handler struct {
	logger      *slog.Logger
	messages    MessageSource
	blobs       blobio.Reader
	logsEnabled func(context.Context, string) (bool, error)
	records     *otelpub.Logger
}

func NewHandler(logger *slog.Logger, messages MessageSource, blobs blobio.Reader, logsEnabled func(context.Context, string) (bool, error), records *otelpub.Logger) *Handler {
	return &Handler{
		logger:      logger.With(attr.SlogComponent("chat-events-bridge")),
		messages:    messages,
		blobs:       blobs,
		logsEnabled: logsEnabled,
		records:     records,
	}
}

var _ streams.BatchResultHandler[*conversationv1.MessageEvent] = (*Handler)(nil)

type message struct {
	event *conversationv1.MessageEvent
	fail  func(error)
}

func (h *Handler) HandleBatchWithResult(ctx context.Context, messages []streams.BatchMessage[*conversationv1.MessageEvent]) error {
	batch := make([]message, len(messages))
	for i, m := range messages {
		batch[i] = message{event: m.Message, fail: m.Fail}
	}
	return h.handleBatch(ctx, batch)
}

// eligible keeps created user and assistant messages with a parseable
// identity; everything else is acknowledged untouched.
func eligible(m *conversationv1.MessageEvent) bool {
	if m == nil || m.GetType() != conversationv1.MessageEvent_TYPE_CREATED {
		return false
	}
	if m.GetRole() != conversationv1.MessageEvent_ROLE_USER && m.GetRole() != conversationv1.MessageEvent_ROLE_ASSISTANT {
		return false
	}
	for _, id := range []string{m.GetId(), m.GetMessageId(), m.GetConversationId(), m.GetProjectId()} {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed == uuid.Nil {
			return false
		}
	}
	return m.GetOrganizationId() != ""
}

// handleBatch reads once per organization/project. A failed gate check or
// tenant read fails only that tenant's messages. A missing or mismatched row
// is acknowledged, since redelivery cannot restore it.
func (h *Handler) handleBatch(ctx context.Context, batch []message) error {
	type tenant struct {
		org     string
		project uuid.UUID
	}
	tenants := make(map[tenant][]message)
	for _, m := range batch {
		if eligible(m.event) {
			key := tenant{org: m.event.GetOrganizationId(), project: uuid.MustParse(m.event.GetProjectId())}
			tenants[key] = append(tenants[key], m)
		}
	}
	var group errgroup.Group
	group.SetLimit(4)
	for key, messages := range tenants {
		enabled, err := h.logsEnabled(ctx, key.org)
		if err != nil {
			err = fmt.Errorf("check logs enabled: %w", err)
			for _, m := range messages {
				m.fail(err)
			}
			continue
		}
		if !enabled {
			continue
		}
		ids := make([]uuid.UUID, 0, len(messages))
		seen := make(map[uuid.UUID]bool, len(messages))
		for _, m := range messages {
			id := uuid.MustParse(m.event.GetMessageId())
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
		rows, err := h.messages.LoadMessages(ctx, key.org, key.project, ids)
		if err != nil {
			for _, m := range messages {
				m.fail(err)
			}
			continue
		}
		byID := make(map[uuid.UUID]repo.ListMessagesForAgentEventsRow, len(rows))
		for _, row := range rows {
			byID[row.ID] = row
		}
		for _, m := range messages {
			row, ok := byID[uuid.MustParse(m.event.GetMessageId())]
			if !ok {
				continue
			}
			group.Go(func() error {
				if err := h.log(ctx, m.event, row); err != nil {
					m.fail(err)
				}
				return nil
			})
		}
	}
	if err := group.Wait(); err != nil {
		return fmt.Errorf("log chat message batch: %w", err)
	}
	return nil
}

func (h *Handler) log(ctx context.Context, event *conversationv1.MessageEvent, row repo.ListMessagesForAgentEventsRow) error {
	if row.ChatID.String() != event.GetConversationId() || row.OrganizationID != event.GetOrganizationId() {
		return nil
	}
	if row.Role != "user" && row.Role != "assistant" {
		return nil
	}
	text, err := h.text(ctx, row)
	if err != nil {
		return err
	}
	sessionID := chat.AgentSessionID(row.ChatID, row.ExternalChatID.String, row.InferenceSessionID.String)
	key := chat.MessageTextKey(sessionID, row.Role, text)
	if key == "" {
		return nil
	}
	if err := h.records.Log(otelpub.WithTenant(ctx, event.GetOrganizationId(), event.GetProjectId()), record(row, sessionID, key, text)); err != nil {
		return fmt.Errorf("log chat message: %w", err)
	}
	return nil
}

// text is the message in words: the stored content blocks, else the text
// projection, else the spilled content asset.
func (h *Handler) text(ctx context.Context, row repo.ListMessagesForAgentEventsRow) (string, error) {
	if text := rawText(row.ContentRaw); text != "" {
		return text, nil
	}
	if row.Content != "" {
		return row.Content, nil
	}
	if !row.ContentAssetUrl.Valid || row.ContentAssetUrl.String == "" {
		return "", nil
	}
	data, err := blobio.ReadAllString(ctx, h.blobs, row.ContentAssetUrl.String, maxContentBytes)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, storage.ErrObjectNotExist) {
		// Redelivery cannot bring the asset back.
		h.logger.WarnContext(ctx, "chat message content asset is missing", attr.SlogChatID(row.ChatID.String()), attr.SlogChatMessageID(row.ID.String()))
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read chat message content asset: %w", err)
	}
	return rawText([]byte(data)), nil
}

// record is deterministic for a row, so a redelivery publishes the same
// record id and collapses downstream.
func record(row repo.ListMessagesForAgentEventsRow, sessionID, key, text string) log.Record {
	var r log.Record
	r.SetTimestamp(row.CreatedAt.Time)
	r.SetEventName(EventName)
	r.SetBody(log.StringValue(text))
	r.AddAttributes(
		log.String(string(attr.ChatIDKey), row.ChatID.String()),
		log.String(string(attr.ChatMessageIDKey), row.ID.String()),
		log.String(string(attr.ChatMessageKeyKey), key),
		log.String(string(attr.ChatRoleKey), row.Role),
		log.String(string(attr.SessionIDKey), sessionID),
	)
	for _, kv := range []struct {
		key   attr.Key
		value string
	}{
		{key: attr.HookTurnIDKey, value: row.MessageID.String},
		{key: attr.GenAIResponseModelKey, value: row.Model.String},
		{key: attr.HookSourceKey, value: row.Source.String},
		{key: attr.UserIDKey, value: row.UserID},
		{key: attr.UserEmailKey, value: row.UserEmail},
		{key: attr.ExternalUserIDKey, value: row.ExternalUserID},
	} {
		if kv.value != "" {
			r.AddAttributes(log.String(string(kv.key), kv.value))
		}
	}
	return r
}
