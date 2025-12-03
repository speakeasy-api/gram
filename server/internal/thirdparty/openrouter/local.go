package openrouter

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/billing"
)

type Development struct {
	apiKey string
}

func NewDevelopment(apiKey string) *Development {
	return &Development{apiKey: apiKey}
}

func (o *Development) ProvisionAPIKey(context.Context, string) (string, error) {
	return o.apiKey, nil
}

func (o *Development) RefreshAPIKeyLimit(ctx context.Context, orgID string, limit *int) (int, error) {
	return 0, nil
}

func (o *Development) GetCreditsUsed(ctx context.Context, orgID string) (float64, int, error) {
	return 12.5, 10, nil // arbitrary local numbers
}

func (o *Development) TriggerModelUsageTracking(ctx context.Context, generationID string, orgID string, projectID string, source billing.ModelUsageSource, chatID string) error {
	// Development mode doesn't track model usage
	return nil
}
