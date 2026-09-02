package rag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/rag/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// This is the only embedding model currently supported
	// If you would like to add another embedding model you must modify the table to handle and index embeddings of that dimension
	defaultEmbeddingModel      = "openai/text-embedding-3-small"
	defaultFindToolsResultSize = 3
	// OpenAI allows at most 300,000 tokens across an embedding request.
	// A tokenizer cannot emit more tokens than the input has UTF-8 bytes, so
	// this byte ceiling guarantees the aggregate token limit without another
	// tokenization pass. Oversized individual inputs are limited by the client.
	embeddingMaxBatchBytes        = 300_000
	embeddingMaxConcurrentBatches = 5
)

type ToolsetVectorStore struct {
	logger         *slog.Logger
	tracer         trace.Tracer
	db             repo.DBTX
	queries        *repo.Queries
	chatClient     openrouter.CompletionClient
	embeddingModel string
}

func NewToolsetVectorStore(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	chatClient openrouter.CompletionClient,
) *ToolsetVectorStore {
	if db == nil {
		return nil
	}

	return &ToolsetVectorStore{
		logger:         logger.With(attr.SlogComponent("toolset_vector_store")),
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
	projectUUID, err := uuid.Parse(toolset.ProjectID)
	if err != nil {
		return fmt.Errorf("parse project id: %w", err)
	}

	candidates, err := s.prepareEmbeddingCandidates(ctx, toolset.Tools)
	if err != nil {
		return err
	}

	if len(candidates) == 0 {
		return nil
	}

	vectors, err := s.generateEmbeddings(ctx, toolset, projectUUID, candidates)
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
			projectUUID,
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
			ResultLimit:        int32(limit),
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
			ResultLimit:        int32(limit),
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
	toolID    string
	entryKey  string
	payload   []byte
	content   string
	fallbacks []string
	tags      []string
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
		toolEntry, err := conv.ToToolListEntry(tool)
		if err != nil {
			continue
		}
		name := strings.TrimSpace(toolEntry.Name)
		if name == "" {
			continue
		}

		entry := toolListEntry{
			Name:        name,
			Description: toolEntry.Description,
			InputSchema: toolEntry.InputSchema,
			Meta:        toolEntry.Meta,
		}

		payload, err := json.Marshal(&entry)
		if err != nil {
			return nil, fmt.Errorf("marshal tool entry %s: %w", name, err)
		}

		tags := extractTags(tool)
		schemaSummary, topLevelSchemaSummary := summarizeInputSchemaLevels(entry.InputSchema)
		content := buildEmbeddableContent(&entry, tags, schemaSummary)
		if strings.TrimSpace(content) == "" {
			continue
		}

		candidates = append(candidates, embeddingCandidate{
			toolID:   baseTool.ID,
			entryKey: baseTool.ToolUrn,
			payload:  payload,
			content:  content,
			fallbacks: []string{
				buildTopLevelEmbeddableContent(&entry, topLevelSchemaSummary),
				buildNameDescriptionEmbeddableContent(&entry),
			},
			tags: tags,
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

type embeddingBatch struct {
	startIdx int
	endIdx   int
}

const (
	embeddingFallbackStrategyTopLevelSchema  = "top_level_schema"
	embeddingFallbackStrategyNameDescription = "name_description"
	embeddingFallbackStrategyTokenTruncation = "token_truncation"
)

type embeddingFallbackSelection struct {
	candidateIndex int
	strategy       string
}

type embeddingFallbackLogContext struct {
	organizationID   string
	organizationSlug string
	projectID        string
	projectSlug      string
	toolsetID        string
	toolsetSlug      string
	model            string
}

func selectEmbeddingCandidateContents(model string, candidates []embeddingCandidate) ([]embeddingFallbackSelection, error) {
	inputs := make([]string, len(candidates))
	inputFallbacks := make([][]string, len(candidates))
	for i, candidate := range candidates {
		inputs[i] = candidate.content
		inputFallbacks[i] = candidate.fallbacks
	}

	selected, selectedFallbacks, err := openrouter.SelectEmbeddingInputFallbacks(model, inputs, inputFallbacks)
	if err != nil {
		return nil, fmt.Errorf("select embedding input fallbacks: %w", err)
	}
	for i, content := range selected {
		candidates[i].content = content
	}

	var fallbackSelections []embeddingFallbackSelection
	for _, selection := range selectedFallbacks {
		strategy := ""
		switch {
		case selection.RequiresTruncation:
			strategy = embeddingFallbackStrategyTokenTruncation
		case selection.FallbackIndex == 0:
			strategy = embeddingFallbackStrategyTopLevelSchema
		case selection.FallbackIndex > 0:
			strategy = embeddingFallbackStrategyNameDescription
		}
		if strategy != "" {
			fallbackSelections = append(fallbackSelections, embeddingFallbackSelection{
				candidateIndex: selection.InputIndex,
				strategy:       strategy,
			})
		}
	}

	return fallbackSelections, nil
}

func emitEmbeddingFallbackLogs(
	ctx context.Context,
	logger *slog.Logger,
	logContext embeddingFallbackLogContext,
	candidates []embeddingCandidate,
	selections []embeddingFallbackSelection,
) {
	for _, selection := range selections {
		candidate := candidates[selection.candidateIndex]
		attrs := []any{
			attr.SlogOrganizationID(logContext.organizationID),
			attr.SlogProjectID(logContext.projectID),
			attr.SlogToolsetID(logContext.toolsetID),
			attr.SlogToolsetSlug(logContext.toolsetSlug),
			attr.SlogToolID(candidate.toolID),
			attr.SlogToolURN(candidate.entryKey),
			attr.SlogGenAIRequestModel(logContext.model),
			attr.SlogEmbeddingFallbackStrategy(selection.strategy),
		}
		if logContext.organizationSlug != "" {
			attrs = append(attrs, attr.SlogOrganizationSlug(logContext.organizationSlug))
		}
		if logContext.projectSlug != "" {
			attrs = append(attrs, attr.SlogProjectSlug(logContext.projectSlug))
		}

		logger.WarnContext(ctx, "tool embedding input exceeded token limit; using fallback", attrs...)
	}
}

func (s *ToolsetVectorStore) logEmbeddingFallbacks(
	ctx context.Context,
	toolset types.Toolset,
	projectID uuid.UUID,
	candidates []embeddingCandidate,
	selections []embeddingFallbackSelection,
) {
	logContext := embeddingFallbackLogContext{
		organizationID:   toolset.OrganizationID,
		organizationSlug: "",
		projectID:        toolset.ProjectID,
		projectSlug:      "",
		toolsetID:        toolset.ID,
		toolsetSlug:      string(toolset.Slug),
		model:            s.embeddingModel,
	}

	metadata, err := projectsrepo.New(s.db).GetProjectWithOrganizationMetadata(ctx, projectID)
	if err != nil {
		s.logger.WarnContext(
			ctx,
			"failed to load project metadata for embedding fallback log",
			attr.SlogError(err),
			attr.SlogOrganizationID(toolset.OrganizationID),
			attr.SlogProjectID(toolset.ProjectID),
			attr.SlogToolsetID(toolset.ID),
			attr.SlogToolsetSlug(string(toolset.Slug)),
		)
	} else {
		logContext.organizationSlug = metadata.Slug
		logContext.projectSlug = metadata.ProjectSlug
	}

	emitEmbeddingFallbackLogs(ctx, s.logger, logContext, candidates, selections)
}

func (s *ToolsetVectorStore) generateEmbeddings(ctx context.Context, toolset types.Toolset, projectID uuid.UUID, candidates []embeddingCandidate) ([][]float32, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	fallbackSelections, err := selectEmbeddingCandidateContents(s.embeddingModel, candidates)
	if err != nil {
		return nil, err
	}
	if len(fallbackSelections) > 0 {
		s.logEmbeddingFallbacks(ctx, toolset, projectID, candidates, fallbackSelections)
	}

	total := len(candidates)
	results := make([][]float32, total)

	// Create batches based on byte size
	batches := s.createBatchesBySize(candidates)
	if len(batches) == 0 {
		return results, nil
	}

	workerCount := min(embeddingMaxConcurrentBatches, len(batches))
	if workerCount == 0 {
		return results, nil
	}

	workChan := make(chan embeddingBatch, len(batches))

	for _, batch := range batches {
		workChan <- batch
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

	for range workerCount {
		wg.Go(func() {
			for batch := range workChan {
				if firstErr != nil {
					return
				}

				inputs := make([]string, 0, batch.endIdx-batch.startIdx)
				for i := batch.startIdx; i < batch.endIdx; i++ {
					inputs = append(inputs, candidates[i].content)
				}

				vectors, err := s.chatClient.CreateEmbeddings(ctx, toolset.OrganizationID, s.embeddingModel, inputs)
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
				for i := batch.startIdx; i < batch.endIdx; i++ {
					results[i] = vectors[i-batch.startIdx]
				}
				mu.Unlock()
			}
		})
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

func (s *ToolsetVectorStore) createBatchesBySize(candidates []embeddingCandidate) []embeddingBatch {
	return createBatchesWithinSize(candidates, embeddingMaxBatchBytes)
}

func createBatchesWithinSize(candidates []embeddingCandidate, maxBatchBytes int) []embeddingBatch {
	var batches []embeddingBatch
	currentBatchStart := 0
	currentBatchBytes := 0

	for i, candidate := range candidates {
		contentBytes := len(candidate.content)

		// If adding this candidate would exceed the limit, finalize current batch
		if currentBatchBytes > 0 && currentBatchBytes+contentBytes > maxBatchBytes {
			batches = append(batches, embeddingBatch{
				startIdx: currentBatchStart,
				endIdx:   i,
			})
			currentBatchStart = i
			currentBatchBytes = 0
		}

		currentBatchBytes += contentBytes
	}

	// Add final batch if there are remaining candidates
	if currentBatchStart < len(candidates) {
		batches = append(batches, embeddingBatch{
			startIdx: currentBatchStart,
			endIdx:   len(candidates),
		})
	}

	return batches
}

type toolListEntry struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	Meta        map[string]any  `json:"_meta,omitempty"`
}

func buildEmbeddableContent(entry *toolListEntry, tags []string, schemaSummary string) string {
	parts := []string{
		entry.Name,
		entry.Description,
	}
	if len(tags) > 0 {
		parts = append(parts, "tags: "+strings.Join(tags, ", "))
	}
	if schemaSummary != "" {
		parts = append(parts, "parameters:\n"+schemaSummary)
	}

	return strings.TrimSpace(strings.Join(filterNonEmpty(parts), "\n"))
}

func summarizeInputSchema(raw json.RawMessage) string {
	summary, _ := summarizeInputSchemaLevels(raw)
	return summary
}

func summarizeInputSchemaLevels(raw json.RawMessage) (full string, topLevel string) {
	if len(raw) == 0 {
		return "", ""
	}

	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return string(raw), ""
	}

	lines := make([]string, 0)
	appendSchemaSummary(&lines, "input", schema, false, 0)
	return strings.Join(lines, "\n"), summarizeTopLevelInputSchema(schema)
}

func summarizeTopLevelInputSchema(schema map[string]any) string {
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return ""
	}

	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)

	lines := make([]string, 0, len(names))
	for _, name := range names {
		line := "- " + name
		if property, ok := properties[name].(map[string]any); ok {
			description, _ := property["description"].(string)
			if description == "" {
				description, _ = property["title"].(string)
			}
			if description = summarizeSchemaText(description, 320); description != "" {
				line += ": " + description
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func buildTopLevelEmbeddableContent(entry *toolListEntry, schemaSummary string) string {
	parts := []string{entry.Name, entry.Description}
	if schemaSummary != "" {
		parts = append(parts, "parameters:\n"+schemaSummary)
	}
	return strings.TrimSpace(strings.Join(filterNonEmpty(parts), "\n"))
}

func buildNameDescriptionEmbeddableContent(entry *toolListEntry) string {
	return strings.TrimSpace(strings.Join(filterNonEmpty([]string{
		entry.Name,
		entry.Description,
	}), "\n"))
}

func appendSchemaSummary(lines *[]string, path string, schema map[string]any, required bool, depth int) {
	if depth > 16 {
		*lines = append(*lines, "- "+path+" (nested schema omitted)")
		return
	}

	details := make([]string, 0, 5)
	if required {
		details = append(details, "required")
	}
	details = append(details, schemaTypes(schema)...)
	if format, ok := schema["format"].(string); ok && format != "" {
		details = append(details, "format="+format)
	}
	if ref, ok := schema["$ref"].(string); ok && ref != "" {
		details = append(details, "ref="+ref)
	}
	if enum, ok := schema["enum"].([]any); ok && len(enum) > 0 {
		details = append(details, "enum="+summarizeSchemaValues(enum, 12))
	}
	if value, ok := schema["const"]; ok {
		details = append(details, "const="+summarizeSchemaValue(value))
	}

	line := "- " + path
	if len(details) > 0 {
		line += " (" + strings.Join(details, ", ") + ")"
	}
	description, _ := schema["description"].(string)
	if description == "" {
		description, _ = schema["title"].(string)
	}
	if description = summarizeSchemaText(description, 320); description != "" {
		line += ": " + description
	}
	*lines = append(*lines, line)

	requiredProperties := make(map[string]struct{})
	if requiredValues, ok := schema["required"].([]any); ok {
		for _, value := range requiredValues {
			if name, ok := value.(string); ok {
				requiredProperties[name] = struct{}{}
			}
		}
	}

	if properties, ok := schema["properties"].(map[string]any); ok {
		names := make([]string, 0, len(properties))
		for name := range properties {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			childPath := name
			if path != "input" {
				childPath = path + "." + name
			}
			_, childRequired := requiredProperties[name]
			appendSchemaSummaryValue(lines, childPath, properties[name], childRequired, depth+1)
		}
	}

	if items, ok := schema["items"]; ok {
		appendSchemaSummaryValue(lines, path+"[]", items, false, depth+1)
	}
	if additional, ok := schema["additionalProperties"].(map[string]any); ok {
		appendSchemaSummary(lines, path+".*", additional, false, depth+1)
	}

	for _, keyword := range []string{"oneOf", "anyOf", "allOf"} {
		alternatives, ok := schema[keyword].([]any)
		if !ok {
			continue
		}
		for i, alternative := range alternatives {
			appendSchemaSummaryValue(
				lines,
				fmt.Sprintf("%s %s[%d]", path, keyword, i+1),
				alternative,
				false,
				depth+1,
			)
		}
	}

	for _, keyword := range []string{"$defs", "definitions"} {
		definitions, ok := schema[keyword].(map[string]any)
		if !ok {
			continue
		}
		names := make([]string, 0, len(definitions))
		for name := range definitions {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			appendSchemaSummaryValue(lines, "definition "+name, definitions[name], false, depth+1)
		}
	}
}

func appendSchemaSummaryValue(lines *[]string, path string, value any, required bool, depth int) {
	switch schema := value.(type) {
	case map[string]any:
		appendSchemaSummary(lines, path, schema, required, depth)
	case bool:
		*lines = append(*lines, fmt.Sprintf("- %s (allowed=%t)", path, schema))
	}
}

func schemaTypes(schema map[string]any) []string {
	switch value := schema["type"].(type) {
	case string:
		if value != "" {
			return []string{value}
		}
	case []any:
		types := make([]string, 0, len(value))
		for _, item := range value {
			if schemaType, ok := item.(string); ok && schemaType != "" {
				types = append(types, schemaType)
			}
		}
		if len(types) > 0 {
			return []string{strings.Join(types, "|")}
		}
	}

	if _, ok := schema["properties"]; ok {
		return []string{"object"}
	}
	if _, ok := schema["items"]; ok {
		return []string{"array"}
	}
	return nil
}

func summarizeSchemaValues(values []any, limit int) string {
	summaries := make([]string, 0, min(len(values), limit))
	for _, value := range values[:min(len(values), limit)] {
		summaries = append(summaries, summarizeSchemaValue(value))
	}
	result := "[" + strings.Join(summaries, ", ") + "]"
	if remaining := len(values) - len(summaries); remaining > 0 {
		result += fmt.Sprintf(" +%d more", remaining)
	}
	return result
}

func summarizeSchemaValue(value any) string {
	switch value := value.(type) {
	case string:
		return summarizeSchemaText(value, 80)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return ""
		}
		return summarizeSchemaText(string(encoded), 80)
	}
}

func summarizeSchemaText(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "…"
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
