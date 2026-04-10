package search

import (
	"context"
	"fmt"

	"github.com/pgvector/pgvector-go"

	"github.com/speakeasy-api/gram/server/internal/corpus/embedding"
	"github.com/speakeasy-api/gram/server/internal/mcp"
)

// DocsSearchAdapter bridges the corpus search.Service to the mcp.DocsSearcher interface.
type DocsSearchAdapter struct {
	searchService   *Service
	embeddingClient *embedding.Client
}

// NewDocsSearchAdapter creates a new adapter wrapping a search.Service and embedding.Client.
func NewDocsSearchAdapter(searchService *Service, embeddingClient *embedding.Client) *DocsSearchAdapter {
	return &DocsSearchAdapter{
		searchService:   searchService,
		embeddingClient: embeddingClient,
	}
}

var _ mcp.DocsSearcher = (*DocsSearchAdapter)(nil)

// SearchDocs implements mcp.DocsSearcher by performing a hybrid FTS + vector search.
func (a *DocsSearchAdapter) SearchDocs(ctx context.Context, projectID string, query string, limit int) ([]mcp.DocsSearchResult, error) {
	// Embed the query for vector search.
	embeddings, err := a.embeddingClient.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	var vec pgvector.Vector
	if len(embeddings) > 0 {
		vec = pgvector.NewVector(embeddings[0].Embedding)
	}

	resp, err := a.searchService.Search(ctx, SearchParams{
		ProjectID: projectID,
		Query:     query,
		Embedding: vec,
		Metadata:  nil,
		Limit:     limit,
		Cursor:    "",
		FTSWeight: 1.0,
		VecWeight: 1.0,
	})
	if err != nil {
		return nil, fmt.Errorf("corpus search: %w", err)
	}

	results := make([]mcp.DocsSearchResult, len(resp.Results))
	for i, r := range resp.Results {
		results[i] = mcp.DocsSearchResult{
			ChunkID:     r.ChunkID,
			FilePath:    r.FilePath,
			HeadingPath: r.HeadingPath,
			Breadcrumb:  r.Breadcrumb,
			Content:     r.Content,
			Score:       r.Score,
		}
	}
	return results, nil
}

// GetChunk implements mcp.DocsSearcher by retrieving a chunk with neighbors.
func (a *DocsSearchAdapter) GetChunk(ctx context.Context, projectID string, chunkID string) (*mcp.DocsChunk, error) {
	chunk, err := a.searchService.GetChunk(ctx, projectID, chunkID)
	if err != nil {
		return nil, err
	}

	result := &mcp.DocsChunk{
		ChunkID:     chunk.ChunkID,
		FilePath:    chunk.FilePath,
		HeadingPath: chunk.HeadingPath,
		Breadcrumb:  chunk.Breadcrumb,
		Content:     chunk.Content,
		ContentText: chunk.ContentText,
		Prev:        nil,
		Next:        nil,
	}

	if chunk.Prev != nil {
		result.Prev = &mcp.DocsNeighborChunk{
			ChunkID:     chunk.Prev.ChunkID,
			HeadingPath: chunk.Prev.HeadingPath,
			Breadcrumb:  chunk.Prev.Breadcrumb,
		}
	}

	if chunk.Next != nil {
		result.Next = &mcp.DocsNeighborChunk{
			ChunkID:     chunk.Next.ChunkID,
			HeadingPath: chunk.Next.HeadingPath,
			Breadcrumb:  chunk.Next.Breadcrumb,
		}
	}

	return result, nil
}
