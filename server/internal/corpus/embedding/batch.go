package embedding

import (
	"context"
	"fmt"
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
	results := make([]BatchEmbedResult, len(requests))

	// Separate requests into those needing embedding and those that can be skipped.
	var toEmbed []BatchEmbedRequest
	var toEmbedIndices []int

	for i, req := range requests {
		if existingFP, ok := existingFingerprints[req.ChunkID]; ok && existingFP == req.Fingerprint {
			results[i] = BatchEmbedResult{
				ChunkID:   req.ChunkID,
				Embedding: nil,
				Skipped:   true,
			}
		} else {
			toEmbed = append(toEmbed, req)
			toEmbedIndices = append(toEmbedIndices, i)
		}
	}

	if len(toEmbed) == 0 {
		return results, nil
	}

	texts := make([]string, len(toEmbed))
	for i, req := range toEmbed {
		texts[i] = req.Text
	}

	embeddings, err := client.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embed batch: %w", err)
	}

	if len(embeddings) != len(toEmbed) {
		return nil, fmt.Errorf("embedding count mismatch: got %d, want %d", len(embeddings), len(toEmbed))
	}

	for i, emb := range embeddings {
		idx := toEmbedIndices[i]
		results[idx] = BatchEmbedResult{
			ChunkID:   toEmbed[i].ChunkID,
			Embedding: emb.Embedding,
			Skipped:   false,
		}
	}

	return results, nil
}
