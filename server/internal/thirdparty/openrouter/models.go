package openrouter

import (
	"context"
	"encoding/json"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/modelcatalog"
)

// ModelCatalogClient discovers supported upstream models without running inference.
type ModelCatalogClient interface {
	ListModels(context.Context) (map[string]json.RawMessage, error)
}

var _ ModelCatalogClient = (*ChatClient)(nil)

// ListModels uses the same pooled HTTP transport and base URL as completions.
// It retains upstream metadata and filters with the inference allowlist.
func (c *ChatClient) ListModels(ctx context.Context) (map[string]json.RawMessage, error) {
	return modelcatalog.Fetch(ctx, c.httpClient, OpenRouterBaseURL, IsModelAllowed)
}
