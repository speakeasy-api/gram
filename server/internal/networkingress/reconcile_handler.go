package networkingress

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	networkingressv1 "github.com/speakeasy-api/gram/infra/gen/gram/networkingress/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

// ReconcileHandler coalesces durable wakes by ingress before contacting Temporal.
type ReconcileHandler struct {
	logger *slog.Logger
	queue  string
	signal func(context.Context, uuid.UUID) error
}

func NewReconcileHandler(logger *slog.Logger, queue string, signal func(context.Context, uuid.UUID) error) *ReconcileHandler {
	return &ReconcileHandler{logger: logger, queue: queue, signal: signal}
}

func (h *ReconcileHandler) HandleBatchWithResult(ctx context.Context, messages []streams.BatchMessage[*networkingressv1.ReconcileRequested]) error {
	results := make(map[uuid.UUID]error)
	for _, message := range messages {
		// A mismatched queue can be a stale preview message. It cannot select
		// a worker; the authoritative sweep redrives the database row.
		id, err := uuid.Parse(message.Message.GetIngressId())
		if err != nil || id == uuid.Nil || message.Message.GetTemporalTaskQueue() != h.queue || h.queue == "" {
			h.logger.WarnContext(ctx, "discard invalid network ingress reconciliation request", attr.SlogError(fmt.Errorf("invalid ingress id or reconciliation queue")))
			continue
		}
		err, seen := results[id]
		if !seen {
			err = h.signal(ctx, id)
			results[id] = err
		}
		if err != nil {
			message.Fail(err)
		}
	}
	return nil
}
