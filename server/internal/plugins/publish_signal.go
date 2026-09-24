package plugins

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// PluginPublishSignaler enqueues a republish of a project's marketplace
// packages. Plugin state changes (a new plugin, a server or skill added to
// one) change what a publish would generate, so every mutation signals its own
// project instead of waiting for the periodic rollout sweep to notice.
//
// Implemented by background.TemporalPluginPublisher. The signal is debounced
// per project, so a burst of changes collapses into one publish.
type PluginPublishSignaler interface {
	SignalPluginPublish(ctx context.Context, projectID uuid.UUID, createdByUserID string) error
}

// SignalPluginPublishAfterRequest schedules the legacy publish signal after the
// transaction that requested publication has committed. A durable outbox request
// already covers the publication, so it must not be signalled again. When the
// request was disabled or no marketplace was configured, preserve the legacy
// signal path. A nil signaler is a no-op.
func SignalPluginPublishAfterRequest(ctx context.Context, signaler PluginPublishSignaler, outcome ProjectPublicationRequestOutcome, projectID uuid.UUID, createdByUserID string) error {
	if signaler == nil || outcome == ProjectPublicationEnqueued {
		return nil
	}
	if err := signaler.SignalPluginPublish(context.WithoutCancel(ctx), projectID, createdByUserID); err != nil {
		return fmt.Errorf("signal plugin publication: %w", err)
	}
	return nil
}

// signalPublish enqueues a republish for the project whose plugins just
// changed. Best-effort: a failed enqueue is logged and never fails the request,
// since the rollout sweep still picks the project up on its next tick. Must
// only be called after the triggering transaction has committed — the publish
// reads the project's live state, which a later rollback would take back.
func (s *Service) signalPublish(ctx context.Context, projectID uuid.UUID, createdByUserID string) {
	if s.publisher == nil || s.github == nil {
		return
	}
	// Keep the direct signal while emission and consumption are independently
	// gated. Rechecking the connection after commit cannot prove that this
	// transaction enqueued a request, and duplicate signals are debounced.
	if err := s.publisher.SignalPluginPublish(context.WithoutCancel(ctx), projectID, createdByUserID); err != nil {
		s.logger.WarnContext(ctx, "failed to signal plugin publish",
			attr.SlogProjectID(projectID.String()), attr.SlogError(err))
	}
}
