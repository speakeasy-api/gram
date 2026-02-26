package activities

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type FallbackModelUsageTracking struct {
	usageTracker openrouter.UsageTrackingStrategy
}

func NewFallbackModelUsageTracking(usageTracker openrouter.UsageTrackingStrategy) *FallbackModelUsageTracking {
	return &FallbackModelUsageTracking{
		usageTracker: usageTracker,
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
	err := f.usageTracker.TrackUsage(ctx, args.GenerationID, args.OrgID, args.ProjectID, args.Source, args.ChatID)
	if err != nil {
		return fmt.Errorf("track usage: %w", err)
	}
	return nil
}
