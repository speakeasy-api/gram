package chat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// FallbackModelUsageTracker schedules fallback model usage tracking when inline tracking fails.
type FallbackModelUsageTracker interface {
	ScheduleFallbackModelUsageTracking(ctx context.Context, generationID, orgID, projectID string, source billing.ModelUsageSource, chatID string) error
}

// DefaultUsageTrackingStrategy implements usage tracking with fallback support.
// It tries to track usage immediately, and schedules a fallback if the initial attempt fails.
type DefaultUsageTrackingStrategy struct {
	logger          *slog.Logger
	provisioner     openrouter.Provisioner
	fallbackTracker FallbackModelUsageTracker
}

// NewDefaultUsageTrackingStrategy creates a new DefaultUsageTrackingStrategy.
func NewDefaultUsageTrackingStrategy(
	logger *slog.Logger,
	provisioner openrouter.Provisioner,
	fallbackTracker FallbackModelUsageTracker,
) *DefaultUsageTrackingStrategy {
	return &DefaultUsageTrackingStrategy{
		logger:          logger,
		provisioner:     provisioner,
		fallbackTracker: fallbackTracker,
	}
}

// TrackUsage attempts to track model usage immediately, with fallback support.
func (s *DefaultUsageTrackingStrategy) TrackUsage(
	ctx context.Context,
	generationID, orgID, projectID string,
	source billing.ModelUsageSource,
	chatID string,
) error {
	// Try to track usage via TriggerModelUsageTracking
	err := s.provisioner.TriggerModelUsageTracking(ctx, generationID, orgID, projectID, source, chatID)
	if err != nil {
		// Check if generation not found (404)
		if errors.Is(err, openrouter.ErrGenerationNotFound) {
			s.logger.WarnContext(ctx, "generation not found, scheduling fallback tracking",
				attr.SlogError(err),
				attr.SlogOrganizationID(orgID),
			)

			// Schedule fallback tracking
			if s.fallbackTracker != nil {
				if scheduleErr := s.fallbackTracker.ScheduleFallbackModelUsageTracking(
					ctx,
					generationID,
					orgID,
					projectID,
					source,
					chatID,
				); scheduleErr != nil {
					s.logger.ErrorContext(ctx, "failed to schedule fallback model usage tracking",
						attr.SlogError(scheduleErr),
						attr.SlogOrganizationID(orgID),
					)
					return fmt.Errorf("schedule fallback model usage tracking: %w", scheduleErr)
				}
			}
		} else {
			// Other errors
			s.logger.ErrorContext(ctx, "failed to track model usage",
				attr.SlogError(err),
				attr.SlogOrganizationID(orgID),
			)
			return fmt.Errorf("track model usage: %w", err)
		}
	}

	return nil
}

// NoOpUsageTrackingStrategy is a usage tracking strategy that does nothing.
// It's useful for tests where usage tracking is not needed.
type NoOpUsageTrackingStrategy struct{}

// NewNoOpUsageTrackingStrategy creates a new NoOpUsageTrackingStrategy.
func NewNoOpUsageTrackingStrategy() *NoOpUsageTrackingStrategy {
	return &NoOpUsageTrackingStrategy{}
}

// TrackUsage does nothing and always returns nil.
func (s *NoOpUsageTrackingStrategy) TrackUsage(
	_ context.Context,
	_, _, _ string,
	_ billing.ModelUsageSource,
	_ string,
) error {
	return nil
}
