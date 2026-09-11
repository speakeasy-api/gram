package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	pgvector_go "github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/mock"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/rag/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func TestBuildEmbeddableContent_SummarizesSchemaDeterministically(t *testing.T) {
	t.Parallel()

	entry := &toolListEntry{
		Name:        "create_record",
		Description: "Create a record.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"description":"Input values.",
			"required":["zeta"],
			"properties":{
				"zeta":{"type":"string","description":" Last field. "},
				"alpha":{
					"type":"array",
					"items":{
						"type":"object",
						"required":["id"],
						"properties":{
							"mode":{"type":"string","enum":["fast","safe"]},
							"id":{"type":"string","format":"uuid"}
						}
					}
				}
			}
		}`),
		Meta: map[string]any{
			"ui": "metadata is retained in the payload, not embedded",
		},
	}

	schemaSummary, topLevelSchemaSummary := summarizeInputSchemaLevels(entry.InputSchema)
	content := buildEmbeddableContent(entry, []string{"source:http", "records"}, schemaSummary)
	require.Equal(t, `create_record
Create a record.
tags: source:http, records
parameters:
- input (object): Input values.
- alpha (array)
- alpha[] (object)
- alpha[].id (required, string, format=uuid)
- alpha[].mode (string, enum=[fast, safe])
- zeta (required, string): Last field.`, content)
	require.NotContains(t, content, "metadata is retained")
	require.Equal(t, `create_record
Create a record.
parameters:
- alpha
- zeta: Last field.`, buildTopLevelEmbeddableContent(entry, topLevelSchemaSummary))
	require.Equal(t, `create_record
Create a record.`, buildNameDescriptionEmbeddableContent(entry))
}

func TestSummarizeInputSchema_MalformedSchemaFallsBackToOriginal(t *testing.T) {
	t.Parallel()

	require.Equal(t, "{malformed", summarizeInputSchema(json.RawMessage("{malformed")))
}

func TestSummarizeInputSchema_BoundsDescriptionsAndEnums(t *testing.T) {
	t.Parallel()

	longDescription := strings.Repeat("x", 321)
	schema, err := json.Marshal(map[string]any{
		"type":        "string",
		"description": longDescription,
		"enum":        []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"},
	})
	require.NoError(t, err)

	summary := summarizeInputSchema(schema)
	require.Contains(t, summary, "enum=[a, b, c, d, e, f, g, h, i, j, k, l] +1 more")
	require.Contains(t, summary, strings.Repeat("x", 320)+"…")
	require.NotContains(t, summary, longDescription)
}

func TestCreateBatchesBySize_RespectsAggregateInputLimit(t *testing.T) {
	t.Parallel()

	denseContent := strings.Repeat("{}", embeddingMaxBatchBytes/4)
	candidates := []embeddingCandidate{
		{content: denseContent},
		{content: denseContent},
		{content: denseContent},
	}

	batches := (&ToolsetVectorStore{}).createBatchesBySize(candidates)
	require.Equal(t, []embeddingBatch{
		{startIdx: 0, endIdx: 2},
		{startIdx: 2, endIdx: 3},
	}, batches)

	for _, batch := range batches {
		totalBytes := 0
		for _, candidate := range candidates[batch.startIdx:batch.endIdx] {
			totalBytes += len(candidate.content)
		}
		require.LessOrEqual(t, totalBytes, embeddingMaxBatchBytes)
	}
}

func TestSelectEmbeddingCandidateContents_UsesSelectedSizeForBatching(t *testing.T) {
	t.Parallel()

	denseContent := strings.Repeat("{}", 9_000)
	candidates := []embeddingCandidate{
		{content: denseContent, fallbacks: []string{"compact-one"}},
		{content: denseContent, fallbacks: []string{"compact-two"}},
		{content: denseContent, fallbacks: []string{"compact-three"}},
	}
	batchLimit := len(denseContent) * 2
	require.Len(t, createBatchesWithinSize(candidates, batchLimit), 2)

	selections, err := selectEmbeddingCandidateContents(defaultEmbeddingModel, candidates)
	require.NoError(t, err)
	require.Equal(t, []embeddingFallbackSelection{
		{candidateIndex: 0, strategy: embeddingFallbackStrategyTopLevelSchema},
		{candidateIndex: 1, strategy: embeddingFallbackStrategyTopLevelSchema},
		{candidateIndex: 2, strategy: embeddingFallbackStrategyTopLevelSchema},
	}, selections)
	require.Equal(t, "compact-one", candidates[0].content)
	require.Equal(t, "compact-two", candidates[1].content)
	require.Equal(t, "compact-three", candidates[2].content)
	require.Len(t, createBatchesWithinSize(candidates, batchLimit), 1)
}

func TestEmitEmbeddingFallbackLogs_IncludesCustomerAndToolContext(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	candidates := []embeddingCandidate{{
		toolID:   "tool-id",
		entryKey: "urn:gram:tool:tool-id",
	}}
	logContext := embeddingFallbackLogContext{
		organizationID:   "org-id",
		organizationSlug: "org-slug",
		projectID:        "project-id",
		projectSlug:      "project-slug",
		toolsetID:        "toolset-id",
		toolsetSlug:      "toolset-slug",
		model:            defaultEmbeddingModel,
	}

	emitEmbeddingFallbackLogs(
		context.Background(),
		logger,
		logContext,
		candidates,
		[]embeddingFallbackSelection{{
			candidateIndex: 0,
			strategy:       embeddingFallbackStrategyNameDescription,
		}},
	)

	var record map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &record))
	require.Equal(t, "WARN", record["level"])
	require.Equal(t, "tool embedding input exceeded token limit; using fallback", record["msg"])
	require.Equal(t, "org-id", record[string(attr.OrganizationIDKey)])
	require.Equal(t, "org-slug", record[string(attr.OrganizationSlugKey)])
	require.Equal(t, "project-id", record[string(attr.ProjectIDKey)])
	require.Equal(t, "project-slug", record[string(attr.ProjectSlugKey)])
	require.Equal(t, "toolset-id", record[string(attr.ToolsetIDKey)])
	require.Equal(t, "toolset-slug", record[string(attr.ToolsetSlugKey)])
	require.Equal(t, "tool-id", record[string(attr.ToolIDKey)])
	require.Equal(t, "urn:gram:tool:tool-id", record[string(attr.ToolURNKey)])
	require.Equal(t, defaultEmbeddingModel, record[string(attr.GenAIRequestModelKey)])
	require.Equal(t, embeddingFallbackStrategyNameDescription, record[string(attr.EmbeddingFallbackStrategyKey)])
}

type embeddingCompletionClientMock struct {
	mock.Mock
	openrouter.CompletionClient
}

func (m *embeddingCompletionClientMock) CreateEmbeddings(
	ctx context.Context,
	orgID string,
	model string,
	inputs []string,
	_ ...openrouter.EmbeddingOption,
) ([][]float32, error) {
	args := m.Called(ctx, orgID, model, inputs)
	vectors, _ := args.Get(0).([][]float32)
	return vectors, args.Error(1)
}

func TestSearchToolsetTools_FilteredHNSWScanFindsToolsetRows(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, projectID, organizationID := newVectorSearchTestDatabase(t)
	queries := repo.New(conn)

	queryVector := make([]float32, 1536)
	queryVector[0] = 1
	distractorToolsetID := uuid.New()
	for i := range 50 {
		_, err := queries.InsertToolsetEmbedding(ctx, repo.InsertToolsetEmbeddingParams{
			ProjectID:      projectID,
			ToolsetID:      distractorToolsetID,
			ToolsetVersion: 1,
			EntryKey:       fmt.Sprintf("tools:distractor-%d", i),
			EmbeddingModel: defaultEmbeddingModel,
			Embedding1536:  pgvector_go.NewVector(queryVector),
			Payload:        []byte(fmt.Sprintf(`{"name":"distractor_%d"}`, i)),
			Tags:           []string{"source:http"},
		})
		require.NoError(t, err)
	}

	targetToolsetID := uuid.New()
	targetVector := make([]float32, 1536)
	targetVector[0] = 0.9
	targetVector[1] = 0.1
	_, err := queries.InsertToolsetEmbedding(ctx, repo.InsertToolsetEmbeddingParams{
		ProjectID:      projectID,
		ToolsetID:      targetToolsetID,
		ToolsetVersion: 1,
		EntryKey:       "tools:customers-list",
		EmbeddingModel: defaultEmbeddingModel,
		Embedding1536:  pgvector_go.NewVector(targetVector),
		Payload:        []byte(`{"name":"customers_list","description":"List customers."}`),
		Tags:           []string{"source:http", "polar/customers"},
	})
	require.NoError(t, err)
	for i := range 500 {
		otherTargetVector := make([]float32, 1536)
		otherTargetVector[0] = 0.8
		otherTargetVector[1] = 0.2
		_, err := queries.InsertToolsetEmbedding(ctx, repo.InsertToolsetEmbeddingParams{
			ProjectID:      projectID,
			ToolsetID:      targetToolsetID,
			ToolsetVersion: 1,
			EntryKey:       fmt.Sprintf("tools:target-%d", i),
			EmbeddingModel: defaultEmbeddingModel,
			Embedding1536:  pgvector_go.NewVector(otherTargetVector),
			Payload:        []byte(fmt.Sprintf(`{"name":"target_%d"}`, i)),
			Tags:           []string{"source:http"},
		})
		require.NoError(t, err)
	}

	approximateMatches, err := queries.SearchToolsetToolEmbeddingsAnyTagsMatch(ctx, repo.SearchToolsetToolEmbeddingsAnyTagsMatchParams{
		QueryEmbedding1536: pgvector_go.NewVector(queryVector),
		ProjectID:          projectID,
		ToolsetID:          targetToolsetID,
		ToolsetVersion:     1,
		Tags:               []string{},
		ResultLimit:        1,
	})
	require.NoError(t, err)
	require.NotEmpty(t, approximateMatches)

	client := &embeddingCompletionClientMock{}
	client.On("CreateEmbeddings", mock.Anything, organizationID, defaultEmbeddingModel, []string{"list customers"}).
		Return([][]float32{queryVector}, nil).
		Once()
	client.On("CreateEmbeddings", mock.Anything, organizationID, defaultEmbeddingModel, []string{"customers list"}).
		Return([][]float32{queryVector}, nil).
		Once()
	client.On("CreateEmbeddings", mock.Anything, organizationID, defaultEmbeddingModel, []string{"customers all tags"}).
		Return([][]float32{queryVector}, nil).
		Once()
	t.Cleanup(func() { client.AssertExpectations(t) })

	store := NewToolsetVectorStore(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		conn,
		client,
	)
	toolset := types.Toolset{
		ID:             targetToolsetID.String(),
		ProjectID:      projectID.String(),
		OrganizationID: organizationID,
		ToolsetVersion: 1,
	}
	matches, err := store.SearchToolsetTools(ctx, toolset, SearchToolsOptions{
		Query:     "list customers",
		Tags:      nil,
		MatchMode: MatchModeAny,
		Limit:     1,
	})
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.NotContains(t, matches[0].ToolName, "distractor")
	require.Greater(t, matches[0].SimilarityScore, 0.95)

	taggedMatches, err := store.SearchToolsetTools(ctx, toolset, SearchToolsOptions{
		Query:     "customers list",
		Tags:      []string{"polar/customers"},
		MatchMode: MatchModeAny,
		Limit:     1,
	})
	require.NoError(t, err)
	require.Len(t, taggedMatches, 1)
	require.Equal(t, "customers_list", taggedMatches[0].ToolName)

	allTagsMatches, err := store.SearchToolsetTools(ctx, toolset, SearchToolsOptions{
		Query:     "customers all tags",
		Tags:      []string{"source:http", "polar/customers"},
		MatchMode: MatchModeAll,
		Limit:     1,
	})
	require.NoError(t, err)
	require.Len(t, allTagsMatches, 1)
	require.Equal(t, "customers_list", allTagsMatches[0].ToolName)
}
