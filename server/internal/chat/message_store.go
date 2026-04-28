package chat

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// MessageStore is the central registry for MessageObservers. All code paths
// that store chat messages call NotifyMessagesStored after a successful insert
// so that observers (e.g. risk analysis) are notified exactly once, regardless
// of which subsystem persisted the message.
type MessageStore struct {
	logger      *slog.Logger
	observers   []MessageObserver
	shutdownCtx context.Context //nolint:containedctx // must outlive any single request
	cancel      context.CancelFunc
}

func NewMessageStore(logger *slog.Logger) (store *MessageStore, shutdown func(context.Context) error) {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:contextcheck,gosec
	store = &MessageStore{
		logger:      logger,
		shutdownCtx: ctx,
		cancel:      cancel,
	}
	shutdown = func(_ context.Context) error {
		cancel()
		return nil
	}
	return store, shutdown
}

func (s *MessageStore) AddObserver(obs MessageObserver) {
	s.observers = append(s.observers, obs)
}

// NotifyMessagesStored fires all registered observers asynchronously.
// Call this after chat messages have been durably stored (committed).
func (s *MessageStore) NotifyMessagesStored(ctx context.Context, projectID uuid.UUID) {
	if s == nil || len(s.observers) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		stop := context.AfterFunc(s.shutdownCtx, cancel)
		defer stop()

		s.logger.DebugContext(ctx, "notifying message observers",
			attr.SlogProjectID(projectID.String()),
			attr.SlogMessageObserverCount(len(s.observers)),
		)

		for _, obs := range s.observers {
			obs.OnMessagesStored(ctx, projectID)
		}
	}()
}
