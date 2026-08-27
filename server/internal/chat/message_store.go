package chat

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"path"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// ChatMessageWriter is the only sanctioned way to persist chat messages.
// It wraps repo.CreateChatMessage and notifies observers after a successful
// write that stored at least one message. External packages must use Write,
// WriteCorrelated, WriteTurn, or WriteWithAssets.
type ChatMessageWriter struct {
	db           *pgxpool.Pool
	logger       *slog.Logger
	assetStorage assets.BlobStore
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
		logger:       logger,
		assetStorage: assetStorage,
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

// stampUnsetCreatedAt fills rows that carry no explicit created_at with one
// shared write-time value. Sharing a single timestamp per batch makes rows
// tie on created_at so seq (insertion order) decides — exactly the pre-DNO-536
// ordering for playground/assistant writers, whose rows may have been
// CONSTRUCTED out of order relative to their intended position. Hook ingest
// sets created_at explicitly to the event's occurred_at and is left alone.
func stampUnsetCreatedAt(params []repo.CreateChatMessageParams) {
	now := conv.ToPGTimestamptz(time.Now())
	for i := range params {
		if !params[i].CreatedAt.Valid {
			params[i].CreatedAt = now
		}
	}
}

// insertChatMessages is the single chokepoint between CreateChatMessageParams
// and the copyfrom insert. created_at is in the COPY column list, so the DB
// default can never apply — an unstamped row would insert NULL into a NOT
// NULL column and fail the whole batch. Every insert routes through here so
// no call site can forget the stamp.
func insertChatMessages(ctx context.Context, db repo.DBTX, params []repo.CreateChatMessageParams) (int64, error) {
	stampUnsetCreatedAt(params)
	n, err := repo.New(db).CreateChatMessage(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("create chat messages: %w", err)
	}
	return n, nil
}

// Write inserts messages via the pool and notifies observers on success.
func (w *ChatMessageWriter) Write(ctx context.Context, projectID uuid.UUID, params []repo.CreateChatMessageParams) (int64, error) {
	n, err := insertChatMessages(ctx, w.db, params)
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
func (w *ChatMessageWriter) WriteCorrelated(ctx context.Context, projectID uuid.UUID, param repo.CreateChatMessageParams, externalMessageID string) (int64, error) {
	params := []repo.CreateChatMessageParams{param}
	stampUnsetCreatedAt(params)
	param = params[0]

	n, err := repo.New(w.db).UpsertCorrelatedChatMessage(ctx, repo.UpsertCorrelatedChatMessageParams{
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
	if err != nil {
		return 0, fmt.Errorf("upsert correlated chat message: %w", err)
	}
	if n > 0 {
		w.notifyMessagesStored(ctx, projectID)
	}
	return n, nil
}

// WriteExternal inserts imported provider messages idempotently and notifies
// observers when at least one new row is stored.
func (w *ChatMessageWriter) WriteExternal(ctx context.Context, projectID uuid.UUID, params []repo.CreateExternalChatMessageParams) (int64, error) {
	q := repo.New(w.db)
	var total int64
	for _, param := range params {
		n, err := q.CreateExternalChatMessage(ctx, param)
		if err != nil {
			return total, fmt.Errorf("create external chat message: %w", err)
		}
		total += n
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
func (w *ChatMessageWriter) WriteInTx(ctx context.Context, tx repo.DBTX, params []repo.CreateChatMessageParams) (int64, error) {
	return insertChatMessages(ctx, tx, params)
}

// NotifyStored fans out a stored-messages signal to registered observers.
// Pair with WriteInTx: invoke after the surrounding transaction commits.
func (w *ChatMessageWriter) NotifyStored(ctx context.Context, projectID uuid.UUID) {
	w.notifyMessagesStored(ctx, projectID)
}

// NotifyStoredRows publishes rows inserted through WriteInTx after commit.
func (w *ChatMessageWriter) NotifyStoredRows(ctx context.Context, projectID uuid.UUID, params []repo.CreateChatMessageParams) {
	w.publishTurnFrames(ctx, nil, params)
	w.notifyMessagesStored(ctx, projectID)
}

// WriteTurn persists a complete chat turn atomically: pending user/tool rows
// (with asset upload) and pre-built assistant rows in a single transaction.
// Observers are notified after commit if anything was stored. A partial write
// would orphan the assistant row and force divergence detection to open a new
// generation on the next turn, so atomicity is required.
func (w *ChatMessageWriter) WriteTurn(ctx context.Context, projectID uuid.UUID, pending []chatMessageRow, assistants []repo.CreateChatMessageParams) error {
	if len(pending) == 0 && len(assistants) == 0 {
		return nil
	}

	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	if err := w.storeMessages(ctx, tx, pending); err != nil {
		return fmt.Errorf("store pending chat messages: %w", err)
	}

	n, err := insertChatMessages(ctx, tx, assistants)
	if err != nil {
		return fmt.Errorf("store assistant chat messages: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	if int64(len(pending))+n > 0 {
		// After commit: a frame announces a row that exists, so a rolled-back
		// turn never announces itself.
		w.publishTurnFrames(ctx, pending, assistants)
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
	if err := w.storeMessages(ctx, w.db, rows); err != nil {
		return err
	}
	w.publishRowFrames(ctx, rows)
	w.notifyMessagesStored(ctx, projectID)
	return nil
}

// storeMessages uploads message content to asset storage in parallel, then
// batch-inserts the messages via the given DBTX. Used by WriteWithAssets
// (with the pool) and WriteTurn (with a transaction).
func (w *ChatMessageWriter) storeMessages(ctx context.Context, tx repo.DBTX, rows []chatMessageRow) error {
	return storeMessages(ctx, w.logger, tx, w.assetStorage, rows)
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
