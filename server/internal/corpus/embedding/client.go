package embedding

import (
	"context"
)

const (
	ModelTextEmbedding3Large = "text-embedding-3-large"
	EmbeddingDimensions      = 3072
)

// EmbeddingResult holds the embedding vector for a single text input.
type EmbeddingResult struct {
	Index     int
	Embedding []float32
}

// Client calls the OpenAI embeddings API.
type Client struct {
	apiKey  string
	baseURL string
	model   string
}

// NewClient creates a new OpenAI embeddings client.
func NewClient(apiKey string, baseURL string) *Client {
	panic("not implemented")
}

// Embed sends a batch of texts to the embeddings API and returns the results.
func (c *Client) Embed(ctx context.Context, texts []string) ([]EmbeddingResult, error) {
	panic("not implemented")
}
