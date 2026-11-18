package chat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type Client struct {
	completionClient openrouter.CompletionClient
}

var _ openrouter.CompletionClient = (*Client)(nil)
var _ openrouter.ModelCatalogClient = (*Client)(nil)

// ListModels preserves the provider catalog and its supported-model policy
// through the agentic wrapper used by the chat service.
func (c *Client) ListModels(ctx context.Context) (map[string]json.RawMessage, error) {
	catalogClient, ok := c.completionClient.(openrouter.ModelCatalogClient)
	if !ok {
		return nil, fmt.Errorf("completion provider does not support model discovery")
	}
	catalog, err := catalogClient.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("list agent client models: %w", err)
	}
	return catalog, nil
}

func (c *Client) GetCompletion(ctx context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	resp, err := c.completionClient.GetCompletion(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("get completion: %w", err)
	}
	return resp, nil
}

func (c *Client) GetCompletionStream(ctx context.Context, request openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	stream, err := c.completionClient.GetCompletionStream(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("get completion stream: %w", err)
	}
	return stream, nil
}

func (c *Client) GetObjectCompletion(ctx context.Context, request openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	resp, err := c.completionClient.GetObjectCompletion(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("get object completion: %w", err)
	}
	return resp, nil
}

func (c *Client) ResolveKey(ctx context.Context, orgID string, projectID string, slot billing.ModelUsageSource, keyType openrouter.KeyType) (openrouter.ResolvedKey, error) {
	resolved, err := c.completionClient.ResolveKey(ctx, orgID, projectID, slot, keyType)
	if err != nil {
		return openrouter.ResolvedKey{}, fmt.Errorf("agent client: %w", err)
	}
	return resolved, nil
}

func (c *Client) CreateEmbeddings(ctx context.Context, orgID string, model string, inputs []string, opts ...openrouter.EmbeddingOption) ([][]float32, error) {
	embeddings, err := c.completionClient.CreateEmbeddings(ctx, orgID, model, inputs, opts...)
	if err != nil {
		return nil, fmt.Errorf("create embeddings: %w", err)
	}
	return embeddings, nil
}

func NewAgenticChatClient(completionClient openrouter.CompletionClient) *Client {
	return &Client{
		completionClient: completionClient,
	}
}
