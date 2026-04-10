package search

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
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

const (
	defaultK     = 60.0
	defaultLimit = 10
	maxFTSRows   = 100
	maxVecRows   = 100
)

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
	projectID, err := uuid.Parse(params.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("parse project ID: %w", err)
	}

	limit := params.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	offset, err := DecodeCursor(params.Cursor)
	if err != nil {
		return nil, fmt.Errorf("decode cursor: %w", err)
	}

	metaClause, metaArgs := buildMetadataFilter(params.Metadata)

	doFTS := params.FTSWeight > 0 && params.Query != ""
	doVec := params.VecWeight > 0 && len(params.Embedding.Slice()) > 0

	var ftsRanked, vecRanked []rankedChunkID

	// Run FTS and vector searches concurrently when both are needed.
	g, gctx := errgroup.WithContext(ctx)

	if doFTS {
		g.Go(func() error {
			ranked, ftsErr := s.ftsSearch(gctx, projectID, params.Query, metaClause, metaArgs)
			if ftsErr != nil {
				s.logger.ErrorContext(gctx, "fts search", attr.SlogError(ftsErr))
				return fmt.Errorf("fts search: %w", ftsErr)
			}
			ftsRanked = ranked
			return nil
		})
	}

	if doVec {
		g.Go(func() error {
			ranked, vecErr := s.vectorSearch(gctx, projectID, params.Embedding, metaClause, metaArgs)
			if vecErr != nil {
				s.logger.ErrorContext(gctx, "vector search", attr.SlogError(vecErr))
				return fmt.Errorf("vector search: %w", vecErr)
			}
			vecRanked = ranked
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	// RRF blending.
	var lists [][]string
	var weights []float64

	if len(ftsRanked) > 0 {
		lists = append(lists, extractIDs(ftsRanked))
		weights = append(weights, params.FTSWeight)
	}

	if len(vecRanked) > 0 {
		lists = append(lists, extractIDs(vecRanked))
		weights = append(weights, params.VecWeight)
	}

	if len(lists) == 0 {
		return &SearchResponse{Results: nil, NextCursor: ""}, nil
	}

	rrfResults := RRF(lists, weights, defaultK)

	// Apply pagination.
	total := len(rrfResults)
	start := min(offset, total)
	end := min(start+limit, total)
	page := rrfResults[start:end]

	// Fetch full chunk data for the page.
	chunkIDs := make([]string, len(page))
	scoreMap := make(map[string]float64, len(page))
	for i, r := range page {
		chunkIDs[i] = r.ID
		scoreMap[r.ID] = r.Score
	}

	chunks, err := s.fetchChunks(ctx, projectID, chunkIDs)
	if err != nil {
		return nil, fmt.Errorf("fetch chunks: %w", err)
	}

	// Maintain RRF order.
	chunkMap := make(map[string]SearchResult, len(chunks))
	for _, c := range chunks {
		c.Score = scoreMap[c.ChunkID]
		chunkMap[c.ChunkID] = c
	}

	results := make([]SearchResult, 0, len(page))
	for _, r := range page {
		if c, ok := chunkMap[r.ID]; ok {
			results = append(results, c)
		}
	}

	nextCursor := ""
	if end < total {
		nextCursor = EncodeCursor(end)
	}

	return &SearchResponse{
		Results:    results,
		NextCursor: nextCursor,
	}, nil
}

type rankedChunkID struct {
	chunkID string
}

func extractIDs(ranked []rankedChunkID) []string {
	ids := make([]string, len(ranked))
	for i, r := range ranked {
		ids[i] = r.chunkID
	}
	return ids
}

// buildMetadataFilter constructs a parameterized SQL clause and args for JSONB metadata filtering.
// Metadata keys are sorted for deterministic query generation.
func buildMetadataFilter(metadata map[string]string) (string, []any) {
	if len(metadata) == 0 {
		return "", nil
	}

	// Sort keys for deterministic SQL generation.
	keys := make([]string, 0, len(metadata))
	for k := range metadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	args := make([]any, 0, len(metadata)*2)
	clause := ""
	argIdx := 3 // $1=projectID, $2=query/embedding param
	for _, k := range keys {
		if clause != "" {
			clause += " AND "
		}
		clause += fmt.Sprintf("metadata->>$%d = $%d", argIdx, argIdx+1)
		args = append(args, k, metadata[k])
		argIdx += 2
	}
	return " AND " + clause, args
}

// ftsSearch runs a full-text search with phrase proximity boosting.
func (s *Service) ftsSearch(ctx context.Context, projectID uuid.UUID, query string, metaClause string, metaArgs []any) ([]rankedChunkID, error) {
	// Combined ranking: ts_rank(plain) + 2*ts_rank(phrase) gives proximity a 2x boost.
	sql := `SELECT chunk_id FROM corpus_chunks
		WHERE project_id = $1
		  AND content_tsvector @@ plainto_tsquery('english', $2)` + metaClause + `
		ORDER BY (ts_rank(content_tsvector, plainto_tsquery('english', $2))
		        + 2.0 * ts_rank(content_tsvector, phraseto_tsquery('english', $2))) DESC
		LIMIT ` + fmt.Sprintf("%d", maxFTSRows)

	return s.scanRankedIDs(ctx, sql, projectID, query, metaArgs)
}

// vectorSearch runs a cosine similarity search on embeddings.
func (s *Service) vectorSearch(ctx context.Context, projectID uuid.UUID, embedding pgvector.Vector, metaClause string, metaArgs []any) ([]rankedChunkID, error) {
	sql := `SELECT chunk_id FROM corpus_chunks
		WHERE project_id = $1
		  AND embedding IS NOT NULL` + metaClause + `
		ORDER BY embedding <=> $2
		LIMIT ` + fmt.Sprintf("%d", maxVecRows)

	return s.scanRankedIDs(ctx, sql, projectID, embedding, metaArgs)
}

// scanRankedIDs executes a query that returns chunk_id rows and collects them in order.
func (s *Service) scanRankedIDs(ctx context.Context, sql string, projectID uuid.UUID, param any, metaArgs []any) ([]rankedChunkID, error) {
	args := make([]any, 0, 2+len(metaArgs))
	args = append(args, projectID, param)
	args = append(args, metaArgs...)

	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("ranked query: %w", err)
	}
	defer rows.Close()

	var results []rankedChunkID
	for rows.Next() {
		var r rankedChunkID
		if err := rows.Scan(&r.chunkID); err != nil {
			return nil, fmt.Errorf("scan ranked row: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ranked rows iteration: %w", err)
	}
	return results, nil
}

// fetchChunks loads full chunk data for a set of chunk IDs.
func (s *Service) fetchChunks(ctx context.Context, projectID uuid.UUID, chunkIDs []string) ([]SearchResult, error) {
	if len(chunkIDs) == 0 {
		return nil, nil
	}

	sql := `SELECT chunk_id, file_path, COALESCE(heading_path, ''), COALESCE(breadcrumb, ''),
				   content, content_text, metadata
			FROM corpus_chunks
			WHERE project_id = $1 AND chunk_id = ANY($2)`

	rows, err := s.db.Query(ctx, sql, projectID, chunkIDs)
	if err != nil {
		return nil, fmt.Errorf("fetch chunks query: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.ChunkID, &r.FilePath, &r.HeadingPath, &r.Breadcrumb,
			&r.Content, &r.ContentText, &r.Metadata); err != nil {
			return nil, fmt.Errorf("scan chunk: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fetch chunks rows iteration: %w", err)
	}
	return results, nil
}

// GetChunk retrieves a chunk by its chunk_id within a project, including neighbor information.
func (s *Service) GetChunk(ctx context.Context, projectID string, chunkID string) (*ChunkWithNeighbors, error) {
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("parse project ID: %w", err)
	}

	// Window functions compute prev/next neighbors in the same file in a single query.
	sql := `WITH ordered AS (
		SELECT chunk_id, file_path, COALESCE(heading_path, '') AS heading_path,
			   COALESCE(breadcrumb, '') AS breadcrumb, content, content_text, metadata,
			   LAG(chunk_id) OVER w AS prev_chunk_id,
			   LAG(heading_path) OVER w AS prev_heading_path,
			   LAG(breadcrumb) OVER w AS prev_breadcrumb,
			   LEAD(chunk_id) OVER w AS next_chunk_id,
			   LEAD(heading_path) OVER w AS next_heading_path,
			   LEAD(breadcrumb) OVER w AS next_breadcrumb
		FROM corpus_chunks
		WHERE project_id = $1
		  AND file_path = (SELECT file_path FROM corpus_chunks WHERE project_id = $1 AND chunk_id = $2)
		WINDOW w AS (PARTITION BY file_path ORDER BY chunk_id)
	)
	SELECT chunk_id, file_path, heading_path, breadcrumb, content, content_text, metadata,
		   prev_chunk_id, prev_heading_path, prev_breadcrumb,
		   next_chunk_id, next_heading_path, next_breadcrumb
	FROM ordered
	WHERE chunk_id = $2`

	var (
		chunk          ChunkWithNeighbors
		prevChunkID    *string
		prevHeading    *string
		prevBreadcrumb *string
		nextChunkID    *string
		nextHeading    *string
		nextBreadcrumb *string
	)

	err = s.db.QueryRow(ctx, sql, pid, chunkID).Scan(
		&chunk.ChunkID, &chunk.FilePath, &chunk.HeadingPath, &chunk.Breadcrumb,
		&chunk.Content, &chunk.ContentText, &chunk.Metadata,
		&prevChunkID, &prevHeading, &prevBreadcrumb,
		&nextChunkID, &nextHeading, &nextBreadcrumb,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("chunk not found: %s", chunkID)
		}
		return nil, fmt.Errorf("get chunk: %w", err)
	}

	if prevChunkID != nil {
		chunk.Prev = &NeighborChunk{
			ChunkID:     *prevChunkID,
			HeadingPath: conv.PtrValOr(prevHeading, ""),
			Breadcrumb:  conv.PtrValOr(prevBreadcrumb, ""),
		}
	}

	if nextChunkID != nil {
		chunk.Next = &NeighborChunk{
			ChunkID:     *nextChunkID,
			HeadingPath: conv.PtrValOr(nextHeading, ""),
			Breadcrumb:  conv.PtrValOr(nextBreadcrumb, ""),
		}
	}

	return &chunk, nil
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
