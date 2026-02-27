package chat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
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
	tracking        billing.Tracker
	fallbackTracker FallbackModelUsageTracker
	orgRepo         *orgRepo.Queries
}

var _ openrouter.UsageTrackingStrategy = (*DefaultUsageTrackingStrategy)(nil)

// NewDefaultUsageTrackingStrategy creates a new DefaultUsageTrackingStrategy.
func NewDefaultUsageTrackingStrategy(
	db *pgxpool.Pool,
	logger *slog.Logger,
	provisioner openrouter.Provisioner,
	tracking billing.Tracker,
	fallbackTracker FallbackModelUsageTracker,
) *DefaultUsageTrackingStrategy {
	orgRepo := orgRepo.New(db)

	return &DefaultUsageTrackingStrategy{
		logger:          logger,
		provisioner:     provisioner,
		tracking:        tracking,
		fallbackTracker: fallbackTracker,
		orgRepo:         orgRepo,
	}
}

// TrackUsage attempts to track model usage immediately, with fallback support.
func (s *DefaultUsageTrackingStrategy) TrackUsage(
	ctx context.Context,
	generationID, orgID, projectID string,
	source billing.ModelUsageSource,
	chatID string,
) error {
	usage, err := s.provisioner.GetModelUsage(ctx, generationID, orgID)
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

	// This (hopefully) means we scheduled fallback tracking above
	if usage == nil {
		return nil
	}

	org, err := s.orgRepo.GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return fmt.Errorf("get organization: %w", err)
	}

	event := billing.ModelUsageEvent{
		OrganizationSlug:      org.Slug,
		OrganizationID:        orgID,
		ProjectID:             projectID,
		Source:                source,
		ChatID:                chatID,
		Model:                 usage.Model,
		InputTokens:           int64(usage.TokensPrompt),
		OutputTokens:          int64(usage.TokensCompletion),
		TotalTokens:           int64(usage.TokensPrompt + usage.TokensCompletion),
		Cost:                  usage.TotalCost,
		NativeTokensCached:    int64(usage.NativeTokensCached),
		NativeTokensReasoning: int64(usage.NativeTokensReasoning),
		CacheDiscount:         usage.CacheDiscount,
		UpstreamInferenceCost: usage.UpstreamInferenceCost,
	}

	s.tracking.TrackModelUsage(ctx, event)

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
