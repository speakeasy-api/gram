package plugins

import (
	"context"

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

// signalPublish enqueues a republish for the project whose plugins just
// changed. Best-effort: a failed enqueue is logged and never fails the request,
// since the rollout sweep still picks the project up on its next tick. Must
// only be called after the triggering transaction has committed — the publish
// reads the project's live state, which a later rollback would take back.
func (s *Service) signalPublish(ctx context.Context, projectID uuid.UUID, createdByUserID string) {
	if s.publisher == nil || s.github == nil {
		return
	}

	// The request returning shouldn't drop the enqueue.
	if err := s.publisher.SignalPluginPublish(context.WithoutCancel(ctx), projectID, createdByUserID); err != nil {
		s.logger.WarnContext(ctx, "failed to signal plugin publish",
			attr.SlogProjectID(projectID.String()), attr.SlogError(err))
	}
}
