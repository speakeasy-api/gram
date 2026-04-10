package search

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// SearchParams defines the parameters for a hybrid search.
type SearchParams struct {
	ProjectID string
	Query     string
	Embedding pgvector.Vector
	Metadata  map[string]string
	Limit     int
	Cursor    string
	FTSWeight float64
	VecWeight float64
}

// SearchResult represents a single search result.
type SearchResult struct {
	ChunkID     string
	FilePath    string
	HeadingPath string
	Breadcrumb  string
	Content     string
	ContentText string
	Metadata    []byte
	Score       float64
}

// SearchResponse is the paginated search response.
type SearchResponse struct {
	Results    []SearchResult
	NextCursor string
}

// ChunkWithNeighbors is a chunk with its adjacent chunks in the same file.
type ChunkWithNeighbors struct {
	ChunkID     string
	FilePath    string
	HeadingPath string
	Breadcrumb  string
	Content     string
	ContentText string
	Metadata    []byte
	Prev        *NeighborChunk
	Next        *NeighborChunk
}

// NeighborChunk is a minimal representation of an adjacent chunk.
type NeighborChunk struct {
	ChunkID     string
	HeadingPath string
	Breadcrumb  string
}

// Service provides hybrid search over corpus chunks.
type Service struct {
	db     *pgxpool.Pool
	logger *slog.Logger
}

// NewService creates a new search service.
func NewService(db *pgxpool.Pool, logger *slog.Logger) *Service {
	return &Service{
		db:     db,
		logger: logger,
	}
}

// Search performs hybrid search combining FTS and vector similarity with RRF blending.
func (s *Service) Search(ctx context.Context, params SearchParams) (*SearchResponse, error) {
	panic("not implemented")
}

// GetChunk retrieves a chunk by its chunk_id within a project, including neighbor information.
func (s *Service) GetChunk(ctx context.Context, projectID string, chunkID string) (*ChunkWithNeighbors, error) {
	panic("not implemented")
}

// DecodeCursor decodes a base64-encoded pagination cursor into offset.
func DecodeCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	data, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("decode cursor: %w", err)
	}
	offset := 0
	_, err = fmt.Sscanf(string(data), "%d", &offset)
	if err != nil {
		return 0, fmt.Errorf("parse cursor offset: %w", err)
	}
	return offset, nil
}

// EncodeCursor encodes an offset into a base64 pagination cursor.
func EncodeCursor(offset int) string {
	return base64.StdEncoding.EncodeToString(fmt.Appendf(nil, "%d", offset))
}
