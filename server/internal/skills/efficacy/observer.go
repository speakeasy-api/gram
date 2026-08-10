package efficacy

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat"
)

// Signaler wakes a project's skill efficacy coordinator. Implementations are
// expected to be idempotent: every producer signals on each durable write, so
// the same project is woken many times over one session and a wake carries no
// payload beyond the project it names.
//
// Declared here rather than imported from the workflow layer so the producers
// — the chat observer below, the hook ingest paths — depend on the efficacy
// domain and never on the background package that runs the coordinator.
type Signaler interface {
	Signal(ctx context.Context, projectID uuid.UUID) error
}

// observer wakes the coordinator when a project stores new chat messages. A
// session becomes scoreable only once its transcript has gone quiet, so the
// transcript write is the event that can make an already-queued activation
// eligible — and the one that eventually stops arriving.
type observer struct {
	logger   *slog.Logger
	signaler Signaler
}

var _ chat.MessageObserver = (*observer)(nil)

// NewObserver builds the chat.MessageObserver that turns durable chat-message
// persistence into an efficacy wake. Register it on the chat message writer.
func NewObserver(logger *slog.Logger, signaler Signaler) chat.MessageObserver {
	return &observer{
		logger:   logger.With(attr.SlogComponent("skill-efficacy")),
		signaler: signaler,
	}
}

// OnMessagesStored implements chat.MessageObserver. The writer only calls this
// after a write that durably stored at least one row, and it dispatches on its
// own goroutine with a detached context, so blocking work is safe here. A wake
// that cannot be delivered is logged and dropped: the persistence path must not
// fail because the coordinator is unreachable, and the next write — or the
// coordinator's own periodic walk — recovers the queue.
func (o *observer) OnMessagesStored(ctx context.Context, projectID uuid.UUID) {
	if err := o.signaler.Signal(ctx, projectID); err != nil {
		o.logger.ErrorContext(ctx, "signal skill efficacy coordinator on stored messages",
			attr.SlogError(err),
			attr.SlogProjectID(projectID.String()),
		)
	}
}
