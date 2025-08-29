package billing

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

type NoopTracker struct {
	logger *slog.Logger
}

func NewNoopTracker(logger *slog.Logger) *NoopTracker {
	if logger == nil {
		logger = slog.Default()
	}
	return &NoopTracker{
		logger: logger.With(attr.SlogComponent("billing")),
	}
}

var _ Tracker = (*NoopTracker)(nil)

func (t *NoopTracker) TrackPlatformUsage(ctx context.Context, event PlatformUsageEvent) {
	t.logger.DebugContext(ctx, "track platform usage", attr.SlogValueAny(event))
}

func (t *NoopTracker) TrackPromptCallUsage(ctx context.Context, event PromptCallUsageEvent) {
	t.logger.DebugContext(ctx, "track prompt call usage", attr.SlogValueAny(event))
}

func (t *NoopTracker) TrackToolCallUsage(ctx context.Context, event ToolCallUsageEvent) {
	t.logger.DebugContext(ctx, "track tool call usage", attr.SlogValueAny(event))
}
