package rag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector_go "github.com/pgvector/pgvector-go"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/rag/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	defaultEmbeddingModel         = "text-embedding-3-small"
	defaultFindToolsResultSize    = 5
	embeddingBatchSize            = 50
	embeddingMaxConcurrentBatches = 5
)

type ToolsetVectorStore struct {
	db             repo.DBTX
	queries        *repo.Queries
	chatClient     *openrouter.ChatClient
	embeddingModel string
}

func NewToolsetVectorStore(db *pgxpool.Pool, chatClient *openrouter.ChatClient) *ToolsetVectorStore {
	if db == nil {
		return nil
	}

	return &ToolsetVectorStore{
		db:             db,
		queries:        repo.New(db),
		chatClient:     chatClient,
		embeddingModel: defaultEmbeddingModel,
	}
}

func (s *ToolsetVectorStore) IndexToolset(ctx context.Context, toolset types.Toolset, entries []*ToolListEntry) error {
	toolsetUUID, err := uuid.Parse(toolset.ID)
	if err != nil {
		return fmt.Errorf("parse toolset id: %w", err)
	}

	candidates, err := s.prepareEmbeddingCandidates(entries)
	if err != nil {
		return err
	}

	if len(candidates) == 0 {
		return nil
	}

	vectors, err := s.generateEmbeddings(ctx, toolset.OrganizationID, candidates)
	if err != nil {
		return err
	}

	for i, candidate := range candidates {
		vector := pgvector_go.NewVector(vectors[i])
		if err := s.upsertToolEmbedding(ctx, uuid.MustParse(toolset.ProjectID), toolsetUUID, candidate.entryKey, candidate.payload, vector); err != nil {
			return err
		}
	}

	return nil
}

func (s *ToolsetVectorStore) SearchToolset(ctx context.Context, toolset types.Toolset, query string, limit int) ([]*ToolListEntry, error) {
	toolsetUUID, err := uuid.Parse(toolset.ID)
	if err != nil {
		return nil, fmt.Errorf("parse toolset id: %w", err)
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("query is required")
	}

	if limit <= 0 {
		limit = defaultFindToolsResultSize
	}

	queryVectors, err := s.chatClient.CreateEmbeddings(ctx, toolset.OrganizationID, s.embeddingModel, []string{query})
	if err != nil {
		return nil, fmt.Errorf("create query embedding: %w", err)
	}
	if len(queryVectors) != 1 {
		return nil, fmt.Errorf("query embedding response contained %d vectors, expected 1", len(queryVectors))
	}

	rows, err := s.queries.SearchToolsetEmbeddings(ctx, repo.SearchToolsetEmbeddingsParams{
		QueryEmbedding1536: pgvector_go.NewVector(queryVectors[0]),
		ProjectID:          uuid.MustParse(toolset.ProjectID),
		ToolsetID:          toolsetUUID,
		ResultLimit:        int32(limit), //nolint:gosec // limit is validated to be positive
	})
	if err != nil {
		return nil, fmt.Errorf("search toolset embeddings: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	matches := make([]*ToolListEntry, 0, len(rows))
	for _, row := range rows {
		var entry ToolListEntry
		if err := json.Unmarshal(row.Payload, &entry); err != nil {
			return nil, fmt.Errorf("unmarshal tool entry payload: %w", err)
		}

		entry.Meta = ensureMetaMap(entry.Meta)
		entry.Meta["similarity_score"] = float64(row.Similarity)
		matches = append(matches, &entry)
	}

	return matches, nil
}

type embeddingCandidate struct {
	entryKey string
	payload  []byte
	content  string
}

func (s *ToolsetVectorStore) prepareEmbeddingCandidates(entries []*ToolListEntry) ([]embeddingCandidate, error) {
	candidates := make([]embeddingCandidate, 0, len(entries))

	for _, entry := range entries {
		if entry == nil {
			continue
		}

		entryCopy := *entry
		entryCopy.Name = strings.TrimSpace(entryCopy.Name)
		if entryCopy.Name == "" {
			continue
		}

		payload, err := json.Marshal(&entryCopy)
		if err != nil {
			return nil, fmt.Errorf("marshal tool entry %s: %w", entryCopy.Name, err)
		}

		content := buildEmbeddableContent(&entryCopy)
		if strings.TrimSpace(content) == "" {
			continue
		}

		candidates = append(candidates, embeddingCandidate{
			entryKey: entryCopy.Name,
			payload:  payload,
			content:  content,
		})
	}

	return candidates, nil
}

func (s *ToolsetVectorStore) upsertToolEmbedding(ctx context.Context, projectID uuid.UUID, toolsetID uuid.UUID, entryKey string, payload []byte, vector pgvector_go.Vector) error {
	if entryKey == "" {
		return errors.New("entry key is required")
	}

	_, err := s.queries.UpsertToolsetEmbedding(ctx, repo.UpsertToolsetEmbeddingParams{
		ProjectID:      projectID,
		ToolsetID:      toolsetID,
		EntryKey:       entryKey,
		EmbeddingModel: s.embeddingModel,
		Embedding1536:  vector,
		Payload:        payload,
	})
	if err != nil {
		return fmt.Errorf("upsert tool embedding %s: %w", entryKey, err)
	}

	return nil
}

func (s *ToolsetVectorStore) generateEmbeddings(ctx context.Context, orgID string, candidates []embeddingCandidate) ([][]float32, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	total := len(candidates)
	results := make([][]float32, total)

	batchCount := (total + embeddingBatchSize - 1) / embeddingBatchSize
	workerCount := embeddingMaxConcurrentBatches
	if workerCount > batchCount {
		workerCount = batchCount
	}

	if workerCount == 0 {
		return results, nil
	}

	workChan := make(chan int, batchCount)

	for i := 0; i < batchCount; i++ {
		workChan <- i
	}
	close(workChan)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErrOnce sync.Once
	var firstErr error

	setErr := func(err error) {
		firstErrOnce.Do(func() {
			firstErr = err
		})
	}

	for w := 0; w < workerCount; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batchIdx := range workChan {
				if firstErr != nil {
					return
				}

				start := batchIdx * embeddingBatchSize
				end := start + embeddingBatchSize
				if end > total {
					end = total
				}

				inputs := make([]string, end-start)
				for i := start; i < end; i++ {
					inputs[i-start] = candidates[i].content
				}

				vectors, err := s.chatClient.CreateEmbeddings(ctx, orgID, s.embeddingModel, inputs)
				if err != nil {
					setErr(fmt.Errorf("create embeddings batch: %w", err))
					return
				}
				if len(vectors) != len(inputs) {
					setErr(fmt.Errorf("embedding vector count %d does not match candidate count %d", len(vectors), len(inputs)))
					return
				}

				// Mutex prevents race condition from multiple goroutines writing to shared results slice
				mu.Lock()
				for i := start; i < end; i++ {
					results[i] = vectors[i-start]
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	for i, vector := range results {
		if vector == nil {
			return nil, fmt.Errorf("missing embedding for entry %s", candidates[i].entryKey)
		}
	}

	return results, nil
}

type ToolListEntry struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	Meta        map[string]any  `json:"_meta,omitempty"`
}

func ensureMetaMap(meta map[string]any) map[string]any {
	if meta == nil {
		return map[string]any{}
	}
	return meta
}

func buildEmbeddableContent(entry *ToolListEntry) string {
	var schema string
	if len(entry.InputSchema) > 0 {
		schema = string(entry.InputSchema)
	}

	var meta string
	if entry.Meta != nil {
		if payload, err := json.Marshal(entry.Meta); err == nil {
			meta = string(payload)
		}
	}

	parts := []string{
		entry.Name,
		entry.Description,
		schema,
		meta,
	}

	return strings.TrimSpace(strings.Join(filterNonEmpty(parts), "\n"))
}

func filterNonEmpty(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			filtered = append(filtered, v)
		}
	}
	return filtered
}
