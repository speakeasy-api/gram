package openrouter

import (
	"context"
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

func (o *Development) RefreshAPIKeyLimit(ctx context.Context, orgID string) (int, error) {
	return 0, nil
}
