package chat

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// DefaultUsageTrackingStrategy emits billing events from an already-decoded
// ModelUsage. OpenRouter returns the full usage payload inline on every chat
// completion, so no out-of-band lookup is required.
type DefaultUsageTrackingStrategy struct {
	logger   *slog.Logger
	tracking billing.Tracker
	orgRepo  *orgRepo.Queries
}

var _ openrouter.UsageTrackingStrategy = (*DefaultUsageTrackingStrategy)(nil)

// NewDefaultUsageTrackingStrategy creates a new DefaultUsageTrackingStrategy.
func NewDefaultUsageTrackingStrategy(
	db *pgxpool.Pool,
	logger *slog.Logger,
	tracking billing.Tracker,
) *DefaultUsageTrackingStrategy {
	orgRepo := orgRepo.New(db)

	return &DefaultUsageTrackingStrategy{
		logger:   logger,
		tracking: tracking,
		orgRepo:  orgRepo,
	}
}

// TrackUsage emits a billing event for an inline ModelUsage payload.
func (s *DefaultUsageTrackingStrategy) TrackUsage(
	ctx context.Context,
	usage *openrouter.ModelUsage,
	orgID, projectID string,
	source billing.ModelUsageSource,
	chatID string,
) error {
	if usage == nil {
		return nil
	}

	org, err := s.orgRepo.GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return fmt.Errorf("get organization: %w", err)
	}

	if usage.TotalCost == nil {
		s.logger.WarnContext(ctx, "openrouter response carried no cost",
			attr.SlogOrganizationID(orgID),
		)
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
	_ *openrouter.ModelUsage,
	_, _ string,
	_ billing.ModelUsageSource,
	_ string,
) error {
	return nil
}
