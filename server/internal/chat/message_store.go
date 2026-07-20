package chat

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// ChatMessageWriter is the only sanctioned way to persist chat messages.
// It wraps repo.CreateChatMessage and notifies observers after a successful
// write that stored at least one message. External packages must use Write,
// WriteTurn, or WriteWithAssets.
type ChatMessageWriter struct {
	db           *pgxpool.Pool
	logger       *slog.Logger
	assetStorage assets.BlobStore
	observers    []MessageObserver
	shutdownCtx  context.Context //nolint:containedctx // must outlive any single request
	cancel       context.CancelFunc
}

func NewChatMessageWriter(logger *slog.Logger, db *pgxpool.Pool, assetStorage assets.BlobStore) (w *ChatMessageWriter, shutdown func(context.Context) error) {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:contextcheck,gosec // shutdown context must outlive any single request
	w = &ChatMessageWriter{
		db:           db,
		logger:       logger,
		assetStorage: assetStorage,
		observers:    nil,
		shutdownCtx:  ctx,
		cancel:       cancel,
	}
	shutdown = func(_ context.Context) error {
		cancel()
		return nil
	}
	return w, shutdown
}

func (w *ChatMessageWriter) AddObserver(obs MessageObserver) {
	w.observers = append(w.observers, obs)
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
// NOT fired here — the caller must invoke NotifyStored after commit so observers
// never see a write that ended up rolled back. Use when the write must be
// atomic with surrounding DB operations (e.g. a row-level lock for generation
// serialisation).
func (w *ChatMessageWriter) WriteInTx(ctx context.Context, tx repo.DBTX, params []repo.CreateChatMessageParams) (int64, error) {
	return insertChatMessages(ctx, tx, params)
}

// NotifyStored fans out a stored-messages signal to registered observers.
// Pair with WriteInTx: invoke after the surrounding transaction commits.
func (w *ChatMessageWriter) NotifyStored(ctx context.Context, projectID uuid.UUID) {
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
