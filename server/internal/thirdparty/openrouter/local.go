package openrouter

import (
	"context"
	"errors"

	"github.com/speakeasy-api/gram/server/internal/mv"
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

func (o *Development) GetModelPricing(ctx context.Context, id string) (*mv.ModelPricing, error) {
	// Development mode doesn't have access to cached pricing
	return nil, errors.New("model pricing not available in development mode")
}

func (o *Development) FetchAndCacheModelPricing(ctx context.Context) error {
	return nil
}
