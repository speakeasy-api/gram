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
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"log/slog"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/rag/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// This is the only embedding model currently supported
	// If you would like to add another embedding model you must modify the table to handle and index embeddings of that dimension
	defaultEmbeddingModel         = "openai/text-embedding-3-small"
	defaultFindToolsResultSize    = 3
	embeddingBatchSize            = 50
	embeddingMaxConcurrentBatches = 5
)

type ToolsetVectorStore struct {
	logger         *slog.Logger
	tracer         trace.Tracer
	db             repo.DBTX
	queries        *repo.Queries
	chatClient     *openrouter.ChatClient
	embeddingModel string
}

func NewToolsetVectorStore(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, chatClient *openrouter.ChatClient) *ToolsetVectorStore {
	if db == nil {
		return nil
	}

	return &ToolsetVectorStore{
		logger:         logger,
		tracer:         tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/rag"),
		db:             db,
		queries:        repo.New(db),
		chatClient:     chatClient,
		embeddingModel: defaultEmbeddingModel,
	}
}

func (s *ToolsetVectorStore) ToolsetToolsAreIndexed(ctx context.Context, toolset types.Toolset) (indexed bool, err error) {
	ctx, span := s.tracer.Start(ctx, "rag.toolsetToolsAreIndexed", trace.WithAttributes(
		attr.ToolsetID(toolset.ID),
		attr.ProjectID(toolset.ProjectID),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	toolsetUUID, err := uuid.Parse(toolset.ID)
	if err != nil {
		return false, fmt.Errorf("parse toolset id: %w", err)
	}

	projectUUID, err := uuid.Parse(toolset.ProjectID)
	if err != nil {
		return false, fmt.Errorf("parse project id: %w", err)
	}

	indexed, err = s.queries.ToolsetToolsAreIndexed(ctx, repo.ToolsetToolsAreIndexedParams{
		ProjectID:      projectUUID,
		ToolsetID:      toolsetUUID,
		ToolsetVersion: toolset.ToolsetVersion,
	})
	if err != nil {
		return false, fmt.Errorf("check toolset indexed status: %w", err)
	}

	return indexed, nil
}

func (s *ToolsetVectorStore) IndexToolset(ctx context.Context, toolset types.Toolset) (err error) {
	ctx, span := s.tracer.Start(ctx, "rag.indexToolset", trace.WithAttributes(
		attr.ToolsetID(toolset.ID),
		attr.ProjectID(toolset.ProjectID),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	toolsetUUID, err := uuid.Parse(toolset.ID)
	if err != nil {
		return fmt.Errorf("parse toolset id: %w", err)
	}

	candidates, err := s.prepareEmbeddingCandidates(ctx, toolset.Tools)
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

	// Delete all existing tool embeddings for this toolset first
	if err := s.queries.DeleteToolsetEmbeddings(ctx, toolsetUUID); err != nil {
		return fmt.Errorf("delete existing toolset embeddings: %w", err)
	}

	// Insert new embeddings
	for i, candidate := range candidates {
		vector := pgvector_go.NewVector(vectors[i])
		if err := s.insertToolEmbedding(
			ctx,
			uuid.MustParse(toolset.ProjectID),
			toolsetUUID,
			toolset.ToolsetVersion,
			candidate.entryKey,
			candidate.payload,
			vector,
			candidate.tags,
		); err != nil {
			return err
		}
	}

	return nil
}

func (s *ToolsetVectorStore) GetToolsetAvailableTags(ctx context.Context, toolset types.Toolset) ([]string, error) {
	tags, err := s.queries.ToolsetAvailableTags(ctx, repo.ToolsetAvailableTagsParams{
		ProjectID:      uuid.MustParse(toolset.ProjectID),
		ToolsetID:      uuid.MustParse(toolset.ID),
		ToolsetVersion: toolset.ToolsetVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("get toolset available tags: %w", err)
	}
	return tags, nil
}

type MatchMode string

const (
	MatchModeAny MatchMode = "any"
	MatchModeAll MatchMode = "all"
)

type SearchToolsOptions struct {
	Query     string
	Tags      []string
	MatchMode MatchMode
	Limit     int
}

// ToolSearchResult represents a search result with tool name and similarity score.
type ToolSearchResult struct {
	ToolName        string
	Tags            []string
	SimilarityScore float64
}

func (s *ToolsetVectorStore) SearchToolsetTools(ctx context.Context, toolset types.Toolset, opts SearchToolsOptions) (matches []*ToolSearchResult, err error) {
	ctx, span := s.tracer.Start(ctx, "rag.searchToolsetTools", trace.WithAttributes(
		attr.ToolsetID(toolset.ID),
		attr.ProjectID(toolset.ProjectID),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	toolsetUUID, err := uuid.Parse(toolset.ID)
	if err != nil {
		return nil, fmt.Errorf("parse toolset id: %w", err)
	}

	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return nil, errors.New("query is required")
	}

	limit := opts.Limit
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

	tags := opts.Tags
	if len(tags) == 0 {
		tags = make([]string, 0)
	}

	var rows []repo.SearchToolsetToolEmbeddingsAnyTagsMatchRow
	switch opts.MatchMode {
	case MatchModeAny:
		rows, err = s.queries.SearchToolsetToolEmbeddingsAnyTagsMatch(ctx, repo.SearchToolsetToolEmbeddingsAnyTagsMatchParams{
			QueryEmbedding1536: pgvector_go.NewVector(queryVectors[0]),
			ProjectID:          uuid.MustParse(toolset.ProjectID),
			ToolsetID:          toolsetUUID,
			ToolsetVersion:     toolset.ToolsetVersion,
			Tags:               tags,
			ResultLimit:        int32(limit), //nolint:gosec // limit is validated to be positive
		})
		if err != nil {
			return nil, fmt.Errorf("search toolset embeddings: %w", err)
		}
	case MatchModeAll:
		anyRows, err := s.queries.SearchToolsetToolEmbeddingsAllTagsMatch(ctx, repo.SearchToolsetToolEmbeddingsAllTagsMatchParams{
			QueryEmbedding1536: pgvector_go.NewVector(queryVectors[0]),
			ProjectID:          uuid.MustParse(toolset.ProjectID),
			ToolsetID:          toolsetUUID,
			ToolsetVersion:     toolset.ToolsetVersion,
			Tags:               tags,
			ResultLimit:        int32(limit), //nolint:gosec // limit is validated to be positive
		})
		if err != nil {
			return nil, fmt.Errorf("search toolset embeddings: %w", err)
		}

		// Need to convert to make the types match
		rows = make([]repo.SearchToolsetToolEmbeddingsAnyTagsMatchRow, len(anyRows))
		for i, r := range anyRows {
			rows[i] = repo.SearchToolsetToolEmbeddingsAnyTagsMatchRow(r)
		}
	default:
		return nil, fmt.Errorf("invalid match mode: %s", opts.MatchMode)
	}

	if len(rows) == 0 {
		return nil, nil
	}

	matches = make([]*ToolSearchResult, 0, len(rows))
	for _, row := range rows {
		var entry toolListEntry
		if err := json.Unmarshal(row.Payload, &entry); err != nil {
			return nil, fmt.Errorf("unmarshal tool entry payload: %w", err)
		}

		matches = append(matches, &ToolSearchResult{
			ToolName:        entry.Name,
			SimilarityScore: float64(row.Similarity),
			Tags:            row.Tags,
		})
	}

	return matches, nil
}

type embeddingCandidate struct {
	entryKey string
	payload  []byte
	content  string
	tags     []string
}

func (s *ToolsetVectorStore) prepareEmbeddingCandidates(ctx context.Context, tools []*types.Tool) ([]embeddingCandidate, error) {
	candidates := make([]embeddingCandidate, 0, len(tools))

	for _, tool := range tools {
		if conv.IsProxyTool(tool) {
			return nil, fmt.Errorf("index proxy tool for vector search: %s", tool.ExternalMcpToolDefinition.Name)
		}

		baseTool, err := conv.ToBaseTool(tool)
		if err != nil {
			continue
		}
		name, description, inputSchema, meta, err := conv.ToToolListEntry(tool)
		if err != nil {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		entry := toolListEntry{
			Name:        name,
			Description: description,
			InputSchema: inputSchema,
			Meta:        meta,
		}

		payload, err := json.Marshal(&entry)
		if err != nil {
			return nil, fmt.Errorf("marshal tool entry %s: %w", name, err)
		}

		content := buildEmbeddableContent(&entry)
		if strings.TrimSpace(content) == "" {
			continue
		}

		candidates = append(candidates, embeddingCandidate{
			entryKey: baseTool.ToolUrn,
			payload:  payload,
			content:  content,
			tags:     extractTags(tool),
		})
	}

	return candidates, nil
}


func extractTags(tool *types.Tool) []string {
	var tags []string
	if tool == nil {
		return tags
	}

	toolURN, err := conv.GetToolURN(*tool)
	if err != nil {
		return nil
	}

	tags = append(tags, fmt.Sprintf("source:%s", toolURN.Source))

	if tool.HTTPToolDefinition != nil {
		for _, tag := range tool.HTTPToolDefinition.Tags {
			tags = append(tags, fmt.Sprintf("%s/%s", toolURN.Source, tag))
		}
	} else if tool.PromptTemplate != nil {
		for _, subtoolURNString := range tool.PromptTemplate.ToolUrnsHint {
			subtoolURN, err := urn.ParseTool(subtoolURNString)
			if err != nil {
				continue
			}
			tags = append(tags, fmt.Sprintf("source:%s", subtoolURN.Source))
		}
	}

	return tags
}

func (s *ToolsetVectorStore) insertToolEmbedding(
	ctx context.Context,
	projectID uuid.UUID,
	toolsetID uuid.UUID,
	toolsetVersion int64,
	entryKey string,
	payload []byte,
	vector pgvector_go.Vector,
	tags []string,
) error {
	if entryKey == "" {
		return errors.New("entry key is required")
	}

	_, err := s.queries.InsertToolsetEmbedding(ctx, repo.InsertToolsetEmbeddingParams{
		ProjectID:      projectID,
		ToolsetID:      toolsetID,
		ToolsetVersion: toolsetVersion,
		EntryKey:       entryKey,
		EmbeddingModel: s.embeddingModel,
		Embedding1536:  vector,
		Payload:        payload,
		Tags:           tags,
	})
	if err != nil {
		return fmt.Errorf("insert tool embedding %s: %w", entryKey, err)
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

type toolListEntry struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	Meta        map[string]any  `json:"_meta,omitempty"`
}

func buildEmbeddableContent(entry *toolListEntry) string {
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
