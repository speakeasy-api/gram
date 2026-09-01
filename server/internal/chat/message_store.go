package chat

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
	"path"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/stokens"
)

// ErrChatNotInProject indicates that a chat is absent or owned by another project.
var ErrChatNotInProject = errors.New("chat does not belong to project")

// MessageWrite pairs persisted native message parameters with producer-only provenance.
type MessageWrite struct {
	// Params contains the SQLc parameters persisted for the message.
	Params repo.CreateChatMessageParams

	// BillingUserID is the Gram user to whom this feature explicitly allocates usage.
	BillingUserID string

	// AssistantID identifies the assistant responsible for this workload when applicable.
	AssistantID uuid.UUID

	// WorkloadSource identifies the product path responsible for this workload.
	WorkloadSource metering.WorkloadSource

	// UserEmail is the email explicitly observed for the message actor.
	UserEmail string

	// Provider is the AI provider explicitly reported by ingestion.
	Provider string

	// HookHostname is the device hostname explicitly reported by hook ingestion.
	HookHostname string

	// AccountType is the AI account classification reported for the session.
	AccountType string

	// BillingMode is the AI account billing mode resolved for the session.
	BillingMode string
}

// ExternalMessageWrite pairs persisted imported message parameters with producer-only provenance.
type ExternalMessageWrite struct {
	// Params contains the SQLc parameters persisted for the imported message.
	Params repo.CreateExternalChatMessageParams

	// BillingUserID is the Gram user to whom this import explicitly allocates usage.
	BillingUserID string

	// WorkloadSource identifies the product path responsible for this imported workload.
	WorkloadSource metering.WorkloadSource

	// UserEmail is the email explicitly observed for the imported message actor.
	UserEmail string

	// Provider is the AI provider explicitly reported by ingestion.
	Provider string

	// HookHostname is the device hostname explicitly reported by hook ingestion.
	HookHostname string

	// AccountType is the AI account classification reported for the imported session.
	AccountType string

	// BillingMode is the AI account billing mode declared for the import.
	BillingMode string
}

// ChatMessageWriter is the only sanctioned way to persist chat messages.
// It wraps repo.CreateChatMessage and notifies observers after a successful
// write that stored at least one message. External packages must use Write,
// WriteCorrelated, WriteExternal, WriteTurn, or WriteWithAssets.
type ChatMessageWriter struct {
	db           *pgxpool.Pool
	logger       *slog.Logger
	assetStorage assets.BlobStore
	stokenCodec  *stokens.Codec
	observers    []MessageObserver
	// turnStream, when set, receives a frame per persisted row so dashboard
	// subscribers can render a turn without polling. Nil disables publishing.
	turnStream  *TurnStream
	shutdownCtx context.Context //nolint:containedctx // must outlive any single request
	cancel      context.CancelFunc
}

func NewChatMessageWriter(logger *slog.Logger, db *pgxpool.Pool, assetStorage assets.BlobStore) (w *ChatMessageWriter, shutdown func(context.Context) error) {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:contextcheck // shutdown context must outlive any single request
	w = &ChatMessageWriter{
		db:           db,
		logger:       logger.With(attr.SlogComponent("chat-message-writer")),
		assetStorage: assetStorage,
		stokenCodec:  stokens.NewCodec(),
		observers:    nil,
		shutdownCtx:  ctx,
		cancel:       cancel,
		turnStream:   nil,
	}
	shutdown = func(_ context.Context) error {
		cancel()
		return nil
	}
	return w, shutdown
}

// WithTurnStream enables per-row turn frame publishing.
func (w *ChatMessageWriter) WithTurnStream(stream *TurnStream) *ChatMessageWriter {
	w.turnStream = stream
	return w
}

func (w *ChatMessageWriter) AddObserver(obs MessageObserver) {
	w.observers = append(w.observers, obs)
}

func (w *ChatMessageWriter) WriteContentPartAsset(ctx context.Context, projectID uuid.UUID, chatID uuid.UUID, content []byte) (string, error) {
	urls, err := w.WriteContentPartAssets(ctx, projectID, chatID, [][]byte{content})
	if err != nil {
		return "", err
	}
	return urls[0], nil
}

func (w *ChatMessageWriter) WriteContentPartAssets(ctx context.Context, projectID uuid.UUID, chatID uuid.UUID, contents [][]byte) ([]string, error) {
	if len(contents) == 0 {
		return nil, nil
	}
	if w == nil || w.assetStorage == nil {
		return nil, fmt.Errorf("content part asset storage unavailable")
	}

	paths := make([]string, len(contents))
	leaders := make(map[string]int, len(contents))
	for i, content := range contents {
		hash := sha256.Sum256(content)
		assetPath := path.Join(projectID.String(), "chats", chatID.String(), "content-parts", hex.EncodeToString(hash[:])+".txt")
		paths[i] = assetPath
		if _, ok := leaders[assetPath]; !ok {
			leaders[assetPath] = i
		}
	}

	type uploadResult struct {
		assetURL string
		err      error
	}
	results := make([]uploadResult, len(contents))
	var group errgroup.Group
	group.SetLimit(maxConcurrentChatAssetWork)
	for assetPath, leader := range leaders {
		group.Go(func() error {
			content := contents[leader]
			writer, assetURL, err := w.assetStorage.Write(ctx, assetPath, "text/plain; charset=utf-8", int64(len(content)))
			if err != nil {
				results[leader] = uploadResult{assetURL: "", err: fmt.Errorf("open content part asset for writing: %w", err)}
				return nil
			}
			if _, err := io.Copy(writer, bytes.NewReader(content)); err != nil {
				defer o11y.NoLogDefer(func() error { return writer.Close() })
				results[leader] = uploadResult{assetURL: "", err: fmt.Errorf("write content part asset: %w", err)}
				return nil
			}
			if err := writer.Close(); err != nil {
				results[leader] = uploadResult{assetURL: "", err: fmt.Errorf("close content part asset: %w", err)}
				return nil
			}
			results[leader] = uploadResult{assetURL: assetURL.String(), err: nil}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, fmt.Errorf("upload content part assets: %w", err)
	}

	urls := make([]string, len(contents))
	for i, assetPath := range paths {
		leader := leaders[assetPath]
		result := results[leader]
		if result.err != nil {
			return nil, result.err
		}
		urls[i] = result.assetURL
	}
	return urls, nil
}

// stampMessageFields assigns each message its durable identity before
// persistence so the message and its atomic meter reading share the same id.
// The batch insert uses COPY FROM, which cannot return database-generated ids.
// It also assigns a shared write-time created_at when the caller provides no
// source timestamp; explicit timestamps preserve source-event ordering.
func stampMessageFields(writes []MessageWrite, writeTime time.Time) error {
	createdAt := conv.ToPGTimestamptz(writeTime)
	for i := range writes {
		param := &writes[i].Params
		if param.ID == uuid.Nil {
			id, err := uuid.NewV7()
			if err != nil {
				return fmt.Errorf("generate chat message id: %w", err)
			}
			param.ID = id
		}
		if !param.CreatedAt.Valid {
			param.CreatedAt = createdAt
		}
	}
	return nil
}

// storedToolCall is the minimal persisted tool-call shape needed to meter the
// function name and argument payload alongside the message text.
type storedToolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

// extractMeteredContent returns every stored text fragment that contributes to
// the storage meter. It rejects malformed tool calls rather than silently
// changing how their content is measured.
func extractMeteredContent(content string, toolCalls []byte) ([]string, error) {
	parts := []string{content}
	if len(toolCalls) == 0 {
		return parts, nil
	}

	var calls []storedToolCall
	if err := json.Unmarshal(toolCalls, &calls); err != nil {
		return nil, fmt.Errorf("unmarshal stored tool calls: %w", err)
	}
	for i, call := range calls {
		parts = append(parts, call.Function.Name)
		if len(call.Function.Arguments) == 0 {
			continue
		}
		var arguments *string
		if err := json.Unmarshal(call.Function.Arguments, &arguments); err != nil {
			return nil, fmt.Errorf("unmarshal stored tool call %d arguments: %w", i, err)
		}
		if arguments == nil {
			return nil, fmt.Errorf("stored tool call %d arguments must be a JSON string", i)
		}
		parts = append(parts, *arguments)
	}
	return parts, nil
}

type meterMessageInput struct {
	organizationID        string
	projectID             uuid.UUID
	messageID             uuid.UUID
	chatID                uuid.UUID
	content               string
	toolCalls             []byte
	model                 pgtype.Text
	provider              string
	source                pgtype.Text
	hookHostname          string
	accountType           string
	billingMode           string
	billingUserID         string
	assistantID           uuid.UUID
	workloadSource        metering.WorkloadSource
	messageUserID         pgtype.Text
	messageExternalUserID pgtype.Text
	messageUserEmail      string
	occurredAt            time.Time
}

func setReadingAttribute(attributes *map[string]string, key string, value string) {
	if value == "" {
		return
	}
	if *attributes == nil {
		*attributes = make(map[string]string)
	}
	(*attributes)[key] = value
}

// meterMessage measures one durable message and returns its project-scoped
// storage usage keyed by the message UUID. A zero-token message emits no reading.
func (w *ChatMessageWriter) meterMessage(ctx context.Context, input meterMessageInput) ([]metering.Reading, error) {
	contentParts, err := extractMeteredContent(input.content, input.toolCalls)
	if err != nil {
		return nil, fmt.Errorf("extract stored chat message content: %w", err)
	}
	count, err := w.stokenCodec.Count(ctx, contentParts...)
	if err != nil {
		return nil, fmt.Errorf("count stored chat message: %w", err)
	}
	if count == 0 {
		return nil, nil
	}

	var attributes map[string]string
	if input.chatID != uuid.Nil {
		setReadingAttribute(&attributes, metering.AttributeChatID, input.chatID.String())
	}
	if input.model.Valid {
		setReadingAttribute(&attributes, metering.AttributeModel, input.model.String)
	}
	setReadingAttribute(&attributes, metering.AttributeProvider, input.provider)
	if input.source.Valid {
		setReadingAttribute(&attributes, metering.AttributeHookSource, CanonicalSource(input.source.String))
	}
	setReadingAttribute(&attributes, metering.AttributeHookHostname, input.hookHostname)
	setReadingAttribute(&attributes, metering.AttributeAccountType, input.accountType)
	setReadingAttribute(&attributes, metering.AttributeBillingMode, input.billingMode)
	if input.assistantID != uuid.Nil {
		setReadingAttribute(&attributes, metering.AttributeAssistantID, input.assistantID.String())
	}
	setReadingAttribute(&attributes, metering.AttributeWorkloadSource, string(input.workloadSource))
	setReadingAttribute(&attributes, metering.AttributeBillingUserID, input.billingUserID)
	if input.messageUserID.Valid {
		setReadingAttribute(&attributes, metering.AttributeMessageUserID, input.messageUserID.String)
	}
	if input.messageExternalUserID.Valid {
		setReadingAttribute(&attributes, metering.AttributeMessageExternalUserID, input.messageExternalUserID.String)
	}
	setReadingAttribute(&attributes, metering.AttributeMessageUserEmail, input.messageUserEmail)

	reading, err := metering.NewUsage(metering.UsageInput{
		Meter:       metering.AgentSessionStorage(),
		Scope:       metering.ProjectScope(input.organizationID, input.projectID),
		OperationID: "chat_message:" + input.messageID.String(),
		Value:       int64(count),
		OccurredAt:  input.occurredAt,
		ProducedAt:  input.occurredAt,
		Source:      "chat_message_writer",
		Attributes:  attributes,
	})
	if err != nil {
		return nil, fmt.Errorf("create chat storage reading: %w", err)
	}
	return []metering.Reading{reading}, nil
}

// meterMessages generates storage readings independently for each row.
// Metering failures are logged and skipped so they never block message storage.
func (w *ChatMessageWriter) meterMessages(
	ctx context.Context,
	logger *slog.Logger,
	organizationID string,
	projectID uuid.UUID,
	writes []MessageWrite,
	occurredAt time.Time,
) ([]metering.Reading, error) {
	readings := make([]metering.Reading, 0, len(writes))
	for _, write := range writes {
		param := write.Params
		if param.ProjectID != projectID {
			return nil, fmt.Errorf("chat message project id does not match writer project")
		}
		rowReadings, err := w.meterMessage(ctx, meterMessageInput{
			organizationID:        organizationID,
			projectID:             projectID,
			messageID:             param.ID,
			chatID:                param.ChatID,
			content:               param.Content,
			toolCalls:             param.ToolCalls,
			model:                 param.Model,
			provider:              write.Provider,
			source:                param.Source,
			hookHostname:          write.HookHostname,
			accountType:           write.AccountType,
			billingMode:           write.BillingMode,
			billingUserID:         write.BillingUserID,
			assistantID:           write.AssistantID,
			workloadSource:        write.WorkloadSource,
			messageUserID:         param.UserID,
			messageExternalUserID: param.ExternalUserID,
			messageUserEmail:      write.UserEmail,
			occurredAt:            occurredAt,
		})
		if err != nil {
			logger.ErrorContext(ctx, "generate chat message storage reading",
				attr.SlogError(err),
				attr.SlogMessageID(param.ID.String()),
				attr.SlogOrganizationID(organizationID),
				attr.SlogProjectID(param.ProjectID.String()),
			)
			continue
		}
		readings = append(readings, rowReadings...)
	}
	return readings, nil
}

type chatProjectKey struct {
	chatID    uuid.UUID
	projectID uuid.UUID
}

func requireChatProject(ctx context.Context, db repo.DBTX, chatID uuid.UUID, projectID uuid.UUID) error {
	belongs, err := repo.New(db).ChatBelongsToProject(ctx, repo.ChatBelongsToProjectParams{
		ChatID:    chatID,
		ProjectID: projectID,
	})
	if err != nil {
		return fmt.Errorf("check chat project: %w", err)
	}
	if !belongs {
		return ErrChatNotInProject
	}
	return nil
}

func messageWriteParams(writes []MessageWrite) []repo.CreateChatMessageParams {
	params := make([]repo.CreateChatMessageParams, len(writes))
	for i := range writes {
		params[i] = writes[i].Params
	}
	return params
}

func insertChatMessages(ctx context.Context, db repo.DBTX, params []repo.CreateChatMessageParams) (int64, error) {
	if len(params) == 0 {
		return 0, nil
	}

	// Validate each distinct chat/project pair once before COPY FROM so a batch
	// cannot write a message under a project that does not own its chat.
	seen := make(map[chatProjectKey]struct{}, len(params))
	for _, param := range params {
		key := chatProjectKey{chatID: param.ChatID, projectID: param.ProjectID}
		if _, ok := seen[key]; ok {
			continue
		}
		if err := requireChatProject(ctx, db, key.chatID, key.projectID); err != nil {
			return 0, err
		}
		seen[key] = struct{}{}
	}
	n, err := repo.New(db).CreateChatMessage(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("create chat messages: %w", err)
	}
	return n, nil
}

func (w *ChatMessageWriter) writeMessages(ctx context.Context, projectID uuid.UUID, writes []MessageWrite) (int64, []repo.CreateChatMessageParams, error) {
	if len(writes) == 0 {
		return 0, nil, nil
	}

	occurredAt := time.Now().UTC()
	if err := stampMessageFields(writes, occurredAt); err != nil {
		return 0, nil, err
	}
	organizationID, err := repo.New(w.db).GetProjectOrganizationID(ctx, projectID)
	if err != nil {
		return 0, nil, fmt.Errorf("get project organization id: %w", err)
	}
	readings, err := w.meterMessages(ctx, w.logger, organizationID, projectID, writes, occurredAt)
	if err != nil {
		return 0, nil, err
	}
	params := messageWriteParams(writes)

	tx, err := w.db.Begin(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("begin chat message transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	n, err := insertChatMessages(ctx, tx, params)
	if err != nil {
		return 0, nil, err
	}
	if err := metering.Enqueue(ctx, tx, readings); err != nil {
		return 0, nil, fmt.Errorf("enqueue chat message readings: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, nil, fmt.Errorf("commit chat message transaction: %w", err)
	}
	return n, params, nil
}

// Write inserts messages via the pool and notifies observers on success.
func (w *ChatMessageWriter) Write(ctx context.Context, projectID uuid.UUID, writes []MessageWrite) (int64, error) {
	n, params, err := w.writeMessages(ctx, projectID, writes)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		w.publishTurnFrames(ctx, nil, params)
		w.notifyMessagesStored(ctx, projectID)
	}
	return n, nil
}

// WriteCorrelated atomically inserts a message or promotes an earlier LiteLLM
// observation of the same turn to the authoritative native-hook source.
func (w *ChatMessageWriter) WriteCorrelated(ctx context.Context, projectID uuid.UUID, write MessageWrite, externalMessageID string) (int64, error) {
	occurredAt := time.Now().UTC()
	writes := []MessageWrite{write}
	if err := stampMessageFields(writes, occurredAt); err != nil {
		return 0, err
	}
	write = writes[0]
	param := write.Params

	organizationID, err := repo.New(w.db).GetProjectOrganizationID(ctx, projectID)
	if err != nil {
		return 0, fmt.Errorf("get project organization id: %w", err)
	}

	tx, err := w.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin correlated chat message transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	if err := requireChatProject(ctx, tx, param.ChatID, param.ProjectID); err != nil {
		return 0, err
	}

	stored, err := repo.New(tx).UpsertCorrelatedChatMessage(ctx, repo.UpsertCorrelatedChatMessageParams{
		ID:                param.ID,
		ChatID:            param.ChatID,
		Role:              param.Role,
		ProjectID:         param.ProjectID,
		Content:           param.Content,
		ContentRaw:        param.ContentRaw,
		ContentAssetUrl:   param.ContentAssetUrl,
		StorageError:      param.StorageError,
		Model:             param.Model,
		MessageID:         param.MessageID,
		ToolCallID:        param.ToolCallID,
		UserID:            param.UserID,
		ExternalUserID:    param.ExternalUserID,
		ExternalMessageID: conv.ToPGText(externalMessageID),
		FinishReason:      param.FinishReason,
		ToolCalls:         param.ToolCalls,
		PromptTokens:      param.PromptTokens,
		CompletionTokens:  param.CompletionTokens,
		TotalTokens:       param.TotalTokens,
		Origin:            param.Origin,
		UserAgent:         param.UserAgent,
		IpAddress:         param.IpAddress,
		Source:            param.Source,
		ContentHash:       param.ContentHash,
		Generation:        param.Generation,
		Replayed:          param.Replayed,
		CreatedAt:         param.CreatedAt,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("upsert correlated chat message: %w", err)
	}
	writes[0].Params.ID = stored.ID
	writes[0].Params.Content = stored.Content
	writes[0].Params.ToolCalls = stored.ToolCalls
	writes[0].Params.Model = stored.Model
	writes[0].Params.UserID = stored.UserID
	writes[0].Params.ExternalUserID = stored.ExternalUserID
	writes[0].Params.Source = stored.Source
	readings, err := w.meterMessages(ctx, w.logger, organizationID, projectID, writes, occurredAt)
	if err != nil {
		return 0, err
	}
	if err := metering.Enqueue(ctx, tx, readings); err != nil {
		return 0, fmt.Errorf("enqueue correlated chat message reading: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit correlated chat message transaction: %w", err)
	}

	w.notifyMessagesStored(ctx, projectID)
	return 1, nil
}

// WriteExternal inserts imported provider messages idempotently and notifies
// observers when at least one new row is stored.
func (w *ChatMessageWriter) WriteExternal(ctx context.Context, projectID uuid.UUID, writes []ExternalMessageWrite) (int64, error) {
	if len(writes) == 0 {
		return 0, nil
	}

	organizationID, err := repo.New(w.db).GetProjectOrganizationID(ctx, projectID)
	if err != nil {
		return 0, fmt.Errorf("get project organization id: %w", err)
	}
	occurredAt := time.Now().UTC()
	createdAt := conv.ToPGTimestamptz(occurredAt)

	var total int64
	for i := range writes {
		write := &writes[i]
		param := &write.Params
		if param.ProjectID != projectID {
			return total, fmt.Errorf("external chat message project id does not match writer project")
		}
		if param.ID == uuid.Nil {
			param.ID, err = uuid.NewV7()
			if err != nil {
				return total, fmt.Errorf("generate external chat message id: %w", err)
			}
		}
		if !param.CreatedAt.Valid {
			param.CreatedAt = createdAt
		}
		readings, err := w.meterMessage(ctx, meterMessageInput{
			organizationID:        organizationID,
			projectID:             projectID,
			messageID:             param.ID,
			chatID:                param.ChatID,
			content:               param.Content,
			toolCalls:             param.ToolCalls,
			model:                 param.Model,
			provider:              write.Provider,
			source:                param.Source,
			hookHostname:          write.HookHostname,
			accountType:           write.AccountType,
			billingMode:           write.BillingMode,
			billingUserID:         write.BillingUserID,
			assistantID:           uuid.Nil,
			workloadSource:        write.WorkloadSource,
			messageUserID:         param.UserID,
			messageExternalUserID: param.ExternalUserID,
			messageUserEmail:      write.UserEmail,
			occurredAt:            occurredAt,
		})
		if err != nil {
			w.logger.ErrorContext(ctx, "generate external chat message storage reading",
				attr.SlogError(err),
				attr.SlogMessageID(param.ID.String()),
				attr.SlogProjectID(projectID.String()),
			)
			readings = nil
		}

		inserted, err := func() (bool, error) {
			tx, err := w.db.Begin(ctx)
			if err != nil {
				return false, fmt.Errorf("begin external chat message transaction: %w", err)
			}
			defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
			if err := requireChatProject(ctx, tx, param.ChatID, param.ProjectID); err != nil {
				return false, err
			}

			if _, err := repo.New(tx).CreateExternalChatMessage(ctx, *param); errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			} else if err != nil {
				return false, fmt.Errorf("create external chat message: %w", err)
			}
			if err := metering.Enqueue(ctx, tx, readings); err != nil {
				return false, fmt.Errorf("enqueue external chat message readings: %w", err)
			}
			if err := tx.Commit(ctx); err != nil {
				return false, fmt.Errorf("commit external chat message transaction: %w", err)
			}
			return true, nil
		}()
		if err != nil {
			return total, err
		}
		if inserted {
			total++
		}
	}
	if total > 0 {
		w.notifyMessagesStored(ctx, projectID)
	}
	return total, nil
}

// WriteInTx inserts messages via a caller-provided transaction. Observers are
// NOT fired here — the caller must invoke NotifyStored or NotifyStoredRows after
// commit so observers never see a write that ended up rolled back. Use when the
// write must be atomic with surrounding DB operations (e.g. a row-level lock
// for generation serialisation).
func (w *ChatMessageWriter) WriteInTx(ctx context.Context, tx repo.DBTX, writes []MessageWrite) (int64, error) {
	if len(writes) == 0 {
		return 0, nil
	}

	occurredAt := time.Now().UTC()
	if err := stampMessageFields(writes, occurredAt); err != nil {
		return 0, err
	}
	projectID := writes[0].Params.ProjectID
	organizationID, err := repo.New(tx).GetProjectOrganizationID(ctx, projectID)
	if err != nil {
		return 0, fmt.Errorf("get project organization id: %w", err)
	}
	readings, err := w.meterMessages(ctx, w.logger, organizationID, projectID, writes, occurredAt)
	if err != nil {
		return 0, err
	}

	n, err := insertChatMessages(ctx, tx, messageWriteParams(writes))
	if err != nil {
		return 0, err
	}
	if err := metering.Enqueue(ctx, tx, readings); err != nil {
		return 0, fmt.Errorf("enqueue chat message readings: %w", err)
	}
	return n, nil
}

// NotifyStored fans out a stored-messages signal to registered observers.
// Pair with WriteInTx: invoke after the surrounding transaction commits.
func (w *ChatMessageWriter) NotifyStored(ctx context.Context, projectID uuid.UUID) {
	w.notifyMessagesStored(ctx, projectID)
}

// NotifyStoredRows publishes rows inserted through WriteInTx after commit.
func (w *ChatMessageWriter) NotifyStoredRows(ctx context.Context, projectID uuid.UUID, writes []MessageWrite) {
	w.publishTurnFrames(ctx, nil, messageWriteParams(writes))
	w.notifyMessagesStored(ctx, projectID)
}

// WriteTurn persists a complete chat turn atomically: pending user/tool rows
// (with asset upload) and pre-built assistant rows in a single transaction.
// Observers are notified after commit if anything was stored. A partial write
// would orphan the assistant row and force divergence detection to open a new
// generation on the next turn, so atomicity is required.
func (w *ChatMessageWriter) WriteTurn(ctx context.Context, projectID uuid.UUID, pending []chatMessageRow, assistants []MessageWrite) error {
	if len(pending) == 0 && len(assistants) == 0 {
		return nil
	}

	pendingWrites, err := prepareMessages(ctx, w.logger, w.assetStorage, pending)
	if err != nil {
		return fmt.Errorf("prepare pending chat messages: %w", err)
	}
	occurredAt := time.Now().UTC()
	if err := stampMessageFields(pendingWrites, occurredAt); err != nil {
		return err
	}
	if err := stampMessageFields(assistants, occurredAt); err != nil {
		return err
	}
	organizationID, err := repo.New(w.db).GetProjectOrganizationID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("get project organization id: %w", err)
	}
	pendingReadings, err := w.meterMessages(ctx, w.logger, organizationID, projectID, pendingWrites, occurredAt)
	if err != nil {
		return err
	}
	assistantReadings, err := w.meterMessages(ctx, w.logger, organizationID, projectID, assistants, occurredAt)
	if err != nil {
		return err
	}
	readings := pendingReadings
	readings = append(readings, assistantReadings...)
	pendingParams := messageWriteParams(pendingWrites)
	assistantParams := messageWriteParams(assistants)

	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	pendingCount, err := insertChatMessages(ctx, tx, pendingParams)
	if err != nil {
		return fmt.Errorf("store pending chat messages: %w", err)
	}
	assistantCount, err := insertChatMessages(ctx, tx, assistantParams)
	if err != nil {
		return fmt.Errorf("store assistant chat messages: %w", err)
	}
	if err := metering.Enqueue(ctx, tx, readings); err != nil {
		return fmt.Errorf("enqueue chat turn readings: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	if pendingCount+assistantCount > 0 {
		// After commit: a frame announces a row that exists, so a rolled-back
		// turn never announces itself.
		w.publishTurnFrames(ctx, pending, assistantParams)
		w.notifyMessagesStored(ctx, projectID)
	}
	return nil
}

// WriteWithAssets uploads message content to asset storage, inserts the
// messages via the pool, and notifies observers on success. This is the
// full pipeline for the OpenRouter proxy path where messages carry rich
// content that needs asset storage.
func (w *ChatMessageWriter) WriteWithAssets(ctx context.Context, projectID uuid.UUID, rows []chatMessageRow) error {
	if len(rows) == 0 {
		return nil
	}
	writes, err := prepareMessages(ctx, w.logger, w.assetStorage, rows)
	if err != nil {
		return err
	}
	n, _, err := w.writeMessages(ctx, projectID, writes)
	if err != nil {
		return err
	}
	if n > 0 {
		w.publishRowFrames(ctx, rows)
		w.notifyMessagesStored(ctx, projectID)
	}
	return nil
}

// notifyMessagesStored fires all registered observers asynchronously.
func (w *ChatMessageWriter) notifyMessagesStored(ctx context.Context, projectID uuid.UUID) {
	if w == nil || len(w.observers) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		stop := context.AfterFunc(w.shutdownCtx, cancel)
		defer stop()

		w.logger.DebugContext(ctx, "notifying message observers",
			attr.SlogProjectID(projectID.String()),
			attr.SlogMessageObserverCount(len(w.observers)),
		)

		for _, obs := range w.observers {
			obs.OnMessagesStored(ctx, projectID)
		}
	}()
}
