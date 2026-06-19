package openrouter

import (
	"context"
)

type Development struct {
	apiKey string
}

var _ Provisioner = (*Development)(nil)

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

func (o *Development) GetKeyUsage(ctx context.Context, apiKey string) (float64, *int64, error) {
	return 12.5, nil, nil // arbitrary local number; unlimited dev key
}

func (o *Development) ReconcileMonthlyCredits(ctx context.Context, orgID string, currentLimit int64, upstreamLimit *int64) (int64, error) {
	return currentLimit, nil
}

func (o *Development) GetModelUsage(ctx context.Context, generationID string, orgID string) (*ModelUsage, error) {
	// Development mode doesn't track model usage
	totalCost := 12.5
	return &ModelUsage{
		TotalCost:             &totalCost,
		CacheDiscount:         0,
		UpstreamInferenceCost: 0,
		Model:                 "gpt-5.4",
		TokensPrompt:          100,
		TokensCompletion:      100,
		NativeTokensCached:    100,
		NativeTokensReasoning: 100,
	}, nil
}
