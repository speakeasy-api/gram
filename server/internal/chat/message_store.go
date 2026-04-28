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

// Write inserts messages via the pool and notifies observers on success.
func (w *ChatMessageWriter) Write(ctx context.Context, projectID uuid.UUID, params []repo.CreateChatMessageParams) (int64, error) {
	n, err := repo.New(w.db).CreateChatMessage(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("create chat messages: %w", err)
	}
	if n > 0 {
		w.notifyMessagesStored(ctx, projectID)
	}
	return n, nil
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
	defer func() { _ = tx.Rollback(ctx) }()

	if err := w.storeMessages(ctx, tx, pending); err != nil {
		return fmt.Errorf("store pending chat messages: %w", err)
	}

	n, err := repo.New(tx).CreateChatMessage(ctx, assistants)
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
// (with the pool) and inside RunInTx callbacks (with a transaction).
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
