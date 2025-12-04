package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type FallbackModelUsageTracking struct {
	openRouter openrouter.Provisioner
	logger     *slog.Logger
}

func NewFallbackModelUsageTracking(logger *slog.Logger, openrouter openrouter.Provisioner) *FallbackModelUsageTracking {
	return &FallbackModelUsageTracking{
		openRouter: openrouter,
		logger:     logger,
	}
}

type FallbackModelUsageTrackingArgs struct {
	GenerationID string
	OrgID        string
	ProjectID    string
	Source       billing.ModelUsageSource
	ChatID       string
}

func (f *FallbackModelUsageTracking) Do(ctx context.Context, args FallbackModelUsageTrackingArgs) error {
	if err := f.openRouter.TriggerModelUsageTracking(ctx, args.GenerationID, args.OrgID, args.ProjectID, args.Source, args.ChatID); err != nil {
		return fmt.Errorf("model usage tracking failed: %w", err)
	}
	return nil
}
