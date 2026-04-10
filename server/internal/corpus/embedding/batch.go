package embedding

import (
	"context"
)

// BatchEmbedRequest describes a single chunk to be embedded.
type BatchEmbedRequest struct {
	ChunkID     string
	Text        string
	Fingerprint string
}

// BatchEmbedResult holds the embedding for a chunk.
type BatchEmbedResult struct {
	ChunkID   string
	Embedding []float32
	Skipped   bool
}

// BatchEmbed embeds a batch of texts, skipping any whose fingerprints match
// the existing set. existingFingerprints maps chunk_id -> content_fingerprint.
func BatchEmbed(ctx context.Context, client *Client, requests []BatchEmbedRequest, existingFingerprints map[string]string) ([]BatchEmbedResult, error) {
	panic("not implemented")
}
