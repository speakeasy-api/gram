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
	telemetryv1 "github.com/speakeasy-api/gram/infra/gen/gram/telemetry/v1"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/stokens"
)

// ErrChatNotInProject indicates that a chat is absent or owned by another project.
var ErrChatNotInProject = errors.New("chat does not belong to project")

// MessageWrite pairs persisted native message parameters with producer-only provenance.
type MessageWrite struct {
	// Params contains the SQLc parameters persisted for the message.
	Params repo.CreateChatMessageParams

	// BillingUserID is the Speakeasy user to whom this feature explicitly allocates usage.
	BillingUserID string

	// AssistantID identifies the assistant responsible for this workload when applicable.
	AssistantID uuid.UUID

	// WorkloadSource identifies the product path responsible for this workload.
	WorkloadSource metering.WorkloadSource

	// UserEmail is the email explicitly observed for the message actor.
	UserEmail string

	// Provider preserves producer-specific AI provider attribution for metering
	// and account context. It does not by itself establish the serving route.
	Provider string

	// InferenceProvider is the reported inference service or a known first-party
	// service. Empty means unknown, independently of the account provider.
	InferenceProvider string

	// Lineage identifies transcript copies and generated history. Nil is original.
	Lineage *MessageLineage

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

	// BillingUserID is the Speakeasy user to whom this import explicitly allocates usage.
	BillingUserID string

	// WorkloadSource identifies the product path responsible for this imported workload.
	WorkloadSource metering.WorkloadSource

	// UserEmail is the email explicitly observed for the imported message actor.
	UserEmail string

	// Provider preserves producer-specific AI provider attribution for metering
	// and account context. It does not by itself establish the serving route.
	Provider string

	// InferenceProvider is the reported inference service or a known first-party
	// service. Empty means unknown, independently of the account provider.
	InferenceProvider string

	// HookHostname is the device hostname explicitly reported by hook ingestion.
	HookHostname string

	// AccountType is the AI account classification reported for the imported session.
	AccountType string

	// BillingMode is the AI account billing mode declared for the import.
	BillingMode string
}

// ChatMessageWriter is the only sanctioned way to persist chat messages.
// It atomically enqueues conversation events with persistence and notifies
// observers after a successful write. External packages must use Write,
// WriteCorrelated, WriteExternal, WriteTurn, or WriteWithAssets.
type ChatMessageWriter struct {
	db           *pgxpool.Pool
	logger       *slog.Logger
	assetStorage assets.BlobStore
	stokenCodec  *stokens.Codec
	observers    []MessageObserver
	// transcripts seals encrypted copies for projects with encrypted
	// transcripts. Nil seals none.
	transcripts *transcriptstore.Transcripts
	// turnStream, when set, receives a frame per persisted row so dashboard
	// subscribers can render a turn without polling. Nil disables publishing.
	turnStream  *TurnStream
	shutdownCtx context.Context //nolint:containedctx // must outlive any single request
	cancel      context.CancelFunc
}

// NewChatMessageWriter builds the writer every chat message write goes
// through. transcripts decides which projects' messages are sealed in the same
// transaction as the write; build it with configureTranscripts before the
// writer. Nil seals nothing, so every project is written plaintext only: pass
// it only where no project can have encrypted transcripts.
func NewChatMessageWriter(logger *slog.Logger, db *pgxpool.Pool, assetStorage assets.BlobStore, transcripts *transcriptstore.Transcripts) (w *ChatMessageWriter, shutdown func(context.Context) error) {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:contextcheck // shutdown context must outlive any single request
	w = &ChatMessageWriter{
		db:           db,
		logger:       logger.With(attr.SlogComponent("chat-message-writer")),
		assetStorage: assetStorage,
		stokenCodec:  stokens.NewCodec(),
		observers:    nil,
		transcripts:  transcripts,
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

// InTx runs fn in a new transaction and commits it. Any write in fn that
// stores or changes a message's protected fields (WriteInTx,
// UpdateChatMessage) must run in it: it loads the project's transcript key
// before the transaction begins, so sealing makes no KMS call while the
// transaction is open, and a key that will not load fails the call before
// anything is written.
func (w *ChatMessageWriter) InTx(ctx context.Context, projectID uuid.UUID, fn func(pgx.Tx) error) error {
	organizationID := ""
	if w.transcripts.MaySeal(ctx, projectID) {
		id, err := repo.New(w.db).GetProjectOrganizationID(ctx, projectID)
		if err != nil {
			return fmt.Errorf("get project organization id: %w", err)
		}
		organizationID = id
	}
	return w.inTx(ctx, organizationID, projectID, fn)
}

func (w *ChatMessageWriter) inTx(ctx context.Context, organizationID string, projectID uuid.UUID, fn func(pgx.Tx) error) error {
	//nolint:wrapcheck // InTx wraps its own errors, and fn's are the caller's.
	return w.transcripts.InTx(ctx, w.db, organizationID, projectID, fn)
}

// UpdateChatMessage changes an existing message through
// Transcripts.UpdateChatMessage, inside the caller's transaction, so every copy
// of the row the project keeps gets the same value. apply receives the whole
// row. Its protected fields come from the decrypted transcript in encrypted
// mode, and in shadow mode when the transcript agrees with the plaintext;
// otherwise they are the plaintext columns. Besides the
// protected fields it may change only tool_outcome, finish_reason,
// content_hash and risk_analyzed_at; any other change fails. Run it in InTx.
// A nil writer updates the plaintext only.
func (w *ChatMessageWriter) UpdateChatMessage(ctx context.Context, db repo.DBTX, organizationID string, projectID, chatID, messageID uuid.UUID, apply func(*repo.ChatMessage) error) (bool, error) {
	var transcripts *transcriptstore.Transcripts
	if w != nil {
		transcripts = w.transcripts
	}
	//nolint:wrapcheck // UpdateChatMessage already wraps with the message context.
	return transcripts.UpdateChatMessage(ctx, db, organizationID, projectID, chatID, messageID, apply)
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

func (w *ChatMessageWriter) insertChatMessages(ctx context.Context, db repo.DBTX, organizationID string, projectID uuid.UUID, params []repo.CreateChatMessageParams) (int64, error) {
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
	n, err := w.transcripts.InsertChatMessages(ctx, db, organizationID, projectID, params)
	if err != nil {
		return 0, fmt.Errorf("insert chat messages: %w", err)
	}
	for _, param := range params {
		if err := persistClaudeTagMetadata(ctx, db, param); err != nil {
			return 0, err
		}
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

	var n int64
	err = w.inTx(ctx, organizationID, projectID, func(tx pgx.Tx) error {
		n, err = w.insertChatMessages(ctx, tx, organizationID, projectID, params)
		if err != nil {
			return err
		}
		if err := metering.Enqueue(ctx, tx, readings); err != nil {
			return fmt.Errorf("enqueue chat message readings: %w", err)
		}
		return w.enqueueMessageEvents(ctx, tx, organizationID, projectID, writes, occurredAt)
	})
	if err != nil {
		return 0, nil, fmt.Errorf("write chat messages: %w", err)
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
// Only initial storage emits usage. Promotion preserves the original usage fact.
// Only insertion publishes a creation event; promotion changes metadata.
func (w *ChatMessageWriter) WriteCorrelated(ctx context.Context, projectID uuid.UUID, write MessageWrite, externalMessageID string) (int64, error) {
	if write.Params.ProjectID != projectID {
		return 0, fmt.Errorf("chat message project id does not match writer project")
	}

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

	upsert := repo.UpsertCorrelatedChatMessageParams{
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
	}
	found := false
	err = w.inTx(ctx, organizationID, projectID, func(tx pgx.Tx) error {
		if err := requireChatProject(ctx, tx, param.ChatID, param.ProjectID); err != nil {
			return err
		}
		stored, ok, err := w.transcripts.UpsertCorrelatedChatMessage(ctx, tx, organizationID, projectID, upsert)
		if err != nil {
			return fmt.Errorf("upsert correlated chat message: %w", err)
		}
		if !ok {
			return nil
		}
		found = true
		param.ID = stored.ID
		param.Content = stored.Content
		param.Source = stored.Source
		param.UserAgent = stored.UserAgent
		if err := persistClaudeTagMetadata(ctx, tx, param); err != nil {
			return err
		}
		// A correlated promotion only updates attribution/source metadata on an
		// existing message; its content is unchanged and its insertion already
		// published a creation event. Do not publish again and trigger duplicate
		// evaluation solely because that metadata was enriched.
		if !stored.Inserted {
			return nil
		}
		writes[0].Params.ID = stored.ID
		metered := writes[0]
		metered.Params.Content = stored.Content
		metered.Params.ToolCalls = stored.ToolCalls
		metered.Params.Model = stored.Model
		metered.Params.UserID = stored.UserID
		metered.Params.ExternalUserID = stored.ExternalUserID
		metered.Params.Source = stored.Source
		readings, err := w.meterMessages(ctx, w.logger, organizationID, projectID, []MessageWrite{metered}, occurredAt)
		if err != nil {
			return err
		}
		if err := metering.Enqueue(ctx, tx, readings); err != nil {
			return fmt.Errorf("enqueue correlated chat message reading: %w", err)
		}
		return w.enqueueMessageEvents(ctx, tx, organizationID, projectID, writes, occurredAt)
	})
	if err != nil {
		return 0, fmt.Errorf("write correlated chat message: %w", err)
	}
	if !found {
		return 0, nil
	}

	w.notifyMessagesStored(ctx, projectID)
	return 1, nil
}

// jsonNULEscape is the JSON spelling of U+0000, which jsonb rejects.
var jsonNULEscape = []byte(`\u` + "0000")

// WriteExternal inserts imported provider messages idempotently and notifies
// observers when at least one new row is stored.
func (w *ChatMessageWriter) WriteExternal(ctx context.Context, projectID uuid.UUID, writes []ExternalMessageWrite) (int64, error) {
	return w.WriteExternalWithContentParts(ctx, projectID, writes, nil)
}

// WriteExternalWithContentParts commits each imported message and its content
// parts together. Parts are keyed by the parent's explicit message ID. A retry
// that conflicts with an existing message also skips its parts and metering.
func (w *ChatMessageWriter) WriteExternalWithContentParts(ctx context.Context, projectID uuid.UUID, writes []ExternalMessageWrite, parts map[uuid.UUID][]repo.CreateChatContentPartParams) (int64, error) {
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
		// Postgres rejects NUL in text and jsonb. Strip it from the text columns;
		// the raw copy is optional and readers fall back to content, so drop it.
		param.Content = conv.StripNUL(param.Content)
		param.ExternalUserID.String = conv.StripNUL(param.ExternalUserID.String)
		param.ExternalMessageID.String = conv.StripNUL(param.ExternalMessageID.String)
		param.UserAgent.String = conv.StripNUL(param.UserAgent.String)
		if bytes.Contains(param.ContentRaw, jsonNULEscape) || bytes.IndexByte(param.ContentRaw, 0) >= 0 {
			param.ContentRaw = nil
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

		publicationWrites := []MessageWrite{externalPublication(*write)}
		inserted := false
		err = w.inTx(ctx, organizationID, projectID, func(tx pgx.Tx) error {
			if err := requireChatProject(ctx, tx, param.ChatID, param.ProjectID); err != nil {
				return err
			}
			created, err := w.transcripts.InsertExternalChatMessage(ctx, tx, organizationID, projectID, *param)
			if err != nil {
				return fmt.Errorf("insert external chat message: %w", err)
			}
			if !created {
				return nil
			}
			if attached := parts[param.ID]; len(attached) > 0 {
				for _, part := range attached {
					if part.ProjectID != projectID || part.ChatID != param.ChatID || !part.ParentChatMessageID.Valid || part.ParentChatMessageID.UUID != param.ID {
						return fmt.Errorf("external content part does not match its parent message")
					}
				}
				if _, err := repo.New(tx).CreateChatContentPart(ctx, attached); err != nil {
					return fmt.Errorf("create external message content parts: %w", err)
				}
			}
			if err := metering.Enqueue(ctx, tx, readings); err != nil {
				return fmt.Errorf("enqueue external chat message readings: %w", err)
			}
			if _, err := outbox.Publish(ctx, tx, organizationID, outbox.Message{Proto: telemetryv1.SessionObserved_builder{
				ProjectId:    new(projectID.String()),
				MessageId:    new(param.ID.String()),
				Provider:     new(write.Provider),
				HookHostname: new(write.HookHostname),
				AccountType:  new(write.AccountType),
				BillingMode:  new(write.BillingMode),
			}.Build(), PublicID: uuid.Nil, Attributes: nil}); err != nil {
				return fmt.Errorf("enqueue imported session observation: %w", err)
			}
			if err := w.enqueueMessageEvents(ctx, tx, organizationID, projectID, publicationWrites, occurredAt); err != nil {
				return err
			}
			inserted = true
			return nil
		})
		if err != nil {
			return total, fmt.Errorf("write external chat message: %w", err)
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

// WriteInTx inserts messages via a caller-provided transaction, which must come
// from InTx so the transcript key is loaded. Observers are
// NOT fired here — the caller must invoke NotifyStored or NotifyStoredRows after
// commit so observers never see a write that ended up rolled back. Use when the
// write must be atomic with surrounding DB operations (e.g. a row-level lock
// for generation serialisation).
// Publication carries references only and never uploads assets.
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

	n, err := w.insertChatMessages(ctx, tx, organizationID, projectID, messageWriteParams(writes))
	if err != nil {
		return 0, err
	}
	if err := metering.Enqueue(ctx, tx, readings); err != nil {
		return 0, fmt.Errorf("enqueue chat message readings: %w", err)
	}
	if err := w.enqueueMessageEvents(ctx, tx, organizationID, projectID, writes, occurredAt); err != nil {
		return 0, err
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
	writes := pendingWrites
	writes = append(writes, assistants...)

	var pendingCount, assistantCount int64
	err = w.inTx(ctx, organizationID, projectID, func(tx pgx.Tx) error {
		pendingCount, err = w.insertChatMessages(ctx, tx, organizationID, projectID, pendingParams)
		if err != nil {
			return fmt.Errorf("store pending chat messages: %w", err)
		}
		assistantCount, err = w.insertChatMessages(ctx, tx, organizationID, projectID, assistantParams)
		if err != nil {
			return fmt.Errorf("store assistant chat messages: %w", err)
		}
		if err := metering.Enqueue(ctx, tx, readings); err != nil {
			return fmt.Errorf("enqueue chat turn readings: %w", err)
		}
		return w.enqueueMessageEvents(ctx, tx, organizationID, projectID, writes, occurredAt)
	})
	if err != nil {
		return fmt.Errorf("write chat turn: %w", err)
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
