package chat

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
)

// ChatMessageWriter is the only sanctioned way to persist chat messages.
// It wraps repo.CreateChatMessage and ensures observers are always notified
// after a successful write.
//
// External packages must use Write or RunInTx. The unexported
// notifyMessagesStored method is available to the chat package's own
// internal helpers (e.g. storeMessages) where the insert and notification
// are managed separately.
type ChatMessageWriter struct {
	db          *pgxpool.Pool
	logger      *slog.Logger
	observers   []MessageObserver
	shutdownCtx context.Context //nolint:containedctx // must outlive any single request
	cancel      context.CancelFunc
}

func NewChatMessageWriter(logger *slog.Logger, db *pgxpool.Pool) (w *ChatMessageWriter, shutdown func(context.Context) error) {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:contextcheck,gosec
	w = &ChatMessageWriter{
		db:          db,
		logger:      logger,
		shutdownCtx: ctx,
		cancel:      cancel,
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
		return 0, err
	}
	w.notifyMessagesStored(ctx, projectID)
	return n, nil
}

// RunInTx runs fn inside a transaction. If fn succeeds and the transaction
// commits, observers are notified. The caller cannot forget notification
// because it is handled by RunInTx itself.
func (w *ChatMessageWriter) RunInTx(ctx context.Context, projectID uuid.UUID, fn func(tx pgx.Tx) error) error {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	w.notifyMessagesStored(ctx, projectID)
	return nil
}

// notifyMessagesStored fires all registered observers asynchronously.
// Unexported so that external packages are forced through Write or RunInTx.
// The chat package uses this directly only when the insert is performed by
// a private helper (e.g. storeMessages) where Write/RunInTx cannot be used.
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
